package state

import (
	"sync"
	"testing"
	"time"
)

func TestNewCopyState(t *testing.T) {
	config := OperationConfig{
		Parallel:  4,
		BatchSize: 1000,
		DryRun:    false,
	}

	state := NewCopyState("test-id", config)

	if state.ID != "test-id" {
		t.Errorf("Expected ID 'test-id', got '%s'", state.ID)
	}
	if state.Status != StatusInitializing {
		t.Errorf("Expected status Initializing, got '%s'", state.Status)
	}
	if state.Config.Parallel != 4 {
		t.Errorf("Expected parallel 4, got %d", state.Config.Parallel)
	}
	if state.Config.BatchSize != 1000 {
		t.Errorf("Expected batch size 1000, got %d", state.Config.BatchSize)
	}
	if state.Tables == nil {
		t.Error("Expected Tables to be initialized")
	}
	if state.Logs == nil {
		t.Error("Expected Logs to be initialized")
	}
	if state.Errors == nil {
		t.Error("Expected Errors to be initialized")
	}
}

func TestCopyState_SetStatus(t *testing.T) {
	state := NewCopyState("test", OperationConfig{Parallel: 4})

	// Test valid transition: initializing -> preparing
	state.SetStatus(StatusPreparing)
	if state.Status != StatusPreparing {
		t.Errorf("Expected status Preparing, got '%s'", state.Status)
	}

	// Test valid transition: preparing -> copying
	state.SetStatus(StatusCopying)
	if state.Status != StatusCopying {
		t.Errorf("Expected status Copying, got '%s'", state.Status)
	}

	// Test valid transition: copying -> completed
	state.SetStatus(StatusCompleted)
	if state.Status != StatusCompleted {
		t.Errorf("Expected status Completed, got '%s'", state.Status)
	}
	if state.EndTime == nil {
		t.Error("Expected EndTime to be set for completed status")
	}
}

func TestCopyState_SetStatus_Terminal(t *testing.T) {
	tests := []struct {
		name           string
		terminalStatus OperationStatus
	}{
		{"completed", StatusCompleted},
		{"failed", StatusFailed},
		{"cancelled", StatusCancelled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := NewCopyState("test", OperationConfig{Parallel: 4})
			state.SetStatus(StatusPreparing)
			state.SetStatus(StatusCopying)
			state.SetStatus(tt.terminalStatus)

			if state.EndTime == nil {
				t.Error("Expected EndTime to be set for terminal status")
			}
		})
	}
}

func TestCopyState_AddTable(t *testing.T) {
	state := NewCopyState("test", OperationConfig{Parallel: 4})

	state.AddTable("public", "users", 1000)

	if len(state.Tables) != 1 {
		t.Fatalf("Expected 1 table, got %d", len(state.Tables))
	}

	table := state.Tables[0]
	if table.Schema != "public" {
		t.Errorf("Expected schema 'public', got '%s'", table.Schema)
	}
	if table.Name != "users" {
		t.Errorf("Expected name 'users', got '%s'", table.Name)
	}
	const expectedFullName = "public.users"
	if table.FullName != expectedFullName {
		t.Errorf("Expected full name '%s', got '%s'", expectedFullName, table.FullName)
	}
	if table.TotalRows != 1000 {
		t.Errorf("Expected total rows 1000, got %d", table.TotalRows)
	}
	if table.Status != TableStatusPending {
		t.Errorf("Expected status Pending, got '%s'", table.Status)
	}
	if state.Summary.TotalTables != 1 {
		t.Errorf("Expected total tables 1, got %d", state.Summary.TotalTables)
	}
	if state.Summary.TotalRows != 1000 {
		t.Errorf("Expected total rows 1000, got %d", state.Summary.TotalRows)
	}
}

