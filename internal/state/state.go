// Package state provides a centralized state management system for pgcopy operations
// Following a React-like hooks pattern for state updates and subscriptions
package state

import (
	"sync"
	"time"
)

// CopyState represents the complete state of a pgcopy operation
type CopyState struct {
	mu sync.RWMutex
	// Management fields
	listeners []Listener

	// Operation metadata
	ID          string            `json:"id"`
	Status      OperationStatus   `json:"status"`
	StartTime   time.Time         `json:"startTime"`
	EndTime     *time.Time        `json:"endTime,omitempty"`
	Config      OperationConfig   `json:"config"`
	Connections ConnectionDetails `json:"connections"`

	// Progress tracking
	Tables   []TableState `json:"tables"`
	Summary  Summary      `json:"summary"`
	Logs     []LogEntry   `json:"logs"`
	Errors   []ErrorEntry `json:"errors"`
	Warnings []Warning    `json:"warnings"`

	// Foreign key management
	ForeignKeys ForeignKeyState `json:"foreignKeys"`

	// Real-time metrics
	Metrics Metrics `json:"metrics"`
}

// OperationStatus represents the current status of the operation
type OperationStatus string

const (
	// StatusInitializing indicates the operation is being initialized
	StatusInitializing OperationStatus = "initializing"
	// StatusConfirming indicates the operation is waiting for user confirmation
	StatusConfirming OperationStatus = "confirming"
	// StatusPreparing indicates the operation is being prepared
	StatusPreparing OperationStatus = "preparing"
	// StatusCopying indicates the operation is actively copying data
	StatusCopying OperationStatus = "copying"
	// StatusCompleted indicates the operation has completed successfully
	StatusCompleted OperationStatus = "completed"
	// StatusFailed indicates the operation has failed
	StatusFailed OperationStatus = "failed"
	// StatusCancelled indicates the operation was cancelled by user
	StatusCancelled OperationStatus = "cancelled"
)

// OperationConfig holds the configuration for the operation
type OperationConfig struct {
	// Connection settings
	SourceConn string `json:"sourceConn"`
	DestConn   string `json:"destConn"`

	// Operation settings
	Parallel      int      `json:"parallel"`
	BatchSize     int      `json:"batchSize"`
	IncludeTables []string `json:"includeTables"`
	ExcludeTables []string `json:"excludeTables"`
	DryRun        bool     `json:"dryRun"`
	SkipBackup    bool     `json:"skipBackup"`
	OutputMode    string   `json:"outputMode"`
	UseCopyPipe   bool     `json:"useCopyPipe"`  // Stream source->dest with COPY ... TO STDOUT / FROM STDIN
	CompressPipe  bool     `json:"compressPipe"` // Gzip compress COPY stream in-flight (local pipe)

	// Row count behavior
	// When true, pgcopy will compute exact row counts using COUNT(*) per table during discovery.
	// This is slower but avoids inaccurate estimates (e.g., after TRUNCATE).
	ExactRows bool `json:"exactRows"`

	// Unlimited timeouts: when true, pgcopy won't set deadlines on internal DB operations
	// (best-effort; still subject to external server timeouts)
	NoTimeouts bool `json:"noTimeouts"`
}

// ConnectionDetails holds information about source and destination connections
type ConnectionDetails struct {
	Source      ConnectionInfo `json:"source"`
	Destination ConnectionInfo `json:"destination"`
}

// ConnectionInfo represents connection information
type ConnectionInfo struct {
	Display      string            `json:"display"`
	Host         string            `json:"host"`
	Port         string            `json:"port"`
	Database     string            `json:"database"`
	IsFile       bool              `json:"isFile"`
	FilePath     string            `json:"filePath,omitempty"`
	ConnectionID string            `json:"connectionId"`
	Status       ConnectionStatus  `json:"status"`
	LastPing     *time.Time        `json:"lastPing,omitempty"`
	Metadata     map[string]string `json:"metadata"`
}

// ConnectionStatus represents the status of a database connection
type ConnectionStatus string

const (
	// ConnectionStatusUnknown indicates the connection status is unknown
	ConnectionStatusUnknown ConnectionStatus = "unknown"
	// ConnectionStatusConnecting indicates the connection is being established
	ConnectionStatusConnecting ConnectionStatus = "connecting"
	// ConnectionStatusConnected indicates the connection is active
	ConnectionStatusConnected ConnectionStatus = "connected"
	// ConnectionStatusDisconnected indicates the connection has been disconnected
	ConnectionStatusDisconnected ConnectionStatus = "disconnected"
	// ConnectionStatusError indicates the connection has an error
	ConnectionStatusError ConnectionStatus = "error"
)

