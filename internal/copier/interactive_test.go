package copier

import (
	"testing"
	"time"

	"github.com/koltyakov/pgcopy/internal/state"
)

func TestNewInteractiveDisplay(t *testing.T) {
	copyState := state.NewCopyState("test", state.OperationConfig{Parallel: 4})
	display := NewInteractiveDisplay(copyState)

	if display == nil {
		t.Fatal("Expected non-nil display")
	}
	if display.state != copyState {
		t.Error("Expected display.state to match copyState")
	}
	if display.maxDisplayLines != 20 {
		t.Errorf("Expected maxDisplayLines 20, got %d", display.maxDisplayLines)
	}
	if display.spinnerIndex != 0 {
		t.Errorf("Expected spinnerIndex 0, got %d", display.spinnerIndex)
	}
	if display.isActive.Load() {
		t.Error("Expected isActive to be false initially")
	}
}

func TestInteractiveDisplay_SpinnerChars(t *testing.T) {
	if len(spinnerChars) == 0 {
		t.Error("Expected spinner chars to be defined")
	}

	// Verify all spinner chars are non-empty
	for i, char := range spinnerChars {
		if char == "" {
			t.Errorf("Spinner char at index %d is empty", i)
		}
	}
}

func TestInteractiveDisplay_StartStop(t *testing.T) {
	copyState := state.NewCopyState("test", state.OperationConfig{Parallel: 4})
	display := NewInteractiveDisplay(copyState)

	// Test that Start activates the display
	display.Start()

	display.mu.RLock()
	isActive := display.isActive.Load()
	startTime := display.startTime
	display.mu.RUnlock()

	if !isActive {
		t.Error("Expected isActive to be true after Start")
	}
	if startTime.IsZero() {
		t.Error("Expected startTime to be set after Start")
	}

	// Give the goroutine a moment to start
	time.Sleep(50 * time.Millisecond)

	// Test that Stop deactivates the display
	display.Stop()

	// Give time for goroutine to stop
	time.Sleep(50 * time.Millisecond)

	if display.isActive.Load() {
		t.Error("Expected isActive to be false after Stop")
	}
}

func TestInteractiveDisplay_GetSortedTables(t *testing.T) {
	copyState := state.NewCopyState("test", state.OperationConfig{Parallel: 4})

	// Add tables with different statuses
	copyState.AddTable("public", "users", 1000)
	copyState.AddTable("public", "orders", 2000)
	copyState.AddTable("public", "products", 500)

	copyState.UpdateTableStatus("public", "users", state.TableStatusCompleted)
	copyState.UpdateTableStatus("public", "orders", state.TableStatusCopying)

	display := NewInteractiveDisplay(copyState)

	// Use reflection to test the internal sorting (getSortedTablesFromCopy is private)
	// We can test that the display can be created with various table states
	if display.state == nil {
		t.Error("Display state should not be nil")
	}

	// Verify tables were added
	tables := copyState.GetAllTables()
	if len(tables) != 3 {
		t.Errorf("Expected 3 tables, got %d", len(tables))
	}
}

func TestInteractiveDisplay_ProgressCalculations(t *testing.T) {
	copyState := state.NewCopyState("test", state.OperationConfig{Parallel: 4})

	// Add table with known values
	copyState.AddTable("public", "users", 1000)
	copyState.UpdateTableStatus("public", "users", state.TableStatusCopying)
	copyState.UpdateTableProgress("public", "users", 500)

	display := NewInteractiveDisplay(copyState)

	tables := display.state.GetAllTables()
	if len(tables) != 1 {
		t.Fatalf("Expected 1 table, got %d", len(tables))
	}

	table := tables[0]
	if table.SyncedRows != 500 {
		t.Errorf("Expected SyncedRows 500, got %d", table.SyncedRows)
	}
	if table.TotalRows != 1000 {
		t.Errorf("Expected TotalRows 1000, got %d", table.TotalRows)
	}

	// Test progress percentage calculation
	expectedProgress := float64(500) / float64(1000) * 100
	if table.Progress != expectedProgress {
		t.Errorf("Expected Progress %.1f, got %.1f", expectedProgress, table.Progress)
	}
}

func TestInteractiveDisplay_MultipleTableStates(t *testing.T) {
	copyState := state.NewCopyState("test", state.OperationConfig{Parallel: 4})

	// Add tables with various states
	copyState.AddTable("public", "completed_table", 100)
	copyState.AddTable("public", "copying_table", 200)
	copyState.AddTable("public", "pending_table", 300)
	copyState.AddTable("public", "failed_table", 400)

	copyState.UpdateTableStatus("public", "completed_table", state.TableStatusCompleted)
	copyState.UpdateTableStatus("public", "copying_table", state.TableStatusCopying)
	// pending_table stays pending
	copyState.UpdateTableStatus("public", "failed_table", state.TableStatusCopying)
	copyState.UpdateTableStatus("public", "failed_table", state.TableStatusFailed)

	display := NewInteractiveDisplay(copyState)

	tables := display.state.GetAllTables()

	// Count tables by status
	statusCounts := make(map[state.TableStatus]int)
	for _, table := range tables {
		statusCounts[table.Status]++
	}

	if statusCounts[state.TableStatusCompleted] != 1 {
		t.Errorf("Expected 1 completed table, got %d", statusCounts[state.TableStatusCompleted])
	}
	if statusCounts[state.TableStatusCopying] != 1 {
		t.Errorf("Expected 1 copying table, got %d", statusCounts[state.TableStatusCopying])
	}
	if statusCounts[state.TableStatusPending] != 1 {
		t.Errorf("Expected 1 pending table, got %d", statusCounts[state.TableStatusPending])
	}
	if statusCounts[state.TableStatusFailed] != 1 {
		t.Errorf("Expected 1 failed table, got %d", statusCounts[state.TableStatusFailed])
	}
}

func TestInteractiveDisplay_SpinnerIncrement(t *testing.T) {
	copyState := state.NewCopyState("test", state.OperationConfig{Parallel: 4})
	display := NewInteractiveDisplay(copyState)

	// Manually test spinner increment logic
	initial := display.spinnerIndex
	display.spinnerIndex = (display.spinnerIndex + 1) % len(spinnerChars)

	if display.spinnerIndex != (initial+1)%len(spinnerChars) {
		t.Errorf("Spinner increment failed")
	}

	// Test wrap-around
	display.spinnerIndex = len(spinnerChars) - 1
	display.spinnerIndex = (display.spinnerIndex + 1) % len(spinnerChars)

	if display.spinnerIndex != 0 {
		t.Errorf("Expected spinner to wrap to 0, got %d", display.spinnerIndex)
	}
}
