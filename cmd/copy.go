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
	"sync/atomic"
	"syscall"
	"time"

	"github.com/koltyakov/pgcopy/internal/copier"
	"github.com/koltyakov/pgcopy/internal/state"
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

// ShutdownInfo provides context for graceful shutdown decisions.
type ShutdownInfo interface {
	// HasVitalOperations returns true if there are operations that need graceful completion
	// (e.g., FK restoration after copy has started).
	HasVitalOperations() bool
}

// setupTwoPhaseShutdown sets up signal handling with two-phase shutdown.
// First signal: graceful shutdown, attempts to finish vital operations (FK restore).
// Second signal: force quit immediately.
// If info is provided and HasVitalOperations() returns false, exits immediately on first signal.
// Returns a channel that receives true on first signal (graceful shutdown requested).
func setupTwoPhaseShutdown(cancel context.CancelFunc, info ShutdownInfo) <-chan struct{} {
	shutdownChan := make(chan struct{}, 1)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	var shutdownRequested atomic.Bool

	go func() {
		for sig := range sigChan {
			if shutdownRequested.CompareAndSwap(false, true) {
				// First signal: check if there are vital operations
				hasVital := info != nil && info.HasVitalOperations()

				if hasVital {
					fmt.Printf("\n⏹️  Shutdown requested (%v). Finishing vital operations...\n", sig)
					fmt.Printf("   Press Ctrl-C again to force quit immediately.\n")
					cancel()
					select {
					case shutdownChan <- struct{}{}:
					default:
					}
				} else {
					fmt.Printf("\n⏹️  Cancelled.\n")
					os.Exit(0)
				}
			} else {
				// Second signal: force quit
				fmt.Printf("\n⚠️  Force quit requested. Exiting immediately.\n")
				os.Exit(1)
			}
		}
	}()

	return shutdownChan
}

