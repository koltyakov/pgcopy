package utils

import (
	"errors"
	"fmt"
	"testing"
)

func TestSentinelErrors_Defined(t *testing.T) {
	// Verify all sentinel errors are non-nil
	sentinels := []struct {
		name string
		err  error
	}{
		{"ErrCanceled", ErrCanceled},
		{"ErrDeadlineExceeded", ErrDeadlineExceeded},
		{"ErrInvalidConfig", ErrInvalidConfig},
		{"ErrCopyFailures", ErrCopyFailures},
		{"ErrFKBackupExists", ErrFKBackupExists},
		{"ErrFKRecoveryFailed", ErrFKRecoveryFailed},
	}

	for _, tt := range sentinels {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err == nil {
				t.Errorf("%s should not be nil", tt.name)
			}
			if tt.err.Error() == "" {
				t.Errorf("%s should have a non-empty message", tt.name)
			}
		})
	}
}

func TestSentinelErrors_Wrapping(t *testing.T) {
	tests := []struct {
		name     string
		sentinel error
		wrapped  error
	}{
		{
			"ErrCanceled wrapped",
			ErrCanceled,
			fmt.Errorf("context canceled: %w", ErrCanceled),
		},
		{
			"ErrInvalidConfig wrapped",
			ErrInvalidConfig,
			fmt.Errorf("source connection missing: %w", ErrInvalidConfig),
		},
		{
			"ErrCopyFailures wrapped",
			ErrCopyFailures,
			fmt.Errorf("3 tables failed: %w", ErrCopyFailures),
		},
		{
			"ErrFKBackupExists wrapped",
			ErrFKBackupExists,
			fmt.Errorf("found fk_backup.csv: %w", ErrFKBackupExists),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !errors.Is(tt.wrapped, tt.sentinel) {
				t.Errorf("errors.Is(%v, %v) should return true", tt.wrapped, tt.sentinel)
			}
		})
	}
}

func TestSentinelErrors_NotEqual(t *testing.T) {
	// Verify each sentinel error is unique
	sentinels := []error{
		ErrCanceled,
		ErrDeadlineExceeded,
		ErrInvalidConfig,
		ErrCopyFailures,
		ErrFKBackupExists,
		ErrFKRecoveryFailed,
	}

	for i, err1 := range sentinels {
		for j, err2 := range sentinels {
			if i != j {
				if errors.Is(err1, err2) {
					t.Errorf("Error %v should not match %v", err1, err2)
				}
			}
		}
	}
}

func TestSentinelErrors_Messages(t *testing.T) {
	tests := []struct {
		err      error
		contains string
	}{
		{ErrCanceled, "canceled"},
		{ErrDeadlineExceeded, "deadline"},
		{ErrInvalidConfig, "invalid"},
		{ErrCopyFailures, "failed"},
		{ErrFKBackupExists, "backup"},
		{ErrFKRecoveryFailed, "recovery"},
	}

	for _, tt := range tests {
		t.Run(tt.err.Error(), func(t *testing.T) {
			msg := tt.err.Error()
			if len(msg) == 0 {
				t.Error("Error message should not be empty")
			}
		})
	}
}

func TestSentinelErrors_Unwrap(t *testing.T) {
	// Create a wrapped error chain
	base := ErrCopyFailures
	wrapped1 := fmt.Errorf("table users failed: %w", base)
	wrapped2 := fmt.Errorf("batch operation failed: %w", wrapped1)

	// Should be able to unwrap through the chain
	if !errors.Is(wrapped2, ErrCopyFailures) {
		t.Error("Should be able to unwrap through multiple layers")
	}

	// Unwrap should return the direct wrapper
	unwrapped := errors.Unwrap(wrapped2)
	if unwrapped == nil {
		t.Error("Unwrap should return the wrapped error")
	}
	if !errors.Is(unwrapped, ErrCopyFailures) {
		t.Error("Unwrapped error should still match ErrCopyFailures")
	}
}
