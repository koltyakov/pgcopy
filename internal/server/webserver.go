// Package server provides a web server for real-time pgcopy state visualization
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/koltyakov/pgcopy/internal/state"
)

// WebServer provides a web interface for monitoring pgcopy operations
type WebServer struct {
	state             *state.CopyState
	port              int
	clients           map[*websocket.Conn]bool
	clientsMu         sync.RWMutex
	writeMu           sync.Mutex // Mutex to protect WebSocket writes
	upgrader          websocket.Upgrader
	completionAckChan chan bool // Channel to signal completion acknowledgment
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
	// Broadcast state changes to all connected clients
	ws.broadcastStateUpdate(event)
}

// handleIndex serves the main web interface
func (ws *WebServer) handleIndex(w http.ResponseWriter, _ *http.Request) {
	html := `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>pgcopy - Real-time Monitor</title>
  <style>
    * { margin: 0; padding: 0; box-sizing: border-box; }
    body { 
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Arial, sans-serif; 
      background: #f5f5f5;
      min-height: 100vh;
      color: #333;
      display: flex;
      flex-direction: column;
    }
    .container { 
      max-width: 1800px; 
      margin: 0 auto; 
      padding: 20px; 
      flex: 1;
      display: flex;
      flex-direction: column;
    }
    .header {
      background: #ffffff;
      border: 1px solid #ddd;
      border-radius: 8px;
      padding: 20px;
      margin-bottom: 20px;
      box-shadow: 0 2px 4px rgba(0, 0, 0, 0.1);
      display: flex;
      justify-content: space-between;
      align-items: flex-start;
    }
    .header h1 { 
      color: #333; 
      font-size: 1.8em; 
      margin: 0;
      display: flex;
      align-items: center;
      gap: 15px;
    }
    .header-right {
      text-align: right;
    }
    .status-indicator {
      width: 12px;
      height: 12px;
      border-radius: 50%;
      display: inline-block;
      margin-right: 8px;
    }
    .status-initializing { background: #ff9500; }
    .status-copying { background: #007aff; }
    .status-completed { background: #34c759; }
    .status-failed { background: #ff3b30; }
    
    .grid {
      display: grid;
      grid-template-columns: 2fr 1fr;
      gap: 20px;
      margin-bottom: 20px;
    }
    
    .tables-section {
      grid-column: 1 / -1; /* Span full width */
      flex: 1; /* Take remaining space */
      display: flex;
      flex-direction: column;
    }
    
    .card {
      background: #ffffff;
      border: 1px solid #ddd;
      border-radius: 8px;
      padding: 20px;
      box-shadow: 0 2px 4px rgba(0, 0, 0, 0.1);
    }
    
    .progress-section h2,
    .tables-section h2 {
      color: #333;
      margin-bottom: 20px; /* Added more space */
      font-size: 1.2em;
    }
    
    .config-section h2 {
      color: #333;
      margin-bottom: 20px; /* Added more space */
      font-size: 1.2em;
    }
    
    .overall-progress {
      margin-bottom: 20px;
    }
    
    .progress-bar {
      width: 100%;
      height: 20px;
      background: #e5e5e5;
      border-radius: 4px;
      overflow: hidden;
      margin: 10px 0;
      border: 1px solid #ccc;
    }
    
    .progress-fill {
      height: 100%;
      background: #007aff;
      border-radius: 3px;
      transition: width 0.3s ease;
    }
    
    .stats-grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(100px, 1fr));
      gap: 10px;
      margin: 15px 0;
    }
    
    .stat-item {
      text-align: center;
      padding: 10px;
      background: #f8f9fa;
      border-radius: 4px;
      border: 1px solid #dee2e6;
    }
    
    .stat-value {
      font-size: 1.5em;
      font-weight: 600;
      color: #007aff;
      display: block;
    }
    
    .stat-label {
      color: #666;
      font-size: 0.85em;
      margin-top: 4px;
    }
    
    .table-item {
      display: flex;
      justify-content: space-between;
      align-items: center;
      padding: 6px 10px;
      margin: 2px 0;
      background: #f8f9fa;
      border-radius: 4px;
      border-left: 3px solid #007aff;
      border: 1px solid #e9ecef;
      font-size: 0.85em;
      min-height: 36px; /* More compact height */
      box-sizing: border-box;
    }
    
    .tables-container {
      flex: 1; /* Take remaining height */
      display: flex;
      flex-direction: column;
      gap: 2px;
      align-content: start; /* Align grid content to top */
      overflow: hidden; /* Prevent scrolling */
    }
    
    .table-item.copying {
      border-left-color: #ff9500;
      background: #fff3cd;
    }
    
    .table-item.completed {
      border-left-color: #34c759;
      background: #d4edda;
    }
    
    .table-item.failed {
      border-left-color: #ff3b30;
      background: #f8d7da;
    }
    
    .table-name {
      font-weight: 500;
      color: #333;
      flex: 1;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    
    .table-progress {
      display: flex;
      align-items: center;
      gap: 8px;
    }
    
    .table-stats {
      text-align: right;
      min-width: 100px;
      font-size: 0.8em;
    }
    
    .connection-status {
      padding: 8px;
      background: #d1ffd6;
      border-radius: 4px;
      border: 1px solid #b3e5b3;
      font-size: 0.85em;
    }
    
    .connection-status.disconnected {
      background: #ffeaea;
      border-color: #ffb3b3;
    }
    
    /* Responsive design */
    @media (max-width: 768px) {
      .grid {
        grid-template-columns: 1fr;
      }
      .header {
        flex-direction: column;
        gap: 15px;
      }
      .header-right {
        align-self: stretch;
        text-align: left;
      }
    }
    
    @media (max-width: 1024px) and (min-width: 769px) {
      /* Keep single column for all screen sizes */
    }
  </style>
</head>
<body>
  <div class="container">
    <div class="header">
      <div>
        <h1>
          <span class="status-indicator status-initializing" id="statusIndicator"></span>
          pgcopy Monitor
        </h1>
      </div>
      <div class="header-right">
        <div class="connection-status" id="connectionStatus">
          <div>WebSocket: <span id="wsStatus">Connecting...</span></div>
          <div>Last Update: <span id="lastUpdate">Never</span></div>
        </div>
      </div>
    </div>
    
    <div class="grid">
      <div class="card progress-section">
        <h2>📊 Copy Progress</h2>
        <div class="overall-progress">
          <div class="progress-bar">
            <div class="progress-fill" id="overallProgress" style="width: 0%"></div>
          </div>
          <div id="progressText">0% Complete</div>
        </div>
        
        <div class="stats-grid">
          <div class="stat-item">
            <span class="stat-value" id="completedTables">0</span>
            <div class="stat-label">Tables Done</div>
          </div>
          <div class="stat-item">
            <span class="stat-value" id="totalRows">0</span>
            <div class="stat-label">Total Rows</div>
          </div>
          <div class="stat-item">
            <span class="stat-value" id="rowsPerSecond">0</span>
            <div class="stat-label">Rows/sec</div>
          </div>
          <div class="stat-item">
            <span class="stat-value" id="eta">--</span>
            <div class="stat-label">ETA</div>
          </div>
          <div class="stat-item">
            <span class="stat-value" id="totalDuration">--</span>
            <div class="stat-label">Duration</div>
          </div>
        </div>
      </div>
      
      <div class="card config-section">
        <h2>⚙️ Configuration</h2>
        <div id="configInfo">
          <p style="margin-bottom: 12px;"><strong>Parallel Workers:</strong> <span id="parallelWorkers">--</span></p>
          <p style="margin-bottom: 12px;"><strong>Batch Size:</strong> <span id="batchSize">--</span></p>
          <p style="margin-bottom: 12px;"><strong>Source:</strong><br><span id="sourceConn">--</span></p>
          <p><strong>Destination:</strong><br><span id="destConn">--</span></p>
        </div>
      </div>
    </div>
    
    <div class="card tables-section">
      <h2>📋 Tables Status</h2>
      <div class="tables-container" id="tablesContainer">
        <!-- Tables will be populated here -->
      </div>
    </div>
  </div>
  <script>
    class PgcopyMonitor {
      constructor() {
        this.ws = null;
        this.state = null;
        this.reconnectAttempts = 0;
        this.maxReconnectAttempts = 5;
        this.completionAcked = false;
        this.connect();
      }

      connect() {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = protocol + '//' + window.location.host + '/ws';
        
        this.ws = new WebSocket(wsUrl);
        
        this.ws.onopen = () => {
          console.log('Connected to pgcopy WebSocket');
          this.reconnectAttempts = 0;
          this.updateConnectionStatus('Connected', true);
        };
        
        this.ws.onmessage = (event) => {
          const data = JSON.parse(event.data);
          this.handleStateUpdate(data);
        };
        
        this.ws.onclose = () => {
          console.log('Disconnected from pgcopy WebSocket');
          this.updateConnectionStatus('Disconnected', false);
          this.attemptReconnect();
        };
        
        this.ws.onerror = (error) => {
          console.error('WebSocket error:', error);
          this.updateConnectionStatus('Error', false);
        };
      }

      attemptReconnect() {
        if (this.reconnectAttempts < this.maxReconnectAttempts) {
          this.reconnectAttempts++;
          console.log('Attempting to reconnect... (' + this.reconnectAttempts + '/' + this.maxReconnectAttempts + ')');
          setTimeout(() => this.connect(), 2000 * this.reconnectAttempts);
        }
      }

      handleStateUpdate(data) {
        if (data.type === 'state_snapshot') {
          this.state = data.state;
          this.updateUI();
        } else if (data.type === 'state_event') {
          // Handle incremental updates
          this.handleStateEvent(data.event);
        }
        
        // Check if operation completed and send acknowledgment
        if (this.state && (this.state.status === 'completed' || this.state.status === 'failed') && !this.completionAcked) {
          this.sendCompletionAck();
        }
        
        this.updateConnectionStatus('Connected', true);
        document.getElementById('lastUpdate').textContent = new Date().toLocaleTimeString();
      }

      sendCompletionAck() {
        if (this.ws && this.ws.readyState === WebSocket.OPEN) {
          this.ws.send(JSON.stringify({
            type: 'completion_ack',
            timestamp: Date.now()
          }));
          this.completionAcked = true;
          console.log('Sent completion acknowledgment');
        }
      }

      handleStateEvent(event) {
        // Update UI based on specific events
        // This allows for more efficient updates
        console.log('State event:', event);
      }

      updateUI() {
        if (!this.state) return;

        // Update status indicator
        const statusIndicator = document.getElementById('statusIndicator');
        statusIndicator.className = 'status-indicator status-' + this.state.status;

        // Update overall progress
        const progress = this.state.summary.overallProgress || 0;
        document.getElementById('overallProgress').style.width = progress + '%';
        document.getElementById('progressText').textContent = progress.toFixed(1) + '% Complete';

        // Update stats
        document.getElementById('completedTables').textContent = this.state.summary.completedTables;
        document.getElementById('totalRows').textContent = this.formatNumber(this.state.summary.totalRows);
        document.getElementById('rowsPerSecond').textContent = this.formatNumber(Math.round(this.state.summary.overallSpeed || 0));
        
        const eta = this.state.summary.estimatedTimeLeft;
        document.getElementById('eta').textContent = eta ? this.formatDuration(eta) : '--';
        
        // Update total duration
        const elapsedTime = this.state.summary.elapsedTime;
        document.getElementById('totalDuration').textContent = elapsedTime ? this.formatDuration(elapsedTime) : '--';

        // Update config
        document.getElementById('parallelWorkers').textContent = this.state.config.parallel;
        document.getElementById('batchSize').textContent = this.state.config.batchSize;
        document.getElementById('sourceConn').textContent = this.state.connections.source.display || '--';
        document.getElementById('destConn').textContent = this.state.connections.destination.display || '--';

        // Update tables
        this.updateTablesUI();
      }

      updateTablesUI() {
        const container = document.getElementById('tablesContainer');
        container.innerHTML = '';

        // Sort tables: copying first, then completed, then pending, failed last
        const sortedTables = [...this.state.tables].sort((a, b) => {
          const statusOrder = {
            'copying': 1,
            'pending': 2,
            'completed': 3,
            'failed': 4
          };
          
          const orderA = statusOrder[a.status] || 5;
          const orderB = statusOrder[b.status] || 5;
          
          if (orderA !== orderB) {
            return orderA - orderB;
          }
          
          // For completed tables, sort by start time (earliest first)
          if (a.status === 'completed' && b.status === 'completed') {
            if (a.startTime && b.startTime) {
              return new Date(a.startTime) - new Date(b.startTime);
            }
          }
          
          // If same status, sort by table name
          return a.fullName.localeCompare(b.fullName);
        });

        sortedTables.forEach(table => {
          const item = document.createElement('div');
          item.className = 'table-item ' + table.status;
          
          const progress = table.progress || 0;
          const speed = table.speed ? this.formatNumber(Math.round(table.speed)) + '/s' : '';
          const duration = this.formatTableDuration(table.startTime, table.endTime);
          
          // Create compact stats display
          let statsHtml = '';
          if (table.status === 'copying') {
            statsHtml = '<div>' + this.formatNumber(table.syncedRows) + '/' + this.formatNumber(table.totalRows) + '</div>';
            if (speed) statsHtml += '<div style="color: #666;">' + speed + '</div>';
          } else if (table.status === 'completed') {
            const durationText = duration !== '--' ? ' (' + duration + ')' : '';
            statsHtml = '<div>' + this.formatNumber(table.totalRows) + '/' + this.formatNumber(table.totalRows) + durationText + '</div>';
          } else if (table.status === 'failed') {
            const durationText = duration !== '--' ? ' (' + duration + ')' : '';
            statsHtml = '<div>' + this.formatNumber(table.syncedRows) + '/' + this.formatNumber(table.totalRows) + durationText + '</div>';
          } else {
            statsHtml = '<div>' + this.formatNumber(table.totalRows) + ' rows</div>';
          }
          
          item.innerHTML = 
            '<div class="table-name">' + table.fullName + '</div>' +
            '<div class="table-progress">' +
              '<div class="table-stats">' + statsHtml + '</div>' +
              '<div class="progress-bar" style="width: 60px; height: 8px; margin-left: 8px;">' +
                '<div class="progress-fill" style="width: ' + progress + '%; height: 100%;"></div>' +
              '</div>' +
            '</div>';
          
          container.appendChild(item);
        });
      }

      updateConnectionStatus(status, connected) {
        const statusElement = document.getElementById('wsStatus');
        const container = document.getElementById('connectionStatus');
        
        statusElement.textContent = status;
        container.className = 'connection-status ' + (connected ? 'connected' : 'disconnected');
      }

      formatNumber(num) {
        if (num >= 1000000000) {
          return (num / 1000000000).toFixed(1) + 'B';
        } else if (num >= 1000000) {
          return (num / 1000000).toFixed(1) + 'M';
        } else if (num >= 1000) {
          return (num / 1000).toFixed(1) + 'K';
        } else {
          return num.toString();
        }
      }

      formatDuration(seconds) {
        if (seconds < 60) return Math.round(seconds) + 's';
        if (seconds < 3600) return Math.floor(seconds / 60) + 'm ' + Math.round(seconds % 60) + 's';
        return Math.floor(seconds / 3600) + 'h ' + Math.floor((seconds % 3600) / 60) + 'm';
      }

      formatTableDuration(startTime, endTime) {
        if (!startTime || !endTime) return '--';
        const duration = (new Date(endTime) - new Date(startTime)) / 1000;
        return this.formatDuration(duration);
      }
    }

    // Start the monitor when page loads
    document.addEventListener('DOMContentLoaded', () => {
      new PgcopyMonitor();
    });
  </script>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html")
	_, _ = w.Write([]byte(html))
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
