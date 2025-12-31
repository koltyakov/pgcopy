// Package state provides React-like hooks for state management
package state

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// generateID creates a simple unique ID
func generateID() string {
	bytes := make([]byte, 8)
	// Intentionally ignoring error - random ID generation failure is not critical
	_, _ = rand.Read(bytes) // #nosec G104 - error handling not critical for ID generation
	return hex.EncodeToString(bytes)
}

// validOperationTransitions defines allowed state transitions for operations.
// This prevents invalid state changes (e.g., completed -> copying) that could
// indicate bugs or race conditions.
var validOperationTransitions = map[OperationStatus][]OperationStatus{
	StatusInitializing: {StatusPreparing, StatusConfirming, StatusFailed, StatusCancelled},
	StatusConfirming:   {StatusPreparing, StatusCancelled},
	StatusPreparing:    {StatusCopying, StatusCompleted, StatusFailed, StatusCancelled},
	StatusCopying:      {StatusCompleted, StatusFailed, StatusCancelled},
	StatusCompleted:    {}, // Terminal state - no transitions allowed
	StatusFailed:       {}, // Terminal state - no transitions allowed
	StatusCancelled:    {}, // Terminal state - no transitions allowed
}

// validTableTransitions defines allowed state transitions for tables.
var validTableTransitions = map[TableStatus][]TableStatus{
	TableStatusPending:   {TableStatusQueued, TableStatusCopying, TableStatusSkipped, TableStatusCancelled},
	TableStatusQueued:    {TableStatusCopying, TableStatusSkipped, TableStatusCancelled},
	TableStatusCopying:   {TableStatusCompleted, TableStatusFailed, TableStatusRetrying, TableStatusCancelled},
	TableStatusRetrying:  {TableStatusCopying, TableStatusFailed, TableStatusCancelled},
	TableStatusCompleted: {},                    // Terminal state
	TableStatusFailed:    {TableStatusRetrying}, // Can retry from failed
	TableStatusSkipped:   {},                    // Terminal state
	TableStatusCancelled: {},                    // Terminal state
}

// isValidOperationTransition checks if transitioning from old to new status is valid.
func isValidOperationTransition(old, new OperationStatus) bool {
	allowed, exists := validOperationTransitions[old]
	if !exists {
		return false
	}
	for _, s := range allowed {
		if s == new {
			return true
		}
	}
	return false
}

// isValidTableTransition checks if transitioning from old to new status is valid.
func isValidTableTransition(old, new TableStatus) bool {
	allowed, exists := validTableTransitions[old]
	if !exists {
		return false
	}
	for _, s := range allowed {
		if s == new {
			return true
		}
	}
	return false
}

// Hook methods for updating state (React-like API)

// SetStatus updates the operation status with validation
func (s *CopyState) SetStatus(status OperationStatus) {
	var event Event

	s.mu.Lock()
	oldStatus := s.Status

	// Validate state transition
	if oldStatus != status && !isValidOperationTransition(oldStatus, status) {
		// Log warning but allow transition for robustness
		// This helps identify bugs without breaking functionality
		fmt.Printf("Warning: Invalid operation state transition: %s -> %s\n", oldStatus, status)
	}

	s.Status = status

	if status == StatusCompleted || status == StatusFailed || status == StatusCancelled {
		now := time.Now()
		s.EndTime = &now
	}

	// Create event while holding lock
	event = Event{
		Type:      EventOperationStarted,
		Timestamp: time.Now(),
		Data: map[string]any{
			"oldStatus": oldStatus,
			"newStatus": status,
		},
	}
	s.mu.Unlock()

	// Emit event after releasing lock
	s.emit(event)
}

