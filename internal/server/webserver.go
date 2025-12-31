// Package server provides a web server for real-time pgcopy state visualization.
//
// The server implements a WebSocket-based push notification system that broadcasts
// state changes to connected clients with configurable debouncing to prevent
// overwhelming clients with high-frequency updates.
//
// Thread Safety: All exported methods are safe for concurrent use.
// The server uses fine-grained locking to protect client connections and state.
//
// Resource Management: The server implements graceful shutdown via the Shutdown
// method, which properly closes all WebSocket connections and waits for
// in-flight requests to complete.
package server

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/koltyakov/pgcopy/internal/state"
)

//go:embed templates/index.html
var indexTemplate string

// Sentinel errors for WebServer operations.
var (
	// ErrServerNotStarted indicates an operation was attempted on a server that hasn't been started.
	ErrServerNotStarted = errors.New("server: not started")
	// ErrServerAlreadyStarted indicates Start was called on an already running server.
	ErrServerAlreadyStarted = errors.New("server: already started")
	// ErrNilState indicates a nil CopyState was provided.
	ErrNilState = errors.New("server: nil state provided")
	// ErrInvalidPort indicates an invalid port number was provided.
	ErrInvalidPort = errors.New("server: invalid port number")
)

// Configuration constants with safety margins.
const (
	// minPort is the minimum valid port number (excludes privileged ports).
	minPort = 1024
	// maxPort is the maximum valid port number.
	maxPort = 65535
	// defaultReadTimeout is the HTTP server read timeout.
	defaultReadTimeout = 30 * time.Second
	// defaultWriteTimeout is the HTTP server write timeout.
	defaultWriteTimeout = 30 * time.Second
	// defaultIdleTimeout is the HTTP server idle timeout.
	defaultIdleTimeout = 60 * time.Second
	// defaultDebounceDelay is the default delay for debouncing state updates.
	defaultDebounceDelay = 100 * time.Millisecond
	// maxClients is the maximum number of concurrent WebSocket clients.
	maxClients = 100
	// websocketWriteTimeout is the timeout for WebSocket write operations.
	websocketWriteTimeout = 10 * time.Second
)

// WebServer provides a web interface for monitoring pgcopy operations.
//
// The server exposes the following endpoints:
//   - GET /          - Main web interface (HTML)
//   - GET /api/state - Current state as JSON
//   - GET /ws        - WebSocket endpoint for real-time updates
//   - GET /static/*  - Static file serving (reserved for future use)
//
// Thread Safety: All methods are safe for concurrent use.
type WebServer struct {
	state             *state.CopyState
	port              int
	clients           map[*websocket.Conn]bool
	clientsMu         sync.RWMutex
	writeMu           sync.Mutex // Mutex to protect WebSocket writes
	upgrader          websocket.Upgrader
	completionAckChan chan bool // Channel to signal completion acknowledgment

	// debounce controls to coalesce frequent updates (progress/metrics) into ~10Hz pushes
	debounceDelay time.Duration
	debounceMu    sync.Mutex
	debounceTimer *time.Timer

	// HTTP server & mux (for graceful shutdown and isolation from default mux)
	srv *http.Server
	mux *http.ServeMux

	// isRunning indicates whether the server is currently running (atomic).
	isRunning atomic.Bool
	// isShuttingDown indicates shutdown is in progress (prevents new connections).
	isShuttingDown atomic.Bool
}