func TestCopyState_UpdateTableStatus(t *testing.T) {
	state := NewCopyState("test", OperationConfig{Parallel: 4})
	state.AddTable("public", "users", 1000)

	// Update to copying
	state.UpdateTableStatus("public", "users", TableStatusCopying)
	if state.Tables[0].Status != TableStatusCopying {
		t.Errorf("Expected status Copying, got '%s'", state.Tables[0].Status)
	}
	if state.Tables[0].StartTime == nil {
		t.Error("Expected StartTime to be set when copying")
	}

	// Update to completed
	state.UpdateTableStatus("public", "users", TableStatusCompleted)
	if state.Tables[0].Status != TableStatusCompleted {
		t.Errorf("Expected status Completed, got '%s'", state.Tables[0].Status)
	}
	if state.Tables[0].EndTime == nil {
		t.Error("Expected EndTime to be set when completed")
	}
	if state.Tables[0].Progress != 100 {
		t.Errorf("Expected progress 100, got %f", state.Tables[0].Progress)
	}
	if state.Summary.CompletedTables != 1 {
		t.Errorf("Expected completed tables 1, got %d", state.Summary.CompletedTables)
	}
}

func TestCopyState_UpdateTableStatus_NonExistent(t *testing.T) {
	state := NewCopyState("test", OperationConfig{Parallel: 4})

	// Should not panic when updating non-existent table
	state.UpdateTableStatus("public", "nonexistent", TableStatusCopying)
	if len(state.Tables) != 0 {
		t.Error("Expected no tables to be added")
	}
}

func TestCopyState_UpdateTableProgress(t *testing.T) {
	state := NewCopyState("test", OperationConfig{Parallel: 4})
	state.AddTable("public", "users", 1000)
	state.UpdateTableStatus("public", "users", TableStatusCopying)

	// Wait a tiny bit for timing calculations
	time.Sleep(10 * time.Millisecond)

	state.UpdateTableProgress("public", "users", 500)

	table := state.Tables[0]
	if table.SyncedRows != 500 {
		t.Errorf("Expected synced rows 500, got %d", table.SyncedRows)
	}
	if table.Progress != 50 {
		t.Errorf("Expected progress 50, got %f", table.Progress)
	}
	if table.Speed <= 0 {
		t.Error("Expected speed to be calculated")
	}
	if state.Summary.SyncedRows != 500 {
		t.Errorf("Expected summary synced rows 500, got %d", state.Summary.SyncedRows)
	}
}

func TestCopyState_UpdateTableProgress_NonExistent(_ *testing.T) {
	state := NewCopyState("test", OperationConfig{Parallel: 4})

	// Should not panic when updating non-existent table
	state.UpdateTableProgress("public", "nonexistent", 500)
}

func TestCopyState_AddLog(t *testing.T) {
	state := NewCopyState("test", OperationConfig{Parallel: 4})

	state.AddLog(LogLevelInfo, "Test message", "copier", "public.users", nil)

	if len(state.Logs) != 1 {
		t.Fatalf("Expected 1 log entry, got %d", len(state.Logs))
	}

	log := state.Logs[0]
	if log.Level != LogLevelInfo {
		t.Errorf("Expected level Info, got '%s'", log.Level)
	}
	if log.Message != "Test message" {
		t.Errorf("Expected message 'Test message', got '%s'", log.Message)
	}
	if log.Component != "copier" {
		t.Errorf("Expected component 'copier', got '%s'", log.Component)
	}
	const expectedTable = "public.users"
	if log.Table != expectedTable {
		t.Errorf("Expected table '%s', got '%s'", expectedTable, log.Table)
	}
}

func TestCopyState_AddLog_TruncatesOldLogs(t *testing.T) {
	state := NewCopyState("test", OperationConfig{Parallel: 4})

	// Add more than 1000 logs
	for i := 0; i < 1050; i++ {
		state.AddLog(LogLevelInfo, "Test message", "copier", "", nil)
	}

	if len(state.Logs) > 1000 {
		t.Errorf("Expected max 1000 logs, got %d", len(state.Logs))
	}
}

