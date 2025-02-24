package errors

import (
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// ErrorCategory defines the type of error for retry decisions
type ErrorCategory int

const (
	// RetryableError indicates the error should be retried
	RetryableError ErrorCategory = iota
	// NonRetryableError indicates the error should not be retried
	NonRetryableError
	// WaitForNextSyncError indicates the error should not be retried until next scheduled sync
	WaitForNextSyncError
)

// SyncError wraps an error with additional context
type SyncError struct {
	Err      error
	Category ErrorCategory
	Resource string
}

func (e *SyncError) Error() string {
	return fmt.Sprintf("%s: %v", e.Resource, e.Err)
}

// NewRetryableError creates a new retryable error
func NewRetryableError(err error, resource string) *SyncError {
	return &SyncError{
		Err:      err,
		Category: RetryableError,
		Resource: resource,
	}
}

// NewNonRetryableError creates a new non-retryable error
func NewNonRetryableError(err error, resource string) *SyncError {
	return &SyncError{
		Err:      err,
		Category: NonRetryableError,
		Resource: resource,
	}
}

// NewWaitForNextSyncError creates a new error that should wait for next scheduled sync
func NewWaitForNextSyncError(err error, resource string) *SyncError {
	return &SyncError{
		Err:      err,
		Category: WaitForNextSyncError,
		Resource: resource,
	}
}

// IsRetryable determines if an error should be retried
func IsRetryable(err error) bool {
	if syncErr, ok := err.(*SyncError); ok {
		return syncErr.Category == RetryableError
	}

	// Handle Kubernetes API errors
	if apierrors.IsServerTimeout(err) ||
		apierrors.IsTimeout(err) ||
		apierrors.IsTooManyRequests(err) ||
		apierrors.IsServiceUnavailable(err) ||
		apierrors.IsInternalError(err) {
		return true
	}

	return false
}

// ShouldWaitForNextSync determines if error should wait for next scheduled sync
func ShouldWaitForNextSync(err error) bool {
	if syncErr, ok := err.(*SyncError); ok {
		return syncErr.Category == WaitForNextSyncError
	}
	return false
}