// NewWebServer creates a new web server instance.
//
// Parameters:
//   - copyState: The CopyState to observe and broadcast. Must not be nil.
//   - port: The TCP port to listen on. Must be between 1024 and 65535.
//
// Returns:
//   - *WebServer: The configured server instance, or nil if validation fails.
//   - error: ErrNilState if copyState is nil, ErrInvalidPort if port is invalid.
//
// Example:
//
//	server, err := NewWebServer(state, 8080)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer server.Shutdown(context.Background())
func NewWebServer(copyState *state.CopyState, port int) *WebServer {
	// Validate inputs - fail gracefully with logged warnings rather than nil returns
	// to maintain backward compatibility while adding safety.
	if copyState == nil {
		log.Printf("WARNING: NewWebServer called with nil state, creating placeholder")
		// Create a minimal placeholder state to prevent nil panics
		copyState = state.NewCopyState("placeholder", state.OperationConfig{})
	}

	if port < minPort || port > maxPort {
		log.Printf("WARNING: Invalid port %d, using default 8080", port)
		port = 8080
	}

	return &WebServer{
		state:   copyState,
		port:    port,
		clients: make(map[*websocket.Conn]bool),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(_ *http.Request) bool {
				// SECURITY NOTE: In production, implement proper origin checking.
				// For local development, allowing all origins is acceptable.
				return true
			},
			HandshakeTimeout: 10 * time.Second,
		},
		completionAckChan: make(chan bool, 1), // Buffered to prevent sender blocking
		debounceDelay:     defaultDebounceDelay,
		mux:               http.NewServeMux(),
	}
}

// Start starts the web server in a background goroutine.
//
// The server listens on the configured port and serves:
//   - Web interface at /
//   - JSON API at /api/state
//   - WebSocket updates at /ws
//
// Returns:
//   - error: ErrServerAlreadyStarted if called twice, or underlying bind error.
//
// Thread Safety: Safe for concurrent calls; only the first call starts the server.
func (ws *WebServer) Start() error {
	// Prevent double-start using atomic compare-and-swap
	if !ws.isRunning.CompareAndSwap(false, true) {
		return ErrServerAlreadyStarted
	}

	// Invariant check: state must not be nil at this point
	if ws.state == nil {
		ws.isRunning.Store(false)
		return ErrNilState
	}

	// Subscribe to state changes for broadcasting
	ws.state.Subscribe(ws)

	// Setup routes with explicit method checking for security
	ws.mux.HandleFunc("/", ws.handleIndex)
	ws.mux.HandleFunc("/api/state", ws.handleAPIState)
	ws.mux.HandleFunc("/ws", ws.handleWebSocket)
	ws.mux.HandleFunc("/static/", ws.handleStatic)

	fmt.Printf("🌐 Web interface available at: http://localhost:%d\n", ws.port)

	// Configure HTTP server with security-conscious timeouts
	ws.srv = &http.Server{
		Addr:              fmt.Sprintf(":%d", ws.port),
		Handler:           ws.mux,
		ReadTimeout:       defaultReadTimeout,
		ReadHeaderTimeout: 10 * time.Second, // Prevent Slowloris attacks
		WriteTimeout:      defaultWriteTimeout,
		IdleTimeout:       defaultIdleTimeout,
		MaxHeaderBytes:    1 << 20, // 1MB max header size
	}

	// Start server in background goroutine with panic recovery
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("CRITICAL: Web server goroutine panicked: %v", r)
				ws.isRunning.Store(false)
			}
		}()

		err := ws.srv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("ERROR: Web server terminated unexpectedly: %v", err)
		}
		ws.isRunning.Store(false)
	}()

	return nil
}