// AddTable adds a new table to the state
func (s *CopyState) AddTable(schema, name string, totalRows int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	table := TableState{
		Schema:       schema,
		Name:         name,
		FullName:     fmt.Sprintf("%s.%s", schema, name),
		TotalRows:    totalRows,
		Status:       TableStatusPending,
		Progress:     0,
		MaxRetries:   3,
		Columns:      make([]string, 0),
		PKColumns:    make([]string, 0),
		ForeignKeys:  make([]ForeignKey, 0),
		Dependencies: make([]string, 0),
		Dependents:   make([]string, 0),
		Errors:       make([]TableError, 0),
		Warnings:     make([]Warning, 0),
	}

	s.Tables = append(s.Tables, table)
	s.Summary.TotalTables++
	s.Summary.TotalRows += totalRows

	s.updateSummary()
}

// UpdateTableStatus updates the status of a specific table
func (s *CopyState) UpdateTableStatus(schema, name string, status TableStatus) {
	var event Event

	s.mu.Lock()
	tableIndex := s.findTableIndex(schema, name)
	if tableIndex == -1 {
		s.mu.Unlock()
		return
	}

	table := &s.Tables[tableIndex]
	oldStatus := table.Status

	// Validate state transition
	if oldStatus != status && !isValidTableTransition(oldStatus, status) {
		// Log warning but allow transition for robustness
		fmt.Printf("Warning: Invalid table state transition for %s.%s: %s -> %s\n", schema, name, oldStatus, status)
	}

	table.Status = status

	now := time.Now()

	switch status {
	case TableStatusCopying:
		table.StartTime = &now
		table.WorkerID = s.assignWorker()
	case TableStatusCompleted:
		table.EndTime = &now
		table.Progress = 100
		if table.StartTime != nil {
			duration := now.Sub(*table.StartTime).Milliseconds()
			table.Duration = &duration
		}
		s.Summary.CompletedTables++
	case TableStatusFailed:
		table.EndTime = &now
		s.Summary.FailedTables++
	case TableStatusSkipped:
		s.Summary.SkippedTables++
	}

	s.updateSummary()

	var eventType EventType
	switch status {
	case TableStatusCopying:
		eventType = EventTableStarted
	case TableStatusCompleted:
		eventType = EventTableCompleted
	case TableStatusFailed:
		eventType = EventTableFailed
	default:
		eventType = EventTableProgress
	}

	// Create event while holding lock
	event = Event{
		Type:      eventType,
		Timestamp: now,
		TableName: table.FullName,
		Data: map[string]any{
			"oldStatus": oldStatus,
			"newStatus": status,
		},
	}
	s.mu.Unlock()

	// Emit event after releasing lock
	s.emit(event)
}

// UpdateTableProgress updates the progress of a specific table
func (s *CopyState) UpdateTableProgress(schema, name string, syncedRows int64) {
	var event Event

	s.mu.Lock()
	tableIndex := s.findTableIndex(schema, name)
	if tableIndex == -1 {
		s.mu.Unlock()
		return
	}

	table := &s.Tables[tableIndex]
	oldSyncedRows := table.SyncedRows
	table.SyncedRows = syncedRows

	if table.TotalRows > 0 {
		table.Progress = float64(syncedRows) / float64(table.TotalRows) * 100
	}

	// Calculate speed if we have timing information
	if table.StartTime != nil {
		elapsed := time.Since(*table.StartTime).Seconds()
		if elapsed > 0 {
			table.Speed = float64(syncedRows) / elapsed

			// Calculate ETA
			if table.Progress > 0 && table.Progress < 100 {
				remainingRows := table.TotalRows - syncedRows
				if table.Speed > 0 {
					eta := int64(float64(remainingRows) / table.Speed)
					table.ETA = &eta
				}
			}
		}
	}

	// Update overall summary
	s.Summary.SyncedRows += (syncedRows - oldSyncedRows)
	s.updateSummary()

	// Create event while holding lock
	event = Event{
		Type:      EventTableProgress,
		Timestamp: time.Now(),
		TableName: table.FullName,
		Data: map[string]any{
			"syncedRows": syncedRows,
			"progress":   table.Progress,
			"speed":      table.Speed,
		},
	}
	s.mu.Unlock()

	// Emit event after releasing lock
	s.emit(event)
}

