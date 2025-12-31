package utils // nolint:revive // utils is an acceptable name for internal utility package

import "errors"

// Sentinel errors used for branching and classification across the application.
//
// These errors follow Go's sentinel error pattern for programmatic error handling.
// They enable callers to use errors.Is() to check error categories regardless of
// wrapping:
//
//	if errors.Is(err, utils.ErrCopyFailures) {
//	    // Handle copy failures specifically
//	}
//
// # Best Practices
//
// When wrapping these errors, always use %w to preserve error chain:
//
//	return fmt.Errorf("failed to copy users table: %w", utils.ErrCopyFailures)
//
// For multiple errors, use errors.Join (Go 1.20+):
//
//	return errors.Join(utils.ErrCopyFailures, tableErr1, tableErr2)
//
// # Error Hierarchy
//
// - ErrCanceled, ErrDeadlineExceeded: Context-related (transient, may retry)
// - ErrInvalidConfig: Configuration issues (fatal, user must fix)
// - ErrCopyFailures: Operation failures (partial success possible)
// - ErrFKBackupExists, ErrFKRecoveryFailed: FK management issues (may need manual intervention)

var (
	// ErrCanceled indicates the operation was canceled (typically via context cancellation).
	// This is usually triggered by user interrupt (Ctrl+C) or programmatic shutdown.
	ErrCanceled = errors.New("operation canceled")

	// ErrDeadlineExceeded indicates the operation exceeded its deadline (context timeout).
	// Consider increasing timeouts or using config.NoTimeouts for large operations.
	ErrDeadlineExceeded = errors.New("deadline exceeded")

	// ErrInvalidConfig indicates invalid or inconsistent configuration values.
	// The wrapped message should identify the specific configuration problem.
	ErrInvalidConfig = errors.New("invalid configuration")

	// ErrCopyFailures aggregates one or more errors that occurred during copy operations.
	// Use errors.Join to combine this with individual table errors for full context.
	ErrCopyFailures = errors.New("one or more copy operations failed")

	// ErrFKBackupExists indicates an FK backup file exists from a previous run.
	// This suggests a previous run was interrupted. Manual review may be needed
	// to restore foreign keys before proceeding.
	ErrFKBackupExists = errors.New("foreign key backup file exists")

	// ErrFKRecoveryFailed indicates attempts to recover FKs from the backup file encountered errors.
	// Some foreign keys may be in an inconsistent state - manual database inspection recommended.
	ErrFKRecoveryFailed = errors.New("foreign key backup recovery failed")
)