// TableState represents the state of a single table during copying
type TableState struct {
	// Identity
	Schema   string `json:"schema"`
	Name     string `json:"name"`
	FullName string `json:"fullName"`

	// Metadata
	Columns       []string     `json:"columns"`
	PKColumns     []string     `json:"pkColumns"`
	HasForeignKey bool         `json:"hasForeignKey"`
	ForeignKeys   []ForeignKey `json:"foreignKeys"`
	Dependencies  []string     `json:"dependencies"`
	Dependents    []string     `json:"dependents"`

	// Row counts
	TotalRows   int64 `json:"totalRows"`
	SyncedRows  int64 `json:"syncedRows"`
	ErrorRows   int64 `json:"errorRows"`
	SkippedRows int64 `json:"skippedRows"`

	// Status and timing
	Status    TableStatus `json:"status"`
	StartTime *time.Time  `json:"startTime,omitempty"`
	EndTime   *time.Time  `json:"endTime,omitempty"`
	Duration  *int64      `json:"duration,omitempty"` // milliseconds

	// Progress and performance
	Progress     float64 `json:"progress"`      // 0-100
	Speed        float64 `json:"speed"`         // rows per second
	ETA          *int64  `json:"eta,omitempty"` // seconds remaining
	BatchesDone  int     `json:"batchesDone"`
	BatchesTotal int     `json:"batchesTotal"`

	// Error tracking
	Errors   []TableError `json:"errors"`
	Warnings []Warning    `json:"warnings"`

	// Worker assignment
	WorkerID   int  `json:"workerId"`
	IsParallel bool `json:"isParallel"`

	// Retry logic
	RetryCount    int        `json:"retryCount"`
	MaxRetries    int        `json:"maxRetries"`
	LastRetry     *time.Time `json:"lastRetry,omitempty"`
	RetryStrategy string     `json:"retryStrategy"`
}

// TableStatus represents the current status of a table
type TableStatus string

const (
	// TableStatusPending indicates the table is pending processing
	TableStatusPending TableStatus = "pending"
	// TableStatusQueued indicates the table is queued for processing
	TableStatusQueued TableStatus = "queued"
	// TableStatusCopying indicates the table is currently being copied
	TableStatusCopying TableStatus = "copying"
	// TableStatusCompleted indicates the table has been copied successfully
	TableStatusCompleted TableStatus = "completed"
	// TableStatusFailed indicates the table copy failed
	TableStatusFailed TableStatus = "failed"
	// TableStatusSkipped indicates the table was skipped
	TableStatusSkipped TableStatus = "skipped"
	// TableStatusRetrying indicates the table is being retried after failure
	TableStatusRetrying TableStatus = "retrying"
	// TableStatusCancelled indicates the table copy was cancelled
	TableStatusCancelled TableStatus = "cancelled"
)

// ForeignKey represents a foreign key constraint
type ForeignKey struct {
	ConstraintName string   `json:"constraintName"`
	SourceTable    string   `json:"sourceTable"`
	SourceColumns  []string `json:"sourceColumns"`
	TargetTable    string   `json:"targetTable"`
	TargetColumns  []string `json:"targetColumns"`
	OnDelete       string   `json:"onDelete"`
	OnUpdate       string   `json:"onUpdate"`
	IsDeferred     bool     `json:"isDeferred"`
	Definition     string   `json:"definition"`
}

// ForeignKeyState tracks the state of foreign key management
type ForeignKeyState struct {
	Mode               string       `json:"mode"` // "replica" or "drop_restore"
	IsUsingReplicaMode bool         `json:"isUsingReplicaMode"`
	TotalFKs           int          `json:"totalFks"`
	DroppedFKs         int          `json:"droppedFks"`
	RestoredFKs        int          `json:"restoredFks"`
	FailedRestores     int          `json:"failedRestores"`
	ForeignKeys        []ForeignKey `json:"foreignKeys"`
	BackupFile         string       `json:"backupFile"`
	HasBackupFile      bool         `json:"hasBackupFile"`
	BackupCreated      *time.Time   `json:"backupCreated,omitempty"`
	Status             string       `json:"status"`
}

// Summary provides overall statistics
type Summary struct {
	TotalTables       int     `json:"totalTables"`
	CompletedTables   int     `json:"completedTables"`
	FailedTables      int     `json:"failedTables"`
	SkippedTables     int     `json:"skippedTables"`
	TotalRows         int64   `json:"totalRows"`
	SyncedRows        int64   `json:"syncedRows"`
	ErrorRows         int64   `json:"errorRows"`
	OverallProgress   float64 `json:"overallProgress"`             // 0-100
	OverallSpeed      float64 `json:"overallSpeed"`                // rows per second
	EstimatedTimeLeft *int64  `json:"estimatedTimeLeft,omitempty"` // seconds
	ElapsedTime       int64   `json:"elapsedTime"`                 // seconds
}

