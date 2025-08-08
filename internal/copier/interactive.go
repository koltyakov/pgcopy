package copier

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/koltyakov/pgcopy/internal/state"
	"github.com/koltyakov/pgcopy/internal/utils"
)

// InteractiveDisplay manages the interactive CLI display
type InteractiveDisplay struct {
	mu              sync.RWMutex
	state           *state.CopyState
	startTime       time.Time
	maxDisplayLines int
	spinnerIndex    int
	isActive        bool
}

var spinnerChars = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// NewInteractiveDisplay creates a new interactive display
func NewInteractiveDisplay(state *state.CopyState) *InteractiveDisplay {
	return &InteractiveDisplay{
		state:           state,
		maxDisplayLines: 20, // Increased to accommodate all in-progress tables
		spinnerIndex:    0,
		isActive:        false,
	}
}

// Start begins the interactive display loop
func (d *InteractiveDisplay) Start() {
	d.mu.Lock()
	d.isActive = true
	d.startTime = time.Now()
	d.mu.Unlock()

	// Hide cursor
	fmt.Print("\033[?25l")

	// Start update loop
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond) // Update 10 times per second
		defer ticker.Stop()

		for d.isActive {
			<-ticker.C
			d.render()
		}
	}()
}

// Stop stops the interactive display
func (d *InteractiveDisplay) Stop() {
	d.isActive = false

	// Show cursor
	fmt.Print("\033[?25h")

	// Clear screen and move to bottom
	d.clearScreen()
	fmt.Println()

	// Interactive mode stays quiet on errors; a full error summary is written to copy.log at the end.
}

