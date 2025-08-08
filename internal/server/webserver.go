// Package server provides a web server for real-time pgcopy state visualization
package server

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/koltyakov/pgcopy/internal/state"
)

//go:embed templates/index.html
var indexTemplate string

// WebServer provides a web interface for monitoring pgcopy operations
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
}

// NewWebServer creates a new web server instance
func NewWebServer(copyState *state.CopyState, port int) *WebServer {
	return &WebServer{
		state:   copyState,
		port:    port,
		clients: make(map[*websocket.Conn]bool),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(_ *http.Request) bool {
				return true // Allow all origins for development
			},
		},
		completionAckChan: make(chan bool, 1), // Buffered channel for completion acknowledgment
		debounceDelay:     100 * time.Millisecond,
	}
}

// Start starts the web server
func (ws *WebServer) Start() error {
	// Subscribe to state changes
	ws.state.Subscribe(ws)

	// Setup routes
	http.HandleFunc("/", ws.handleIndex)
	http.HandleFunc("/api/state", ws.handleAPIState)
	http.HandleFunc("/ws", ws.handleWebSocket)
	http.HandleFunc("/static/", ws.handleStatic)

	fmt.Printf("🌐 Web interface available at: http://localhost:%d\n", ws.port)

	go func() {
		server := &http.Server{
			Addr:         fmt.Sprintf(":%d", ws.port),
			Handler:      nil,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  60 * time.Second,
		}
		if err := server.ListenAndServe(); err != nil {
			fmt.Printf("Web server error: %v\n", err)
		}
	}()

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

// handleIndex serves the main web interface
func (ws *WebServer) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	_, _ = w.Write([]byte(indexTemplate))
}

// handleAPIState serves the current state as JSON
func (ws *WebServer) handleAPIState(w http.ResponseWriter, _ *http.Request) {
	snapshot := ws.state.GetSnapshot()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"type":  "state_snapshot",
		"state": snapshot,
	})
}

// handleWebSocket handles WebSocket connections for real-time updates
func (ws *WebServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := ws.upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Printf("WebSocket upgrade error: %v\n", err)
		return
	}
	defer func() { _ = conn.Close() }()

	// Add client to the list
	ws.clientsMu.Lock()
	ws.clients[conn] = true
	ws.clientsMu.Unlock()

	// Send initial state (protected by write mutex)
	snapshot := ws.state.GetSnapshot()
	initialMessage := map[string]interface{}{
		"type":  "state_snapshot",
		"state": snapshot,
	}

	ws.writeMu.Lock()
	err = conn.WriteJSON(initialMessage)
	ws.writeMu.Unlock()

	if err != nil {
		fmt.Printf("Error sending initial state: %v\n", err)
		return
	}

	// Keep connection alive and handle client messages
	for {
		// Read message from client
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			// Client disconnected
			ws.clientsMu.Lock()
			delete(ws.clients, conn)
			ws.clientsMu.Unlock()
			break
		}

		// Handle JSON messages from client
		if messageType == websocket.TextMessage {
			var msg map[string]interface{}
			if err := json.Unmarshal(message, &msg); err == nil {
				if msgType, ok := msg["type"].(string); ok && msgType == "completion_ack" {
					// Signal completion acknowledgment received
					select {
					case ws.completionAckChan <- true:
						fmt.Println("Received completion acknowledgment from web client")
					default:
						// Channel already has a value, acknowledgment already received
					}
				}
			}
		}
	}
}

// handleStatic serves static files (if needed)
func (ws *WebServer) handleStatic(w http.ResponseWriter, r *http.Request) {
	// For now, we embed everything in the HTML
	http.NotFound(w, r)
}

// broadcastStateUpdate sends state updates to all connected clients
func (ws *WebServer) broadcastStateUpdate(event state.Event) {
	// Serialize all WebSocket writes to prevent concurrent write errors
	ws.writeMu.Lock()
	defer ws.writeMu.Unlock()

	ws.clientsMu.RLock()
	if len(ws.clients) == 0 {
		ws.clientsMu.RUnlock()
		return
	}

	message := map[string]interface{}{
		"type":      "state_event",
		"event":     event,
		"timestamp": time.Now(),
	}

	// Also include full state snapshot for major events
	if event.Type == state.EventOperationStarted ||
		event.Type == state.EventOperationCompleted ||
		event.Type == state.EventTableCompleted {
		snapshot := ws.state.GetSnapshot()
		message = map[string]interface{}{
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
func (ws *WebServer) broadcastSnapshot() {
	// Serialize all WebSocket writes to prevent concurrent write errors
	ws.writeMu.Lock()
	defer ws.writeMu.Unlock()

	ws.clientsMu.RLock()
	if len(ws.clients) == 0 {
		ws.clientsMu.RUnlock()
		return
	}

	snapshot := ws.state.GetSnapshot()
	message := map[string]interface{}{
		"type":  "state_snapshot",
		"state": snapshot,
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
