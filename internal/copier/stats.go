package copier

import (
	"fmt"
	"time"

	"github.com/koltyakov/pgcopy/internal/utils"
)

// printStats prints final copy statistics
func (c *Copier) printStats() {
	duration := time.Since(c.stats.StartTime)

	// Create a beautiful table for the statistics
	fmt.Printf("\n")
	fmt.Printf("╔══════════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║                        📊 COPY STATISTICS                    ║\n")
	fmt.Printf("╠══════════════════════════════════════════════════════════════╣\n")

	// Calculate column widths for proper alignment
	maxLabelWidth := 18
	maxValueWidth := 35

	// Main statistics
	if c.stats.TablesProcessed == c.stats.TotalTables {
		fmt.Printf("║  📋 %-*s  %*d  ║\n", maxLabelWidth, "Tables Processed:", maxValueWidth, c.stats.TablesProcessed)
	} else {
		fmt.Printf("║  📋 %-*s  %*s  ║\n", maxLabelWidth, "Tables Processed:", maxValueWidth,
			fmt.Sprintf("%d / %d", c.stats.TablesProcessed, c.stats.TotalTables))
	}
	fmt.Printf("║  📊 %-*s  %*d  ║\n", maxLabelWidth, "Rows Copied:", maxValueWidth, c.stats.RowsCopied)
	fmt.Printf("║  ⏱️  %-*s  %*s  ║\n", maxLabelWidth, "Duration:", maxValueWidth,
		utils.FormatDuration(duration))

	if c.stats.RowsCopied > 0 && duration.Seconds() > 0 {
		rowsPerSecond := float64(c.stats.RowsCopied) / duration.Seconds()
		fmt.Printf("║  🚀 %-*s  %*s  ║\n", maxLabelWidth, "Average Speed:", maxValueWidth,
			fmt.Sprintf("%d rows/s", int(rowsPerSecond)))
	}

	// Foreign key statistics
	if c.fkManager != nil {
		total, dropped := c.fkManager.GetForeignKeyStats()
		if dropped > 0 {
			fmt.Printf("║  🔗 %-*s  %*s  ║\n", maxLabelWidth, "Foreign Keys:", maxValueWidth,
				fmt.Sprintf("%d detected, %d dropped", total, dropped))
		}
	}

	// Errors section (if any)
	if len(c.stats.Errors) > 0 {
		fmt.Printf("╠══════════════════════════════════════════════════════════════╣\n")
		fmt.Printf("║                          ⚠️  ERRORS                          ║\n")
		fmt.Printf("╠══════════════════════════════════════════════════════════════╣\n")

		for i, err := range c.stats.Errors {
			errorText := err.Error()
			// Truncate long error messages
			if len(errorText) > 55 {
				errorText = errorText[:52] + "..."
			}
			fmt.Printf("║  %d. %-55s ║\n", i+1, errorText)
		}
	}

	fmt.Printf("╚══════════════════════════════════════════════════════════════╝\n")
	fmt.Printf("\n")
}