// LogEntry represents a log message
type LogEntry struct {
	ID        string         `json:"id"`
	Timestamp time.Time      `json:"timestamp"`
	Level     LogLevel       `json:"level"`
	Message   string         `json:"message"`
	Table     string         `json:"table,omitempty"`
	Component string         `json:"component"` // "copier", "fk_manager", "validator", etc.
	Context   map[string]any `json:"context,omitempty"`
}

// LogLevel represents log severity
type LogLevel string

const (
	// LogLevelDebug indicates debug-level log messages
	LogLevelDebug LogLevel = "debug"
	// LogLevelInfo indicates informational log messages
	LogLevelInfo LogLevel = "info"
	// LogLevelWarn indicates warning log messages
	LogLevelWarn LogLevel = "warn"
	// LogLevelError indicates error log messages
	LogLevelError LogLevel = "error"
)

// ErrorEntry represents an error that occurred during the operation
type ErrorEntry struct {
	ID        string         `json:"id"`
	Timestamp time.Time      `json:"timestamp"`
	Type      string         `json:"type"`
	Message   string         `json:"message"`
	Table     string         `json:"table,omitempty"`
	Component string         `json:"component"`
	Context   map[string]any `json:"context,omitempty"`
	Stack     string         `json:"stack,omitempty"`
	IsFatal   bool           `json:"isFatal"`
	IsRetried bool           `json:"isRetried"`
}

// TableError represents an error specific to a table operation
type TableError struct {
	ID        string         `json:"id"`
	Timestamp time.Time      `json:"timestamp"`
	Type      string         `json:"type"`
	Message   string         `json:"message"`
	RowNumber *int64         `json:"rowNumber,omitempty"`
	Context   map[string]any `json:"context,omitempty"`
	IsRetried bool           `json:"isRetried"`
}

// Warning represents a warning message
type Warning struct {
	ID        string         `json:"id"`
	Timestamp time.Time      `json:"timestamp"`
	Type      string         `json:"type"`
	Message   string         `json:"message"`
	Table     string         `json:"table,omitempty"`
	Context   map[string]any `json:"context,omitempty"`
}

// Metrics holds real-time performance metrics
type Metrics struct {
	// Performance metrics
	RowsPerSecond     float64 `json:"rowsPerSecond"`
	TablesPerMinute   float64 `json:"tablesPerMinute"`
	AverageTableTime  float64 `json:"averageTableTime"` // seconds
	PeakRowsPerSecond float64 `json:"peakRowsPerSecond"`

	// Resource usage
	ActiveWorkers   int     `json:"activeWorkers"`
	QueuedTables    int     `json:"queuedTables"`
	MemoryUsageMB   float64 `json:"memoryUsageMb"`
	CPUUsagePercent float64 `json:"cpuUsagePercent"`

	// Database metrics
	SourceConnections int `json:"sourceConnections"`
	DestConnections   int `json:"destConnections"`
	ActiveQueries     int `json:"activeQueries"`

	// Network metrics
	NetworkLatencyMS  float64 `json:"networkLatencyMs"`
	DataTransferredMB float64 `json:"dataTransferredMb"`

	// Historical data for charts (last 60 data points)
	RowsPerSecondHistory []MetricPoint `json:"rowsPerSecondHistory"`
	MemoryHistory        []MetricPoint `json:"memoryHistory"`
	CPUHistory           []MetricPoint `json:"cpuHistory"`
}

// MetricPoint represents a single data point for historical metrics
type MetricPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
}

// Listener defines the interface for state change listeners
type Listener interface {
	OnStateChange(state *CopyState, event Event)
}

// Event represents different types of state changes
type Event struct {
	Type      EventType      `json:"type"`
	Timestamp time.Time      `json:"timestamp"`
	Data      map[string]any `json:"data,omitempty"`
	TableName string         `json:"tableName,omitempty"`
}

// EventType represents the type of state change event
type EventType string

