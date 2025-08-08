package utils // nolint:revive // utils is an acceptable name for internal utility package

import "errors"

// Sentinel errors used for branching and classification across the app.
// Keep these small and generic; attach context with fmt.Errorf("%w: ...", ErrX).

var (
	// ErrCanceled indicates the operation was canceled (typically via context cancellation).
	ErrCanceled = errors.New("operation canceled")

	// ErrDeadlineExceeded indicates the operation exceeded its deadline (context timeout).
	ErrDeadlineExceeded = errors.New("deadline exceeded")

	// ErrInvalidConfig indicates invalid or inconsistent configuration values.
	ErrInvalidConfig = errors.New("invalid configuration")

	// ErrCopyFailures aggregates one or more errors that occurred during copy operations.
	ErrCopyFailures = errors.New("one or more copy operations failed")

	// ErrFKBackupExists indicates an FK backup file exists from a previous run and needs attention.
	ErrFKBackupExists = errors.New("foreign key backup file exists")

	// ErrFKRecoveryFailed indicates attempts to recover FKs from the backup file encountered errors.
	ErrFKRecoveryFailed = errors.New("foreign key backup recovery failed")
)
