// Package cmd provides command-line interface for pgcopy
package cmd

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/koltyakov/pgcopy/internal/copier"
	"github.com/koltyakov/pgcopy/internal/utils"
	"github.com/spf13/cobra"
)

// openBrowser opens the default web browser to the given URL
func openBrowser(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start"}
	case "darwin":
		cmd = "open"
	default: // "linux", "freebsd", "openbsd", "netbsd"
		cmd = "xdg-open"
	}
	args = append(args, url)
	return exec.Command(cmd, args...).Start() // #nosec G204 - command constructed from validated OS constants
}

// findAvailablePort finds an available port starting from the given port
func findAvailablePort(startPort int) int {
	for port := startPort; port < startPort+100; port++ {
		listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err == nil {
			_ = listener.Close()
			return port
		}
	}
	// If we can't find any port in range, fall back to a random available port
	listener, err := net.Listen("tcp", ":0") // #nosec G102 - binding to all interfaces is required for port detection
	if err != nil {
		return startPort // Fallback to original port
	}
	defer func() { _ = listener.Close() }()
	return listener.Addr().(*net.TCPAddr).Port
}

// copyCmd represents the copy command
var copyCmd = &cobra.Command{
	Use:   "copy",
	Short: "Copy data from source to destination database",
	Long: `Copy data from source PostgreSQL database to destination PostgreSQL database.
Both databases must have the same schema structure.

Examples:
  pgcopy copy --source "postgres://user:pass@localhost:5432/sourcedb" --dest "postgres://user:pass@localhost:5433/destdb"
  pgcopy copy -s "postgres://user:pass@host1/db1" -d "postgres://user:pass@host2/db2" --parallel 4
  pgcopy copy --source "postgres://user:pass@source:5432/db" --dest "postgres://user:pass@dest:5432/db" --batch-size 5000
  pgcopy copy --source "postgres://user:pass@source:5432/db" --dest "postgres://user:pass@dest:5432/db" --output plain       # Plain mode for CI/headless (default)
  pgcopy copy --source "postgres://user:pass@source:5432/db" --dest "postgres://user:pass@dest:5432/db" --output progress    # Progress bar mode
  pgcopy copy --source "postgres://user:pass@source:5432/db" --dest "postgres://user:pass@dest:5432/db" --output interactive # Interactive mode with live table progress
  pgcopy copy --source "postgres://user:pass@source:5432/db" --dest "postgres://user:pass@dest:5432/db" --output web         # Web interface mode with real-time monitoring
  pgcopy copy -s "..." -d "..." --exclude "temp_*,*_logs,*_cache"        # Exclude with wildcards
  pgcopy copy -s "..." -d "..." --include "user_*,order_*"               # Include with wildcards`,
	Run: func(cmd *cobra.Command, _ []string) {
		sourceConn, _ := cmd.Flags().GetString("source")
		destConn, _ := cmd.Flags().GetString("dest")
		parallel, _ := cmd.Flags().GetInt("parallel")
		batchSize, _ := cmd.Flags().GetInt("batch-size")
		excludeTables, _ := cmd.Flags().GetStringSlice("exclude")
		includeTables, _ := cmd.Flags().GetStringSlice("include")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		skipBackup, _ := cmd.Flags().GetBool("skip-backup")
		output, _ := cmd.Flags().GetString("output")
		useCopyPipe, _ := cmd.Flags().GetBool("copy-pipe")
		compressPipe, _ := cmd.Flags().GetBool("compress")
		// Parse display mode
		var displayMode copier.DisplayMode
		switch output {
		case "interactive":
			displayMode = copier.DisplayModeInteractive
		case "progress":
			displayMode = copier.DisplayModeProgress
		case "web":
			displayMode = copier.DisplayModeWeb
		case "plain":
			displayMode = copier.DisplayModeRaw
		default:
			displayMode = copier.DisplayModeRaw // Default to plain mode
		}

		config := &copier.Config{
			SourceConn:    sourceConn,
			DestConn:      destConn,
			Parallel:      parallel,
			BatchSize:     batchSize,
			ExcludeTables: excludeTables,
			IncludeTables: includeTables,
			DryRun:        dryRun,
			SkipBackup:    skipBackup,
			OutputMode:    string(displayMode),
			UseCopyPipe:   useCopyPipe,
			CompressPipe:  compressPipe,
		}

		start := time.Now()

		if err := copier.ValidateConfig(config); err != nil {
			duration := time.Since(start)
			log.Fatalf("Configuration error after %s: %v", utils.FormatDuration(duration), err)
		}

		fmt.Printf("Starting data copy operation...\n")

		// Use state-driven copier for web mode
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		if displayMode == copier.DisplayModeWeb {
			// Prefer default 8080, otherwise fall back to a random available port
			preferred := 8080
			actualPort := findAvailablePort(preferred)
			if actualPort != preferred {
				fmt.Printf("Port %d unavailable, selected free port %d\n", preferred, actualPort)
			}

			if err := openBrowser(fmt.Sprintf("http://localhost:%d", actualPort)); err != nil {
				fmt.Printf("Could not open browser automatically: %v\n", err)
				fmt.Printf("Open http://localhost:%d manually\n", actualPort)
			}

			stateCopier, err := copier.NewWithWebPort(config, actualPort)
			if err != nil {
				duration := time.Since(start)
				log.Fatalf("Failed to initialize copier after %s: %v", utils.FormatDuration(duration), err)
			}
			defer stateCopier.Close()

			// Handle graceful shutdown
			c := make(chan os.Signal, 1)
			signal.Notify(c, os.Interrupt, syscall.SIGTERM)

			// Start copy operation in a goroutine
			copyDone := make(chan error, 1)
			go func() { copyDone <- stateCopier.Copy(ctx) }()

			// Wait for either completion or interrupt
			select {
			case err := <-copyDone:
				if err != nil {
					duration := time.Since(start)
					log.Fatalf("Copy operation failed after %s: %v", utils.FormatDuration(duration), err)
				}
				fmt.Printf("\n🎉 Copy operation completed successfully!\n")
				fmt.Printf("You can close the web interface now.\n")
			case <-c:
				fmt.Printf("\n⏹️  Operation cancelled by user\n")
				return
			}
		} else {
			// Use regular copier for other modes
			dataCopier, err := copier.New(config)
			if err != nil {
				duration := time.Since(start)
				log.Fatalf("Failed to initialize copier after %s: %v", utils.FormatDuration(duration), err)
			}
			defer dataCopier.Close()

			// Graceful shutdown: cancel ctx on SIGINT/SIGTERM
			c := make(chan os.Signal, 1)
			signal.Notify(c, os.Interrupt, syscall.SIGTERM)

			done := make(chan error, 1)
			go func() { done <- dataCopier.Copy(ctx) }()

			select {
			case err := <-done:
				if err != nil {
					duration := time.Since(start)
					log.Fatalf("Copy operation failed after %s: %v", utils.FormatDuration(duration), err)
				}
			case <-c:
				fmt.Printf("\n⏹️  Operation cancelled by user\n")
				cancel()
				return
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(copyCmd)

	copyCmd.Flags().StringP("source", "s", "", "Source database connection string (postgres://user:pass@host:port/dbname)")
	copyCmd.Flags().StringP("dest", "d", "", "Destination database connection string (postgres://user:pass@host:port/dbname)")
	copyCmd.Flags().IntP("parallel", "p", 4, "Number of parallel workers")
	copyCmd.Flags().Int("batch-size", 1000, "Batch size for data copying")
	copyCmd.Flags().StringSlice("exclude", []string{}, "Tables to exclude from copying (supports wildcards: temp_*,*_logs)")
	copyCmd.Flags().StringSlice("include", []string{}, "Tables to include in copying (supports wildcards: user_*,*_data)")
	copyCmd.Flags().Bool("dry-run", false, "Show what would be copied without actually copying data")
	copyCmd.Flags().Bool("skip-backup", false, "Skip confirmation dialog for data overwrite")
	copyCmd.Flags().StringP("output", "o", "plain", "Output mode: 'plain' (minimal output, default), 'progress' (progress bar), 'interactive' (live table progress), 'web' (web interface; auto on :8080 or random)")
	copyCmd.Flags().Bool("copy-pipe", false, "Use streaming COPY pipeline (source->dest) instead of row fetch + insert")
	copyCmd.Flags().Bool("compress", false, "Gzip-compress streaming COPY pipeline (requires --copy-pipe)")
}
