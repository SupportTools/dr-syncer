package errors

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNewRetryableError(t *testing.T) {
	innerErr := errors.New("connection failed")
	syncErr := NewRetryableError(innerErr, "Deployment/my-app")

	assert.NotNil(t, syncErr)
	assert.Equal(t, RetryableError, syncErr.Category)
	assert.Equal(t, "Deployment/my-app", syncErr.Resource)
	assert.Equal(t, innerErr, syncErr.Err)
}

func TestNewNonRetryableError(t *testing.T) {
	innerErr := errors.New("invalid configuration")
	syncErr := NewNonRetryableError(innerErr, "ConfigMap/settings")

	assert.NotNil(t, syncErr)
	assert.Equal(t, NonRetryableError, syncErr.Category)
	assert.Equal(t, "ConfigMap/settings", syncErr.Resource)
	assert.Equal(t, innerErr, syncErr.Err)
}

func TestNewWaitForNextSyncError(t *testing.T) {
	innerErr := errors.New("resource busy")
	syncErr := NewWaitForNextSyncError(innerErr, "Secret/credentials")

	assert.NotNil(t, syncErr)
	assert.Equal(t, WaitForNextSyncError, syncErr.Category)
	assert.Equal(t, "Secret/credentials", syncErr.Resource)
	assert.Equal(t, innerErr, syncErr.Err)
}

func TestSyncError_Error(t *testing.T) {
	innerErr := errors.New("something went wrong")
	syncErr := NewRetryableError(innerErr, "Pod/test-pod")

	errorMsg := syncErr.Error()

	assert.Equal(t, "Pod/test-pod: something went wrong", errorMsg)
}

func TestSyncError_Error_DifferentResources(t *testing.T) {
	testCases := []struct {
		resource string
		errMsg   string
		expected string
	}{
		{"Deployment/app", "failed", "Deployment/app: failed"},
		{"Service/api", "timeout", "Service/api: timeout"},
		{"", "no resource", ": no resource"},
	}

	for _, tc := range testCases {
		syncErr := NewRetryableError(errors.New(tc.errMsg), tc.resource)
		assert.Equal(t, tc.expected, syncErr.Error())
	}
}

func TestIsRetryable_RetryableSyncError(t *testing.T) {
	syncErr := NewRetryableError(errors.New("timeout"), "Deployment/app")

	assert.True(t, IsRetryable(syncErr), "RetryableError should be retryable")
}

func TestIsRetryable_NonRetryableSyncError(t *testing.T) {
	syncErr := NewNonRetryableError(errors.New("invalid"), "Deployment/app")

	assert.False(t, IsRetryable(syncErr), "NonRetryableError should not be retryable")
}

func TestIsRetryable_WaitForNextSyncError(t *testing.T) {
	syncErr := NewWaitForNextSyncError(errors.New("busy"), "Deployment/app")

	assert.False(t, IsRetryable(syncErr), "WaitForNextSyncError should not be retryable")
}

func TestIsRetryable_KubernetesServerTimeout(t *testing.T) {
	// Create a Kubernetes ServerTimeout error
	err := &metav1.Status{
		Status:  metav1.StatusFailure,
		Reason:  metav1.StatusReasonServerTimeout,
		Message: "server timeout",
	}
	statusErr := &statusError{status: err}

	assert.True(t, IsRetryable(statusErr), "Kubernetes ServerTimeout should be retryable")
}

func TestIsRetryable_KubernetesTooManyRequests(t *testing.T) {
	err := &metav1.Status{
		Status:  metav1.StatusFailure,
		Reason:  metav1.StatusReasonTooManyRequests,
		Message: "too many requests",
	}
	statusErr := &statusError{status: err}

	assert.True(t, IsRetryable(statusErr), "Kubernetes TooManyRequests should be retryable")
}

func TestIsRetryable_KubernetesServiceUnavailable(t *testing.T) {
	err := &metav1.Status{
		Status:  metav1.StatusFailure,
		Reason:  metav1.StatusReasonServiceUnavailable,
		Message: "service unavailable",
		Code:    http.StatusServiceUnavailable,
	}
	statusErr := &statusError{status: err}

	assert.True(t, IsRetryable(statusErr), "Kubernetes ServiceUnavailable should be retryable")
}

func TestIsRetryable_KubernetesInternalError(t *testing.T) {
	err := &metav1.Status{
		Status:  metav1.StatusFailure,
		Reason:  metav1.StatusReasonInternalError,
		Message: "internal error",
		Code:    http.StatusInternalServerError,
	}
	statusErr := &statusError{status: err}

	assert.True(t, IsRetryable(statusErr), "Kubernetes InternalError should be retryable")
}

func TestIsRetryable_RegularError(t *testing.T) {
	err := errors.New("generic error")

	assert.False(t, IsRetryable(err), "Regular error should not be retryable")
}

func TestIsRetryable_NilError(t *testing.T) {
	assert.False(t, IsRetryable(nil), "Nil error should not be retryable")
}

func TestShouldWaitForNextSync_WaitForNextSyncError(t *testing.T) {
	syncErr := NewWaitForNextSyncError(errors.New("busy"), "PVC/data")

	assert.True(t, ShouldWaitForNextSync(syncErr), "WaitForNextSyncError should wait for next sync")
}

func TestShouldWaitForNextSync_RetryableError(t *testing.T) {
	syncErr := NewRetryableError(errors.New("timeout"), "PVC/data")

	assert.False(t, ShouldWaitForNextSync(syncErr), "RetryableError should not wait for next sync")
}

func TestShouldWaitForNextSync_NonRetryableError(t *testing.T) {
	syncErr := NewNonRetryableError(errors.New("invalid"), "PVC/data")

	assert.False(t, ShouldWaitForNextSync(syncErr), "NonRetryableError should not wait for next sync")
}

func TestShouldWaitForNextSync_RegularError(t *testing.T) {
	err := errors.New("generic error")

	assert.False(t, ShouldWaitForNextSync(err), "Regular error should not wait for next sync")
}

func TestShouldWaitForNextSync_NilError(t *testing.T) {
	assert.False(t, ShouldWaitForNextSync(nil), "Nil error should not wait for next sync")
}

func TestErrorCategory_Values(t *testing.T) {
	// Verify the category constants have expected values
	assert.Equal(t, ErrorCategory(0), RetryableError)
	assert.Equal(t, ErrorCategory(1), NonRetryableError)
	assert.Equal(t, ErrorCategory(2), WaitForNextSyncError)
}

// statusError implements the error interface and apierrors.APIStatus
// to simulate Kubernetes API errors in tests
type statusError struct {
	status *metav1.Status
}

func (e *statusError) Error() string {
	return e.status.Message
}

func (e *statusError) Status() metav1.Status {
	return *e.status
}