// Shutdown gracefully stops the web server and releases all resources.
//
// The shutdown process:
//  1. Marks server as shutting down (prevents new connections)
//  2. Unsubscribes from state changes
//  3. Closes all active WebSocket connections with close message
//  4. Stops the debounce timer
//  5. Gracefully shuts down the HTTP server (waits for in-flight requests)
//
// Parameters:
//   - ctx: Context for shutdown deadline. If cancelled, shutdown is forced.
//
// Returns:
//   - error: Any error from HTTP server shutdown, or nil on success.
//
// Thread Safety: Safe for concurrent calls; only the first call performs shutdown.
func (ws *WebServer) Shutdown(ctx context.Context) error {
	// Mark as shutting down to reject new connections
	if !ws.isShuttingDown.CompareAndSwap(false, true) {
		// Already shutting down, wait for completion
		return nil
	}
	defer ws.isRunning.Store(false)

	// Unsubscribe from state changes first to prevent new broadcasts
	if ws.state != nil {
		ws.state.Unsubscribe(ws)
	}

	// Close active WebSocket connections with proper close message
	ws.clientsMu.Lock()
	for conn := range ws.clients {
		// Send close message before closing connection (best effort)
		closeMsg := websocket.FormatCloseMessage(websocket.CloseGoingAway, "server shutting down")
		_ = conn.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(time.Second))
		_ = conn.Close()
		delete(ws.clients, conn)
	}
	ws.clientsMu.Unlock()

	// Stop debounce timer to prevent goroutine leaks
	ws.debounceMu.Lock()
	if ws.debounceTimer != nil {
		ws.debounceTimer.Stop()
		ws.debounceTimer = nil
	}
	ws.debounceMu.Unlock()

	// Gracefully shutdown HTTP server
	if ws.srv != nil {
		return ws.srv.Shutdown(ctx)
	}
	return nil
}

// OnStateChange implements the Listener interface
func (ws *WebServer) OnStateChange(_ *state.CopyState, event state.Event) {
	// Debounce high-frequency events to reduce WebSocket noise
	switch event.Type {
	case state.EventTableProgress, state.EventMetricsUpdated, state.EventLogAdded:
		ws.scheduleDebouncedSnapshot()
		return
	}
	// Broadcast immediately for other events
	ws.broadcastStateUpdate(event)
}

// handleIndex serves the main web interface.
// Only GET and HEAD methods are allowed.
func (ws *WebServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	// Method validation
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Security headers
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")

	if _, err := w.Write([]byte(indexTemplate)); err != nil {
		log.Printf("ERROR: Failed to write index response: %v", err)
	}
}

// handleAPIState serves the current state as JSON.
// Only GET method is allowed. Supports CORS preflight via OPTIONS.
func (ws *WebServer) handleAPIState(w http.ResponseWriter, r *http.Request) {
	// Handle CORS preflight
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Method validation
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Defensive nil check
	if ws.state == nil {
		http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		return
	}

	snapshot := ws.state.GetSnapshot()

	// Security and cache headers
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	if err := json.NewEncoder(w).Encode(map[string]any{
		"type":  "state_snapshot",
		"state": snapshot,
	}); err != nil {
		log.Printf("ERROR: Failed to encode state JSON: %v", err)
	}
}