const (
	// EventOperationStarted indicates the copy operation has started
	EventOperationStarted EventType = "operation_started"
	// EventOperationCompleted indicates the copy operation has completed successfully
	EventOperationCompleted EventType = "operation_completed"
	// EventOperationFailed indicates the copy operation has failed
	EventOperationFailed EventType = "operation_failed"
	// EventTableStarted indicates a table copy has started
	EventTableStarted EventType = "table_started"
	// EventTableCompleted indicates a table copy has completed
	EventTableCompleted EventType = "table_completed"
	// EventTableFailed indicates a table copy has failed
	EventTableFailed EventType = "table_failed"
	// EventTableProgress indicates progress update for a table
	EventTableProgress EventType = "table_progress"
	// EventFKDetected indicates a foreign key was detected
	EventFKDetected EventType = "fk_detected"
	// EventFKDropped indicates a foreign key was dropped
	EventFKDropped EventType = "fk_dropped"
	// EventFKRestored indicates a foreign key was restored
	EventFKRestored EventType = "fk_restored"
	// EventLogAdded indicates a new log entry was added
	EventLogAdded EventType = "log_added"
	// EventErrorAdded indicates a new error was added
	EventErrorAdded EventType = "error_added"
	// EventWarningAdded indicates a new warning was added
	EventWarningAdded EventType = "warning_added"
	// EventMetricsUpdated indicates metrics were updated
	EventMetricsUpdated EventType = "metrics_updated"
	// EventConfigChanged indicates configuration was changed
	EventConfigChanged EventType = "config_changed"
)

// NewCopyState creates a new copy state instance
func NewCopyState(id string, config OperationConfig) *CopyState {
	return &CopyState{
		ID:        id,
		Status:    StatusInitializing,
		StartTime: time.Now(),
		Config:    config,
		Tables:    make([]TableState, 0),
		Logs:      make([]LogEntry, 0),
		Errors:    make([]ErrorEntry, 0),
		Warnings:  make([]Warning, 0),
		ForeignKeys: ForeignKeyState{
			ForeignKeys: make([]ForeignKey, 0),
		},
		Metrics: Metrics{
			RowsPerSecondHistory: make([]MetricPoint, 0, 60),
			MemoryHistory:        make([]MetricPoint, 0, 60),
			CPUHistory:           make([]MetricPoint, 0, 60),
		},
		listeners: make([]Listener, 0),
	}
}

// Subscribe adds a state listener
func (s *CopyState) Subscribe(listener Listener) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, listener)
}

// Unsubscribe removes a state listener
func (s *CopyState) Unsubscribe(listener Listener) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, l := range s.listeners {
		if l == listener {
			s.listeners = append(s.listeners[:i], s.listeners[i+1:]...)
			break
		}
	}
}

// emit notifies all listeners of a state change
func (s *CopyState) emit(event Event) {
	s.mu.RLock()
	listeners := make([]Listener, len(s.listeners))
	copy(listeners, s.listeners)
	s.mu.RUnlock()

	for _, listener := range listeners {
		go listener.OnStateChange(s, event)
	}
}

// CopyStateSnapshot represents a read-only snapshot of CopyState without mutex
type CopyStateSnapshot struct {
	// Operation metadata
	ID          string            `json:"id"`
	Status      OperationStatus   `json:"status"`
	StartTime   time.Time         `json:"startTime"`
	EndTime     *time.Time        `json:"endTime,omitempty"`
	Config      OperationConfig   `json:"config"`
	Connections ConnectionDetails `json:"connections"`

	// Progress tracking
	Tables   []TableState `json:"tables"`
	Summary  Summary      `json:"summary"`
	Logs     []LogEntry   `json:"logs"`
	Errors   []ErrorEntry `json:"errors"`
	Warnings []Warning    `json:"warnings"`

	// Foreign key management
	ForeignKeys ForeignKeyState `json:"foreignKeys"`

	// Real-time metrics
	Metrics Metrics `json:"metrics"`
}

// GetSnapshot returns a read-only snapshot of the current state
func (s *CopyState) GetSnapshot() CopyStateSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Create a snapshot without the mutex
	snapshot := CopyStateSnapshot{
		ID:          s.ID,
		Status:      s.Status,
		StartTime:   s.StartTime,
		EndTime:     s.EndTime,
		Config:      s.Config,
		Connections: s.Connections,
		Summary:     s.Summary,
		ForeignKeys: s.ForeignKeys,
		Metrics:     s.Metrics,
	}

	// Deep copy slices
	snapshot.Tables = make([]TableState, len(s.Tables))
	copy(snapshot.Tables, s.Tables)

	snapshot.Logs = make([]LogEntry, len(s.Logs))
	copy(snapshot.Logs, s.Logs)

	snapshot.Errors = make([]ErrorEntry, len(s.Errors))
	copy(snapshot.Errors, s.Errors)

	snapshot.Warnings = make([]Warning, len(s.Warnings))
	copy(snapshot.Warnings, s.Warnings)

	return snapshot
}