// AddTableError adds an error to a specific table
func (s *CopyState) AddTableError(schema, name string, errorMsg string, context map[string]any) {
	var event Event

	s.mu.Lock()
	tableIndex := s.findTableIndex(schema, name)
	if tableIndex == -1 {
		s.mu.Unlock()
		return
	}

	table := &s.Tables[tableIndex]

	tableError := TableError{
		ID:        generateID(),
		Timestamp: time.Now(),
		Type:      "table_error",
		Message:   errorMsg,
		Context:   context,
	}

	table.Errors = append(table.Errors, tableError)
	table.ErrorRows++

	// Create event while holding lock
	event = Event{
		Type:      EventErrorAdded,
		Timestamp: time.Now(),
		TableName: table.FullName,
		Data: map[string]any{
			"error": tableError,
		},
	}
	s.mu.Unlock()

	// Emit event after releasing lock
	s.emit(event)
}

// AddLog adds a log entry to the state
func (s *CopyState) AddLog(level LogLevel, message string, component string, table string, context map[string]any) {
	var event Event

	s.mu.Lock()
	logEntry := LogEntry{
		ID:        generateID(),
		Timestamp: time.Now(),
		Level:     level,
		Message:   message,
		Table:     table,
		Component: component,
		Context:   context,
	}

	s.Logs = append(s.Logs, logEntry)

	// Keep only last 1000 log entries to prevent memory issues
	if len(s.Logs) > 1000 {
		s.Logs = s.Logs[len(s.Logs)-1000:]
	}

	// Create event while holding lock
	event = Event{
		Type:      EventLogAdded,
		Timestamp: time.Now(),
		Data: map[string]any{
			"log": logEntry,
		},
	}
	s.mu.Unlock()

	// Emit event after releasing lock
	s.emit(event)
}

// AddError adds a global error to the state
func (s *CopyState) AddError(errorType, message, component string, isFatal bool, context map[string]any) {
	var event Event

	s.mu.Lock()
	errorEntry := ErrorEntry{
		ID:        generateID(),
		Timestamp: time.Now(),
		Type:      errorType,
		Message:   message,
		Component: component,
		Context:   context,
		IsFatal:   isFatal,
	}

	s.Errors = append(s.Errors, errorEntry)

	// Create event while holding lock
	event = Event{
		Type:      EventErrorAdded,
		Timestamp: time.Now(),
		Data: map[string]any{
			"error": errorEntry,
		},
	}
	s.mu.Unlock()

	// Emit event after releasing lock
	s.emit(event)
}

// UpdateForeignKeys updates the foreign key state
func (s *CopyState) UpdateForeignKeys(mode string, isUsingReplica bool, totalFKs int) {
	var event Event

	s.mu.Lock()
	s.ForeignKeys.Mode = mode
	s.ForeignKeys.IsUsingReplicaMode = isUsingReplica
	s.ForeignKeys.TotalFKs = totalFKs

	// Create event while holding lock
	event = Event{
		Type:      EventFKDetected,
		Timestamp: time.Now(),
		Data: map[string]any{
			"mode":        mode,
			"totalFKs":    totalFKs,
			"replicaMode": isUsingReplica,
		},
	}
	s.mu.Unlock()

	// Emit event after releasing lock
	s.emit(event)
}

// AddForeignKey adds a foreign key to the state
func (s *CopyState) AddForeignKey(fk ForeignKey) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ForeignKeys.ForeignKeys = append(s.ForeignKeys.ForeignKeys, fk)

	// Update table dependencies
	for i := range s.Tables {
		if s.Tables[i].FullName == fk.SourceTable {
			s.Tables[i].HasForeignKey = true
			s.Tables[i].ForeignKeys = append(s.Tables[i].ForeignKeys, fk)
			s.Tables[i].Dependencies = append(s.Tables[i].Dependencies, fk.TargetTable)
		}
		if s.Tables[i].FullName == fk.TargetTable {
			s.Tables[i].Dependents = append(s.Tables[i].Dependents, fk.SourceTable)
		}
	}
}

