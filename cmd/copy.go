package cmd

import (
	"fmt"
	"log"
	"time"

	"github.com/koltyakov/pgcopy/internal/copier"
	"github.com/spf13/cobra"
)

// formatDuration formats a duration without decimal parts
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		minutes := int(d.Minutes())
		seconds := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60
	return fmt.Sprintf("%dh%dm%ds", hours, minutes, seconds)
}

// copyCmd represents the copy command
var copyCmd = &cobra.Command{
	Use:   "copy",
	Short: "Copy data from source to destination database",
	Long: `Copy data from source PostgreSQL database to destination PostgreSQL database.
Both databases must have the same schema structure. Progress bar is enabled by default.

Examples:
  pgcopy copy --source "postgres://user:pass@localhost:5432/sourcedb" --dest "postgres://user:pass@localhost:5433/destdb"
  pgcopy copy -s "postgres://user:pass@host1/db1" -d "postgres://user:pass@host2/db2" --parallel 4
  pgcopy copy --source-file source.conf --dest-file dest.conf --batch-size 5000
  pgcopy copy --source-file source.conf --dest-file dest.conf --no-progress  # Disable progress for CI`,
	Run: func(cmd *cobra.Command, args []string) {
		sourceConn, _ := cmd.Flags().GetString("source")
		destConn, _ := cmd.Flags().GetString("dest")
		sourceFile, _ := cmd.Flags().GetString("source-file")
		destFile, _ := cmd.Flags().GetString("dest-file")
		parallel, _ := cmd.Flags().GetInt("parallel")
		batchSize, _ := cmd.Flags().GetInt("batch-size")
		excludeTables, _ := cmd.Flags().GetStringSlice("exclude-tables")
		includeTables, _ := cmd.Flags().GetStringSlice("include-tables")
		resume, _ := cmd.Flags().GetBool("resume")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		noProgress, _ := cmd.Flags().GetBool("no-progress")

		config := &copier.Config{
			SourceConn:    sourceConn,
			DestConn:      destConn,
			SourceFile:    sourceFile,
			DestFile:      destFile,
			Parallel:      parallel,
			BatchSize:     batchSize,
			ExcludeTables: excludeTables,
			IncludeTables: includeTables,
			Resume:        resume,
			DryRun:        dryRun,
			ProgressBar:   !noProgress, // Enable progress by default, disable if --no-progress is set
		}

		start := time.Now()

		if err := copier.ValidateConfig(config); err != nil {
			duration := time.Since(start)
			log.Fatalf("Configuration error after %s: %v", formatDuration(duration), err)
		}

		fmt.Printf("Starting data copy operation...\n")

		dataCopier, err := copier.New(config)
		if err != nil {
			duration := time.Since(start)
			log.Fatalf("Failed to initialize copier after %s: %v", formatDuration(duration), err)
		}
		defer dataCopier.Close()

		if err := dataCopier.Copy(); err != nil {
			duration := time.Since(start)
			log.Fatalf("Copy operation failed after %s: %v", formatDuration(duration), err)
		}
	},
}

func init() {
	rootCmd.AddCommand(copyCmd)

	copyCmd.Flags().StringP("source", "s", "", "Source database connection string (postgres://user:pass@host:port/dbname)")
	copyCmd.Flags().StringP("dest", "d", "", "Destination database connection string (postgres://user:pass@host:port/dbname)")
	copyCmd.Flags().String("source-file", "", "Source database connection config file")
	copyCmd.Flags().String("dest-file", "", "Destination database connection config file")
	copyCmd.Flags().IntP("parallel", "p", 4, "Number of parallel workers")
	copyCmd.Flags().Int("batch-size", 1000, "Batch size for data copying")
	copyCmd.Flags().StringSlice("exclude-tables", []string{}, "Tables to exclude from copying")
	copyCmd.Flags().StringSlice("include-tables", []string{}, "Tables to include in copying (if specified, only these tables will be copied)")
	copyCmd.Flags().Bool("resume", false, "Resume from previous incomplete copy")
	copyCmd.Flags().Bool("dry-run", false, "Show what would be copied without actually copying data")
	copyCmd.Flags().Bool("no-progress", false, "Disable progress bar (useful for CI/headless environments)")
}