// copyCmd represents the copy command
var copyCmd = &cobra.Command{
	Use:   "copy",
	Short: "Copy data from source to target database",
	Long: `Copy data from source PostgreSQL database to target PostgreSQL database.
Both databases must have the same schema structure.

Examples:
  pgcopy copy --source "postgres://user:pass@localhost:5432/sourcedb" --target "postgres://user:pass@localhost:5433/targetdb"
  pgcopy copy -s "postgres://user:pass@host1/db1" -t "postgres://user:pass@host2/db2" --parallel 4
  pgcopy copy --source "postgres://user:pass@source:5432/db" --target "postgres://user:pass@target:5432/db" --batch-size 5000
  pgcopy copy --source "postgres://user:pass@source:5432/db" --target "postgres://user:pass@target:5432/db" --output plain       # Plain mode for CI/headless (default)
  pgcopy copy --source "postgres://user:pass@source:5432/db" --target "postgres://user:pass@target:5432/db" --output progress    # Progress bar mode
  pgcopy copy --source "postgres://user:pass@source:5432/db" --target "postgres://user:pass@target:5432/db" --output interactive # Interactive mode with live table progress
  pgcopy copy --source "postgres://user:pass@source:5432/db" --target "postgres://user:pass@target:5432/db" --output web         # Web interface mode with real-time monitoring
  pgcopy copy -s "..." -t "..." --exclude "temp_*,*_logs,*_cache"        # Exclude with wildcards
  pgcopy copy -s "..." -t "..." --include "user_*,order_*"               # Include with wildcards`,
	Run: func(cmd *cobra.Command, _ []string) {
		sourceConn, _ := cmd.Flags().GetString("source")
		// Support both --target (new) and --dest (deprecated, hidden)
		targetConn, _ := cmd.Flags().GetString("target")
		deprecatedDest, _ := cmd.Flags().GetString("dest")
		if targetConn == "" && deprecatedDest != "" {
			targetConn = deprecatedDest
		}
		parallel, _ := cmd.Flags().GetInt("parallel")
		batchSize, _ := cmd.Flags().GetInt("batch-size")
		excludeTables, _ := cmd.Flags().GetStringSlice("exclude")
		includeTables, _ := cmd.Flags().GetStringSlice("include")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		skipBackup, _ := cmd.Flags().GetBool("skip-backup")
		output, _ := cmd.Flags().GetString("output")
		legacyBulk, _ := cmd.Flags().GetBool("legacy-bulk")
		compressPipe, _ := cmd.Flags().GetBool("compress")
		exactRows, _ := cmd.Flags().GetBool("exact-rows")
		noTimeouts, _ := cmd.Flags().GetBool("no-timeouts")
		snapshot, _ := cmd.Flags().GetBool("snapshot")

		// StreamCopy (COPY protocol) is now the default; use --legacy-bulk to revert
		useCopyPipe := !legacyBulk

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
			TargetConn:    targetConn,
			Parallel:      parallel,
			BatchSize:     batchSize,
			ExcludeTables: excludeTables,
			IncludeTables: includeTables,
			DryRun:        dryRun,
			SkipBackup:    skipBackup,
			OutputMode:    string(displayMode),
			UseCopyPipe:   useCopyPipe,
			CompressPipe:  compressPipe,
			ExactRows:     exactRows,
			NoTimeouts:    noTimeouts,
			Snapshot:      snapshot,
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

			stateCopier, err := copier.NewWithWebPort(config, actualPort)
			if err != nil {
				duration := time.Since(start)
				log.Fatalf("Failed to initialize copier after %s: %v", utils.FormatDuration(duration), err)
			}
			defer stateCopier.Close()

			// Setup two-phase graceful shutdown (pass copier for vital ops check)
			shutdownChan := setupTwoPhaseShutdown(cancel, stateCopier)

			// Start copy operation in a goroutine
			copyDone := make(chan error, 1)
			go func() { copyDone <- stateCopier.Copy(ctx) }()

			// Only auto-open the browser after overwrite confirmation. If --skip-backup is set,
			// confirmation is bypassed and status will move to copying immediately.
			if skipBackup {
				if err := openBrowser(fmt.Sprintf("http://localhost:%d", actualPort)); err != nil {
					fmt.Printf("Could not open browser automatically: %v\n", err)
					fmt.Printf("Open http://localhost:%d manually\n", actualPort)
				}
			} else {
				// Poll state until StatusCopying, then open browser
				go func() {
					ticker := time.NewTicker(200 * time.Millisecond)
					defer ticker.Stop()
					for {
						select {
						case <-ctx.Done():
							return
						case <-ticker.C:
							snap := stateCopier.State().GetSnapshot()
							if snap.Status == state.StatusCopying {
								if err := openBrowser(fmt.Sprintf("http://localhost:%d", actualPort)); err != nil {
									fmt.Printf("Could not open browser automatically: %v\n", err)
									fmt.Printf("Open http://localhost:%d manually\n", actualPort)
								}
								return
							}
						}
					}
				}()
			}

			// Wait for either completion or interrupt
			select {
			case err := <-copyDone:
				if err != nil {
					duration := time.Since(start)
					log.Fatalf("Copy operation failed after %s: %v", utils.FormatDuration(duration), err)
				}
				fmt.Printf("\n🎉 Copy operation completed successfully!\n")
				fmt.Printf("You can close the web interface now.\n")
			case <-shutdownChan:
				// Graceful shutdown: wait for copy to finish (which includes FK restoration)
				// The context is already cancelled, so Copy() should wind down
				select {
				case err := <-copyDone:
					if err != nil {
						fmt.Printf("Copy operation ended with error: %v\n", err)
					} else {
						fmt.Printf("Graceful shutdown completed.\n")
					}
				case <-time.After(30 * time.Second):
					fmt.Printf("Timeout waiting for graceful shutdown.\n")
				}
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

			// Setup two-phase graceful shutdown (pass copier for vital ops check)
			shutdownChan := setupTwoPhaseShutdown(cancel, dataCopier)

			done := make(chan error, 1)
			go func() { done <- dataCopier.Copy(ctx) }()

			select {
			case err := <-done:
				if err != nil {
					duration := time.Since(start)
					log.Fatalf("Copy operation failed after %s: %v", utils.FormatDuration(duration), err)
				}
			case <-shutdownChan:
				// Graceful shutdown: wait for copy to finish (which includes FK restoration)
				select {
				case err := <-done:
					if err != nil {
						fmt.Printf("Copy operation ended with error: %v\n", err)
					} else {
						fmt.Printf("Graceful shutdown completed.\n")
					}
				case <-time.After(30 * time.Second):
					fmt.Printf("Timeout waiting for graceful shutdown.\n")
				}
				return
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(copyCmd)

	copyCmd.Flags().StringP("source", "s", "", "Source database connection string (postgres://user:pass@host:port/dbname)")
	copyCmd.Flags().StringP("target", "t", "", "Target database connection string (postgres://user:pass@host:port/dbname)")
	// Deprecated: --dest is kept for backwards compatibility but hidden from help
	copyCmd.Flags().StringP("dest", "d", "", "Deprecated: use --target instead")
	_ = copyCmd.Flags().MarkHidden("dest")
	copyCmd.Flags().IntP("parallel", "p", 4, "Number of parallel workers")
	copyCmd.Flags().Int("batch-size", 1000, "Batch size for data copying")
	copyCmd.Flags().StringSlice("exclude", []string{}, "Tables to exclude from copying (supports wildcards: temp_*,*_logs)")
	copyCmd.Flags().StringSlice("include", []string{}, "Tables to include in copying (supports wildcards: user_*,*_data)")
	copyCmd.Flags().Bool("dry-run", false, "Show what would be copied without actually copying data")
	copyCmd.Flags().Bool("skip-backup", false, "Skip confirmation dialog for data overwrite")
	copyCmd.Flags().StringP("output", "o", "plain", "Output mode: 'plain' (minimal output, default), 'progress' (progress bar), 'interactive' (live table progress), 'web' (web interface; auto on :8080 or random)")
	copyCmd.Flags().Bool("legacy-bulk", false, "Use legacy bulk INSERT mode instead of streaming COPY pipeline (slower but more compatible)")
	copyCmd.Flags().Bool("compress", false, "Gzip-compress streaming COPY pipeline")
	copyCmd.Flags().Bool("exact-rows", false, "Use exact row counting (COUNT(*)) during discovery to avoid bad estimates after TRUNCATE; slower on large tables")
	copyCmd.Flags().Bool("no-timeouts", false, "Disable internal operation timeouts (use with caution; may wait indefinitely)")
	copyCmd.Flags().Bool("snapshot", false, "Read each table inside a REPEATABLE READ transaction to ensure consistent pagination (avoids missing/duplicate rows if new rows are inserted during copy)")
}