// render updates the display
func (d *InteractiveDisplay) render() {
	if !d.isActive {
		return
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	// Move to top of our display area
	d.clearScreen()

	// Get all tables from state (thread-safe)
	allTables := d.state.GetAllTables()

	// Count tables by status
	completedTables := 0
	for _, table := range allTables {
		if table.Status == state.TableStatusCompleted {
			completedTables++
		}
	}

	totalTables := len(allTables)

	// Add header with table status
	fmt.Printf("Tables: %d/%d completed\n\n", completedTables, totalTables)

	// Get sorted tables
	sortedTables := d.getSortedTablesFromCopy(allTables)

	// Show tables - prioritize showing ALL in-progress tables
	completedShown := 0
	inProgressShown := 0

	// First pass: Show all in-progress and failed tables (no limits)
	for _, table := range sortedTables {
		switch table.Status {
		case state.TableStatusCopying:
			d.renderInProgressTable(table)
			inProgressShown++
		case state.TableStatusFailed:
			d.renderFailedTable(table)
		}
	}

	// Second pass: Show completed tables up to remaining display space
	remainingLines := d.maxDisplayLines - 4 - inProgressShown // Reserve space for header, progress bar, and in-progress tables
	for _, table := range sortedTables {
		if table.Status == state.TableStatusCompleted && completedShown < remainingLines && completedShown < 15 {
			d.renderCompletedTable(table)
			completedShown++
		}
	}

	// Show summary if we have hidden tables
	hiddenCompleted := completedTables - completedShown
	if hiddenCompleted > 0 {
		fmt.Printf("  %s %d more tables completed...\n",
			utils.Colorize(utils.ColorGreen, "✓"), hiddenCompleted)
	}

	// Show overall progress bar
	d.renderProgressBar()

	d.spinnerIndex = (d.spinnerIndex + 1) % len(spinnerChars)
}

// getSortedTablesFromCopy returns tables sorted by completion status
func (d *InteractiveDisplay) getSortedTablesFromCopy(tables []state.TableState) []state.TableState {
	sort.Slice(tables, func(i, j int) bool {
		// Completed tables first, then in-progress, then failed
		if tables[i].Status != tables[j].Status {
			if tables[i].Status == state.TableStatusCompleted {
				return true
			}
			if tables[j].Status == state.TableStatusCompleted {
				return false
			}
			if tables[i].Status == state.TableStatusCopying {
				return true
			}
			if tables[j].Status == state.TableStatusCopying {
				return false
			}
		}

		// Within same status, sort by start time
		if tables[i].StartTime != nil && tables[j].StartTime != nil {
			if tables[i].Status == state.TableStatusCompleted {
				// Most recent completed first
				return tables[i].StartTime.After(*tables[j].StartTime)
			}
			// Oldest in-progress first
			return tables[i].StartTime.Before(*tables[j].StartTime)
		}

		// Fallback to name sorting
		return strings.Compare(tables[i].FullName, tables[j].FullName) < 0
	})

	return tables
}

// renderCompletedTable renders a completed table
func (d *InteractiveDisplay) renderCompletedTable(table state.TableState) {
	var duration time.Duration
	if table.StartTime != nil && table.EndTime != nil {
		duration = table.EndTime.Sub(*table.StartTime)
	}
	rowsFormatted := utils.FormatNumber(table.TotalRows)

	fmt.Printf("  %s %s (%s rows) - %s\n",
		utils.Colorize(utils.ColorGreen, "✓"),
		utils.HighlightTableName(table.Schema, table.Name),
		utils.Colorize(utils.ColorDim, rowsFormatted),
		utils.Colorize(utils.ColorDim, utils.FormatDuration(duration)))
}

// renderInProgressTable renders an in-progress table
func (d *InteractiveDisplay) renderInProgressTable(table state.TableState) {
	spinner := spinnerChars[d.spinnerIndex]
	var duration time.Duration
	if table.StartTime != nil {
		duration = time.Since(*table.StartTime)
	}

	var progressStr string
	if table.TotalRows > 0 {
		percentage := float64(table.SyncedRows) / float64(table.TotalRows) * 100
		progressStr = fmt.Sprintf("%.1f%% (%s/%s)",
			percentage,
			utils.FormatNumber(table.SyncedRows),
			utils.FormatNumber(table.TotalRows))
	} else {
		progressStr = fmt.Sprintf("%s rows", utils.FormatNumber(table.SyncedRows))
	}

	fmt.Printf("  %s %s %s - %s\n",
		utils.Colorize(utils.ColorYellow, spinner),
		utils.HighlightTableName(table.Schema, table.Name),
		utils.Colorize(utils.ColorCyan, progressStr),
		utils.Colorize(utils.ColorDim, utils.FormatDuration(duration)))
}

// renderFailedTable renders a failed table
func (d *InteractiveDisplay) renderFailedTable(table state.TableState) {
	var duration time.Duration
	if table.StartTime != nil {
		duration = time.Since(*table.StartTime)
	}

	fmt.Printf("  %s %s - %s\n",
		utils.Colorize(utils.ColorRed, "✗"),
		utils.HighlightTableName(table.Schema, table.Name),
		utils.Colorize(utils.ColorDim, utils.FormatDuration(duration)))
}

// renderProgressBar renders the overall progress bar
func (d *InteractiveDisplay) renderProgressBar() {
	elapsed := time.Since(d.startTime)

	// Calculate totals from state
	allTables := d.state.GetAllTables()
	var totalRows, totalCopiedRows int64
	for _, table := range allTables {
		totalRows += table.TotalRows
		totalCopiedRows += table.SyncedRows
	}

	// Calculate progress
	var percentage float64
	if totalRows > 0 {
		percentage = float64(totalCopiedRows) / float64(totalRows) * 100
	}

	// Calculate speed
	var speedStr string
	if elapsed.Seconds() > 0 {
		rowsPerSecond := float64(totalCopiedRows) / elapsed.Seconds()
		speedStr = fmt.Sprintf(" (%s/s)", utils.FormatNumber(int64(rowsPerSecond)))
	}

	// Progress bar width
	barWidth := 40
	filled := int(percentage * float64(barWidth) / 100)

	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

	fmt.Printf("\n")
	fmt.Printf("Overall Progress: %.1f%% [%s] %s/%s rows%s\n",
		percentage,
		utils.Colorize(utils.ColorGreen, bar),
		utils.FormatNumber(totalCopiedRows),
		utils.FormatNumber(totalRows),
		speedStr)

	fmt.Printf("Elapsed: %s\n",
		utils.FormatDuration(elapsed))
}

// clearScreen clears the display area
func (d *InteractiveDisplay) clearScreen() {
	// Move cursor to beginning of our area and clear down
	fmt.Print("\r\033[K")

	// Clear multiple lines
	for i := 0; i < d.maxDisplayLines+5; i++ {
		fmt.Print("\033[K\033[1A") // Clear line and move up
	}
	fmt.Print("\033[K") // Clear current line
}
