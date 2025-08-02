package copier

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/koltyakov/pgcopy/internal/utils"
)

// TableProgress represents the progress of a single table
type TableProgress struct {
	Schema     string
	Name       string
	RowCount   int64
	CopiedRows int64
	StartTime  time.Time
	EndTime    *time.Time
	Status     TableStatus
	Order      int // Order in which table was started or completed
}

// TableStatus represents the current status of a table
type TableStatus int

// Table status constants
const (
	TableStatusPending    TableStatus = iota // Table is waiting to be processed
	TableStatusInProgress                    // Table is currently being copied
	TableStatusCompleted                     // Table has been successfully copied
	TableStatusFailed                        // Table copying failed
)

// InteractiveDisplay manages the interactive CLI display
type InteractiveDisplay struct {
	mu              sync.RWMutex
	tables          map[string]*TableProgress
	totalRows       int64
	totalCopiedRows int64
	totalTables     int64
	completedTables int64
	startTime       time.Time
	maxDisplayLines int
	orderCounter    int
	spinnerIndex    int
	isActive        bool
}

var spinnerChars = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// NewInteractiveDisplay creates a new interactive display
func NewInteractiveDisplay(totalTables int) *InteractiveDisplay {
	return &InteractiveDisplay{
		tables:          make(map[string]*TableProgress),
		totalTables:     int64(totalTables),
		completedTables: 0,
		maxDisplayLines: 20, // Increased to accommodate all in-progress tables
		spinnerIndex:    0,
		isActive:        false,
	}
}

// StartTable marks a table as started
func (d *InteractiveDisplay) StartTable(schema, name string, rowCount int64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	tableKey := fmt.Sprintf("%s.%s", schema, name)
	d.orderCounter++
	d.tables[tableKey] = &TableProgress{
		Schema:    schema,
		Name:      name,
		RowCount:  rowCount,
		StartTime: time.Now(),
		Status:    TableStatusInProgress,
		Order:     d.orderCounter,
	}
}

// SetTotalRows sets the total number of rows across all tables
func (d *InteractiveDisplay) SetTotalRows(totalRows int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.totalRows = totalRows
}

// UpdateTableProgress updates the progress of a table
func (d *InteractiveDisplay) UpdateTableProgress(schema, name string, copiedRows int64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	tableKey := fmt.Sprintf("%s.%s", schema, name)
	if table, exists := d.tables[tableKey]; exists {
		oldCopied := table.CopiedRows
		table.CopiedRows = copiedRows

		// Update total copied rows
		d.totalCopiedRows += (copiedRows - oldCopied)
	}
}