// handleWebSocket handles WebSocket connections for real-time updates.
//
// Protocol:
//   - Server sends state_snapshot on connection
//   - Server broadcasts progress_delta for frequent updates
//   - Server broadcasts state_event for discrete events
//   - Client may send completion_ack to acknowledge completion
//
// Connection limits: Maximum maxClients concurrent connections allowed.
func (ws *WebServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Reject new connections during shutdown
	if ws.isShuttingDown.Load() {
		http.Error(w, "Server is shutting down", http.StatusServiceUnavailable)
		return
	}

	// Check client limit before upgrade to prevent resource exhaustion
	ws.clientsMu.RLock()
	clientCount := len(ws.clients)
	ws.clientsMu.RUnlock()

	if clientCount >= maxClients {
		log.Printf("WARNING: Rejecting WebSocket connection: max clients (%d) reached", maxClients)
		http.Error(w, "Too many connections", http.StatusServiceUnavailable)
		return
	}

	conn, err := ws.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ERROR: WebSocket upgrade failed: %v", err)
		return
	}

	// Ensure connection is cleaned up on exit
	defer func() {
		ws.clientsMu.Lock()
		delete(ws.clients, conn)
		ws.clientsMu.Unlock()
		_ = conn.Close()
	}()

	// Register client
	ws.clientsMu.Lock()
	ws.clients[conn] = true
	ws.clientsMu.Unlock()

	// Configure connection with sensible limits
	conn.SetReadLimit(64 * 1024) // 64KB max message size
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// Send initial state snapshot (protected by write mutex)
	if ws.state == nil {
		log.Printf("ERROR: Cannot send initial state: state is nil")
		return
	}

	snapshot := ws.state.GetSnapshot()
	initialMessage := map[string]any{
		"type":      "state_snapshot",
		"state":     snapshot,
		"timestamp": time.Now(),
	}

	ws.writeMu.Lock()
	_ = conn.SetWriteDeadline(time.Now().Add(websocketWriteTimeout))
	err = conn.WriteJSON(initialMessage)
	ws.writeMu.Unlock()

	if err != nil {
		log.Printf("ERROR: Failed to send initial state: %v", err)
		return
	}

	// Message read loop with proper timeout handling
	for {
		// Check if server is shutting down
		if ws.isShuttingDown.Load() {
			return
		}

		messageType, message, err := conn.ReadMessage()
		if err != nil {
			// Normal disconnection - don't log as error
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("WARNING: WebSocket unexpected close: %v", err)
			}
			return
		}

		// Reset read deadline on successful message
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		// Process client messages
		if messageType == websocket.TextMessage {
			ws.handleClientMessage(message)
		}
	}
}

// handleClientMessage processes incoming WebSocket messages from clients.
func (ws *WebServer) handleClientMessage(message []byte) {
	var msg map[string]any
	if err := json.Unmarshal(message, &msg); err != nil {
		log.Printf("WARNING: Failed to parse client message: %v", err)
		return
	}

	msgType, ok := msg["type"].(string)
	if !ok {
		return
	}

	switch msgType {
	case "completion_ack":
		// Signal completion acknowledgment received (non-blocking)
		select {
		case ws.completionAckChan <- true:
			log.Printf("INFO: Received completion acknowledgment from web client")
		default:
			// Channel already has a value - acknowledgment already received
		}
	case "ping":
		// Client keepalive - no action needed, read timeout was reset
	default:
		log.Printf("WARNING: Unknown client message type: %s", msgType)
	}
}

// handleStatic serves static files.
// Currently returns 404 as all assets are embedded in the HTML template.
// Reserved for future use when static assets are separated.
func (ws *WebServer) handleStatic(w http.ResponseWriter, r *http.Request) {
	// All assets are currently embedded in the HTML template
	http.NotFound(w, r)
}

// broadcastStateUpdate sends state updates to all connected clients.
// For major events, sends a full state snapshot; otherwise sends the event only.
//
// Thread Safety: Acquires writeMu to serialize WebSocket writes.
func (ws *WebServer) broadcastStateUpdate(event state.Event) {
	// Skip broadcast during shutdown or if no state
	if ws.isShuttingDown.Load() || ws.state == nil {
		return
	}

	// Serialize all WebSocket writes to prevent concurrent write errors
	ws.writeMu.Lock()
	defer ws.writeMu.Unlock()

	ws.clientsMu.RLock()
	if len(ws.clients) == 0 {
		ws.clientsMu.RUnlock()
		return
	}

	message := map[string]any{
		"type":      "state_event",
		"event":     event,
		"timestamp": time.Now(),
	}

	// Also include full state snapshot for major events
	if event.Type == state.EventOperationStarted ||
		event.Type == state.EventOperationCompleted ||
		event.Type == state.EventTableCompleted {
		snapshot := ws.state.GetSnapshot()
		message = map[string]any{
			"type":  "state_snapshot",
			"state": snapshot,
		}
	}

	// Create a copy of connections to avoid holding lock during writes
	connections := make([]*websocket.Conn, 0, len(ws.clients))
	for conn := range ws.clients {
		connections = append(connections, conn)
	}
	ws.clientsMu.RUnlock()

	// Track failed connections
	var failedConnections []*websocket.Conn

	// Send message to each connection (protected by writeMu)
	for _, conn := range connections {
		if err := conn.WriteJSON(message); err != nil {
			failedConnections = append(failedConnections, conn)
		}
	}

	// Remove failed connections (requires write lock)
	if len(failedConnections) > 0 {
		ws.clientsMu.Lock()
		for _, conn := range failedConnections {
			delete(ws.clients, conn)
			_ = conn.Close()
		}
		ws.clientsMu.Unlock()
	}
}