// UpdateMetrics updates the real-time metrics
func (s *CopyState) UpdateMetrics(metrics Metrics) {
	var event Event

	s.mu.Lock()
	s.Metrics = metrics

	// Add to historical data
	now := time.Now()
	point := MetricPoint{Timestamp: now, Value: metrics.RowsPerSecond}
	s.Metrics.RowsPerSecondHistory = append(s.Metrics.RowsPerSecondHistory, point)

	// Keep only last 60 points (for 1-minute history if updated every second)
	if len(s.Metrics.RowsPerSecondHistory) > 60 {
		s.Metrics.RowsPerSecondHistory = s.Metrics.RowsPerSecondHistory[1:]
	}

	// Create event while holding lock
	event = Event{
		Type:      EventMetricsUpdated,
		Timestamp: now,
		Data: map[string]any{
			"metrics": metrics,
		},
	}
	s.mu.Unlock()

	// Emit event after releasing lock
	s.emit(event)
}

// UpdateConnectionDetails updates connection information
func (s *CopyState) UpdateConnectionDetails(connType, display string, status ConnectionStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	switch connType {
	case "source":
		s.Connections.Source.Display = display
		s.Connections.Source.Status = status
		s.Connections.Source.LastPing = &now
	case "destination":
		s.Connections.Destination.Display = display
		s.Connections.Destination.Status = status
		s.Connections.Destination.LastPing = &now
	}
}

// Helper methods

// findTableIndex finds the index of a table in the tables slice
func (s *CopyState) findTableIndex(schema, name string) int {
	fullName := fmt.Sprintf("%s.%s", schema, name)
	for i, table := range s.Tables {
		if table.FullName == fullName {
			return i
		}
	}
	return -1
}

// assignWorker assigns a worker ID to a table (simple round-robin for now)
func (s *CopyState) assignWorker() int {
	// Count active workers
	activeWorkers := 0
	for _, table := range s.Tables {
		if table.Status == TableStatusCopying {
			activeWorkers++
		}
	}
	return (activeWorkers % s.Config.Parallel) + 1
}

// updateSummary recalculates the summary statistics
func (s *CopyState) updateSummary() {
	summary := &s.Summary

	// Calculate overall progress
	if summary.TotalRows > 0 {
		summary.OverallProgress = float64(summary.SyncedRows) / float64(summary.TotalRows) * 100
	}

	// Calculate overall speed
	elapsed := time.Since(s.StartTime).Seconds()
	if elapsed > 0 {
		summary.OverallSpeed = float64(summary.SyncedRows) / elapsed

		// Calculate ETA
		if summary.OverallProgress > 0 && summary.OverallProgress < 100 {
			remainingRows := summary.TotalRows - summary.SyncedRows
			if summary.OverallSpeed > 0 {
				eta := int64(float64(remainingRows) / summary.OverallSpeed)
				summary.EstimatedTimeLeft = &eta
			}
		}
	}

	summary.ElapsedTime = int64(elapsed)

	// Update metrics
	s.Metrics.ActiveWorkers = 0
	s.Metrics.QueuedTables = 0

	for _, table := range s.Tables {
		switch table.Status {
		case TableStatusCopying:
			s.Metrics.ActiveWorkers++
		case TableStatusQueued, TableStatusPending:
			s.Metrics.QueuedTables++
		}
	}
}

// GetTablesByStatus returns a slice of table names that have the specified status
func (s *CopyState) GetTablesByStatus(status TableStatus) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tables := make([]string, 0)
	for _, table := range s.Tables {
		if table.Status == status {
			tables = append(tables, fmt.Sprintf("%s.%s", table.Schema, table.Name))
		}
	}
	return tables
}

// GetAllTables returns a copy of all tables in the state
func (s *CopyState) GetAllTables() []TableState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tables := make([]TableState, len(s.Tables))
	copy(tables, s.Tables)
	return tables
}