// CompleteTable marks a table as completed
func (d *InteractiveDisplay) CompleteTable(schema, name string, success bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	tableKey := fmt.Sprintf("%s.%s", schema, name)
	if table, exists := d.tables[tableKey]; exists {
		now := time.Now()
		table.EndTime = &now

		if success {
			table.Status = TableStatusCompleted
			d.completedTables++
			// Update order for completed tables to sort them properly
			d.orderCounter++
			table.Order = d.orderCounter
		} else {
			table.Status = TableStatusFailed
		}
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

	// Add header with table status
	fmt.Printf("Tables: %d/%d completed\n\n",
		d.completedTables,
		d.totalTables)

	// Get sorted tables (completed first, then in-progress, ordered by their order number)
	sortedTables := d.getSortedTables()

	// Show tables - prioritize showing ALL in-progress tables
	completedShown := 0
	inProgressShown := 0

	// First pass: Show all in-progress and failed tables (no limits)
	for _, table := range sortedTables {
		switch table.Status {
		case TableStatusInProgress:
			d.renderInProgressTable(table)
			inProgressShown++
		case TableStatusFailed:
			d.renderFailedTable(table)
		}
	}

	// Second pass: Show completed tables up to remaining display space
	remainingLines := d.maxDisplayLines - 4 - inProgressShown // Reserve space for header, progress bar, and in-progress tables
	for _, table := range sortedTables {
		if table.Status == TableStatusCompleted && completedShown < remainingLines && completedShown < 15 {
			d.renderCompletedTable(table)
			completedShown++
		}
	}

	// Show summary if we have hidden tables
	hiddenCompleted := int(d.completedTables) - completedShown
	if hiddenCompleted > 0 {
		fmt.Printf("  %s %d more tables completed...\n",
			utils.Colorize(utils.ColorGreen, "✓"), hiddenCompleted)
	}

	// Show overall progress bar
	d.renderProgressBar()

	d.spinnerIndex = (d.spinnerIndex + 1) % len(spinnerChars)
}

// getSortedTables returns tables sorted by completion status and order
func (d *InteractiveDisplay) getSortedTables() []*TableProgress {
	tables := make([]*TableProgress, 0, len(d.tables))
	for _, table := range d.tables {
		tables = append(tables, table)
	}

	sort.Slice(tables, func(i, j int) bool {
		// Completed tables first, then in-progress, then failed
		if tables[i].Status != tables[j].Status {
			if tables[i].Status == TableStatusCompleted {
				return true
			}
			if tables[j].Status == TableStatusCompleted {
				return false
			}
			if tables[i].Status == TableStatusInProgress {
				return true
			}
			if tables[j].Status == TableStatusInProgress {
				return false
			}
		}

		// Within same status, sort by order (most recent first for completed, oldest first for in-progress)
		if tables[i].Status == TableStatusCompleted {
			return tables[i].Order > tables[j].Order // Most recent completed first
		}
		return tables[i].Order < tables[j].Order // Oldest in-progress first
	})

	return tables
}

// renderCompletedTable renders a completed table
func (d *InteractiveDisplay) renderCompletedTable(table *TableProgress) {
	duration := table.EndTime.Sub(table.StartTime)
	rowsFormatted := utils.FormatNumber(table.RowCount)

	fmt.Printf("  %s %s (%s rows) - %s\n",
		utils.Colorize(utils.ColorGreen, "✓"),
		utils.HighlightTableName(table.Schema, table.Name),
		utils.Colorize(utils.ColorDim, rowsFormatted),
		utils.Colorize(utils.ColorDim, utils.FormatDuration(duration)))
}

// renderInProgressTable renders an in-progress table
func (d *InteractiveDisplay) renderInProgressTable(table *TableProgress) {
	spinner := spinnerChars[d.spinnerIndex]
	duration := time.Since(table.StartTime)

	var progressStr string
	if table.RowCount > 0 {
		percentage := float64(table.CopiedRows) / float64(table.RowCount) * 100
		progressStr = fmt.Sprintf("%.1f%% (%s/%s)",
			percentage,
			utils.FormatNumber(table.CopiedRows),
			utils.FormatNumber(table.RowCount))
	} else {
		progressStr = fmt.Sprintf("%s rows", utils.FormatNumber(table.CopiedRows))
	}

	fmt.Printf("  %s %s %s - %s\n",
		utils.Colorize(utils.ColorYellow, spinner),
		utils.HighlightTableName(table.Schema, table.Name),
		utils.Colorize(utils.ColorCyan, progressStr),
		utils.Colorize(utils.ColorDim, utils.FormatDuration(duration)))
}

// renderFailedTable renders a failed table
func (d *InteractiveDisplay) renderFailedTable(table *TableProgress) {
	duration := time.Since(table.StartTime)

	fmt.Printf("  %s %s - %s\n",
		utils.Colorize(utils.ColorRed, "✗"),
		utils.HighlightTableName(table.Schema, table.Name),
		utils.Colorize(utils.ColorDim, utils.FormatDuration(duration)))
}

// renderProgressBar renders the overall progress bar
func (d *InteractiveDisplay) renderProgressBar() {
	elapsed := time.Since(d.startTime)

	// Calculate progress
	var percentage float64
	if d.totalRows > 0 {
		percentage = float64(d.totalCopiedRows) / float64(d.totalRows) * 100
	}

	// Calculate speed
	var speedStr string
	if elapsed.Seconds() > 0 {
		rowsPerSecond := float64(d.totalCopiedRows) / elapsed.Seconds()
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
		utils.FormatNumber(d.totalCopiedRows),
		utils.FormatNumber(d.totalRows),
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