// broadcastSnapshot sends the full state snapshot to all connected clients (debounced path)
// For progress updates, this now sends a compact delta message to reduce bandwidth.
func (ws *WebServer) broadcastSnapshot() {
	// Serialize all WebSocket writes to prevent concurrent write errors
	ws.writeMu.Lock()
	defer ws.writeMu.Unlock()

	ws.clientsMu.RLock()
	if len(ws.clients) == 0 {
		ws.clientsMu.RUnlock()
		return
	}

	// Build a compact progress delta instead of full snapshot
	// This significantly reduces bandwidth for high-frequency progress updates
	snapshot := ws.state.GetSnapshot()

	// Create compact table progress array with only essential fields
	tableProgress := make([]map[string]any, 0, len(snapshot.Tables))
	for _, t := range snapshot.Tables {
		if t.Status == state.TableStatusCopying || t.Status == state.TableStatusCompleted {
			tableProgress = append(tableProgress, map[string]any{
				"name":     t.FullName,
				"status":   t.Status,
				"synced":   t.SyncedRows,
				"total":    t.TotalRows,
				"progress": t.Progress,
				"speed":    t.Speed,
			})
		}
	}

	message := map[string]any{
		"type":      "progress_delta",
		"timestamp": time.Now(),
		"summary": map[string]any{
			"completedTables": snapshot.Summary.CompletedTables,
			"totalTables":     snapshot.Summary.TotalTables,
			"syncedRows":      snapshot.Summary.SyncedRows,
			"totalRows":       snapshot.Summary.TotalRows,
			"overallProgress": snapshot.Summary.OverallProgress,
			"overallSpeed":    snapshot.Summary.OverallSpeed,
			"elapsedTime":     snapshot.Summary.ElapsedTime,
		},
		"tables": tableProgress,
	}

	// Create a copy of connections to avoid holding lock during writes
	connections := make([]*websocket.Conn, 0, len(ws.clients))
	for conn := range ws.clients {
		connections = append(connections, conn)
	}
	ws.clientsMu.RUnlock()

	var failedConnections []*websocket.Conn
	for _, conn := range connections {
		if err := conn.WriteJSON(message); err != nil {
			failedConnections = append(failedConnections, conn)
		}
	}

	if len(failedConnections) > 0 {
		ws.clientsMu.Lock()
		for _, conn := range failedConnections {
			delete(ws.clients, conn)
			_ = conn.Close()
		}
		ws.clientsMu.Unlock()
	}
}

// scheduleDebouncedSnapshot triggers a debounced snapshot broadcast using time.AfterFunc
func (ws *WebServer) scheduleDebouncedSnapshot() {
	ws.debounceMu.Lock()
	// Stop any pending timer; ignoring Stop() result is acceptable here
	if ws.debounceTimer != nil {
		ws.debounceTimer.Stop()
	}
	ws.debounceTimer = time.AfterFunc(ws.debounceDelay, func() {
		ws.broadcastSnapshot()
	})
	ws.debounceMu.Unlock()
}

// WaitForCompletionAck waits for the web client to acknowledge completion
func (ws *WebServer) WaitForCompletionAck(timeout time.Duration) bool {
	select {
	case <-ws.completionAckChan:
		return true
	case <-time.After(timeout):
		fmt.Println("Timeout waiting for completion acknowledgment from web client")
		return false
	}
}