func TestCopyState_GetSnapshot(t *testing.T) {
	state := NewCopyState("test-id", OperationConfig{Parallel: 4})
	state.AddTable("public", "users", 1000)
	state.AddLog(LogLevelInfo, "Test", "copier", "", nil)

	snapshot := state.GetSnapshot()

	if snapshot.ID != "test-id" {
		t.Errorf("Expected ID 'test-id', got '%s'", snapshot.ID)
	}
	if len(snapshot.Tables) != 1 {
		t.Errorf("Expected 1 table, got %d", len(snapshot.Tables))
	}
	if len(snapshot.Logs) != 1 {
		t.Errorf("Expected 1 log, got %d", len(snapshot.Logs))
	}

	// Ensure snapshot is independent (modifying original doesn't affect snapshot)
	state.AddTable("public", "orders", 500)
	if len(snapshot.Tables) != 1 {
		t.Error("Snapshot should be independent of original state")
	}
}

func TestCopyState_Subscribe_Unsubscribe(t *testing.T) {
	state := NewCopyState("test", OperationConfig{Parallel: 4})

	var receivedEvents []Event
	var mu sync.Mutex

	listener := &testListener{
		onStateChange: func(_ *CopyState, e Event) {
			mu.Lock()
			receivedEvents = append(receivedEvents, e)
			mu.Unlock()
		},
	}

	state.Subscribe(listener)

	state.SetStatus(StatusPreparing)

	// Wait for async event
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if len(receivedEvents) == 0 {
		t.Error("Expected to receive at least one event")
	}
	mu.Unlock()

	// Unsubscribe and verify no more events
	state.Unsubscribe(listener)
	initialCount := len(receivedEvents)

	state.SetStatus(StatusCopying)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if len(receivedEvents) != initialCount {
		t.Error("Should not receive events after unsubscribe")
	}
	mu.Unlock()
}

func TestCopyState_ConcurrentAccess(_ *testing.T) {
	state := NewCopyState("test", OperationConfig{Parallel: 4})

	var wg sync.WaitGroup
	numGoroutines := 10
	numOperations := 100

	// Add some tables first
	for i := 0; i < 5; i++ {
		state.AddTable("public", "table"+string(rune('0'+i)), int64(i*100))
	}

	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(_ int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				switch j % 4 {
				case 0:
					state.AddLog(LogLevelInfo, "test", "copier", "", nil)
				case 1:
					state.UpdateTableProgress("public", "table0", int64(j))
				case 2:
					_ = state.GetSnapshot()
				case 3:
					state.mu.RLock()
					_ = len(state.Tables)
					state.mu.RUnlock()
				}
			}
		}(i)
	}

	wg.Wait()
	// If we get here without deadlock or panic, test passes
}

// testListener implements Listener interface for testing
type testListener struct {
	onStateChange func(*CopyState, Event)
}

func (l *testListener) OnStateChange(state *CopyState, event Event) {
	if l.onStateChange != nil {
		l.onStateChange(state, event)
	}
}

func TestOperationStatus_String(t *testing.T) {
	tests := []struct {
		status   OperationStatus
		expected string
	}{
		{StatusInitializing, "initializing"},
		{StatusPreparing, "preparing"},
		{StatusCopying, "copying"},
		{StatusCompleted, "completed"},
		{StatusFailed, "failed"},
		{StatusCancelled, "cancelled"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if string(tt.status) != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, string(tt.status))
			}
		})
	}
}

func TestTableStatus_String(t *testing.T) {
	tests := []struct {
		status   TableStatus
		expected string
	}{
		{TableStatusPending, "pending"},
		{TableStatusQueued, "queued"},
		{TableStatusCopying, "copying"},
		{TableStatusCompleted, "completed"},
		{TableStatusFailed, "failed"},
		{TableStatusSkipped, "skipped"},
		{TableStatusRetrying, "retrying"},
		{TableStatusCancelled, "cancelled"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if string(tt.status) != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, string(tt.status))
			}
		})
	}
}
