package replication

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRetryableError_Error(t *testing.T) {
	innerErr := errors.New("connection failed")
	retryable := &RetryableError{Err: innerErr}

	assert.Equal(t, "connection failed", retryable.Error())
}

func TestRetryableError_PreservesInnerError(t *testing.T) {
	innerErr := errors.New("timeout occurred")
	retryable := &RetryableError{Err: innerErr}

	assert.Equal(t, innerErr.Error(), retryable.Error())
	assert.Equal(t, innerErr, retryable.Err)
}

func TestWithRetry_SuccessOnFirstAttempt(t *testing.T) {
	ctx := context.Background()
	callCount := 0

	err := withRetry(ctx, 3, 10*time.Millisecond, func() error {
		callCount++
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 1, callCount, "Operation should be called only once on success")
}

func TestWithRetry_SuccessAfterRetries(t *testing.T) {
	ctx := context.Background()
	callCount := 0

	err := withRetry(ctx, 5, 10*time.Millisecond, func() error {
		callCount++
		if callCount < 3 {
			return &RetryableError{Err: errors.New("transient failure")}
		}
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 3, callCount, "Operation should be retried until success")
}

func TestWithRetry_NonRetryableErrorStopsImmediately(t *testing.T) {
	ctx := context.Background()
	callCount := 0

	err := withRetry(ctx, 5, 10*time.Millisecond, func() error {
		callCount++
		return errors.New("non-retryable error")
	})

	assert.Error(t, err)
	assert.Equal(t, 1, callCount, "Should not retry on non-retryable error")
	assert.Contains(t, err.Error(), "non-retryable error")
}

func TestWithRetry_ExhaustsAllRetries(t *testing.T) {
	ctx := context.Background()
	callCount := 0
	maxRetries := 3

	err := withRetry(ctx, maxRetries, 10*time.Millisecond, func() error {
		callCount++
		return &RetryableError{Err: errors.New("always fails")}
	})

	assert.Error(t, err)
	assert.Equal(t, maxRetries, callCount, "Should try all retries")
	assert.Contains(t, err.Error(), "operation failed after")
	assert.Contains(t, err.Error(), "attempts")
}

func TestWithRetry_RespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	callCount := 0

	// Cancel after first attempt
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := withRetry(ctx, 10, 100*time.Millisecond, func() error {
		callCount++
		return &RetryableError{Err: errors.New("transient")}
	})

	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
	assert.LessOrEqual(t, callCount, 2, "Should stop after context is cancelled")
}

func TestPVCSyncOptions_Struct(t *testing.T) {
	sourcePVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "source-pvc",
			Namespace: "source-ns",
		},
	}
	destPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dest-pvc",
			Namespace: "dest-ns",
		},
	}

	opts := PVCSyncOptions{
		SourcePVC:            sourcePVC,
		DestinationPVC:       destPVC,
		SourceNamespace:      "source-ns",
		DestinationNamespace: "dest-ns",
		SourceNode:           "node-1",
		DestinationNode:      "node-2",
		TempPodKeySecretName: "ssh-key-secret",
		RsyncOptions:         []string{"-avz", "--progress"},
	}

	assert.Equal(t, sourcePVC, opts.SourcePVC)
	assert.Equal(t, destPVC, opts.DestinationPVC)
	assert.Equal(t, "source-ns", opts.SourceNamespace)
	assert.Equal(t, "dest-ns", opts.DestinationNamespace)
	assert.Equal(t, "node-1", opts.SourceNode)
	assert.Equal(t, "node-2", opts.DestinationNode)
	assert.Equal(t, "ssh-key-secret", opts.TempPodKeySecretName)
	assert.Equal(t, []string{"-avz", "--progress"}, opts.RsyncOptions)
}

func TestNamespaceMappingPVCSyncStatus_Struct(t *testing.T) {
	now := metav1.Now()
	later := metav1.NewTime(now.Add(1 * time.Hour))

	status := NamespaceMappingPVCSyncStatus{
		Phase:        "Running",
		Message:      "Syncing PVCs",
		LastSyncTime: &now,
		NextSyncTime: &later,
	}

	assert.Equal(t, "Running", status.Phase)
	assert.Equal(t, "Syncing PVCs", status.Message)
	assert.Equal(t, &now, status.LastSyncTime)
	assert.Equal(t, &later, status.NextSyncTime)
}

func TestNamespaceMappingPVCSyncStatus_Phases(t *testing.T) {
	phases := []string{"Pending", "Running", "Completed", "Failed"}

	for _, phase := range phases {
		status := NamespaceMappingPVCSyncStatus{
			Phase: phase,
		}
		assert.Equal(t, phase, status.Phase)
	}
}

func TestPVCSyncer_Struct(t *testing.T) {
	syncer := &PVCSyncer{
		SourceNamespace:      "source-ns",
		DestinationNamespace: "dest-ns",
	}

	assert.Equal(t, "source-ns", syncer.SourceNamespace)
	assert.Equal(t, "dest-ns", syncer.DestinationNamespace)
	assert.Nil(t, syncer.SourceClient)
	assert.Nil(t, syncer.DestinationClient)
}

func TestReplicationModeConstants(t *testing.T) {
	// Verify the constants match the API definitions
	assert.Equal(t, "Scheduled", string(ScheduledMode))
	assert.Equal(t, "Continuous", string(ContinuousMode))
	assert.Equal(t, "Manual", string(ManualMode))
}

func TestWithRetry_ZeroMaxRetries(t *testing.T) {
	ctx := context.Background()
	callCount := 0

	// With 0 max retries, operation should not be called
	err := withRetry(ctx, 0, 10*time.Millisecond, func() error {
		callCount++
		return &RetryableError{Err: errors.New("failure")}
	})

	assert.Error(t, err)
	assert.Equal(t, 0, callCount, "Should not call operation with 0 max retries")
}

func TestWithRetry_SingleRetry(t *testing.T) {
	ctx := context.Background()
	callCount := 0

	err := withRetry(ctx, 1, 10*time.Millisecond, func() error {
		callCount++
		return &RetryableError{Err: errors.New("failure")}
	})

	assert.Error(t, err)
	assert.Equal(t, 1, callCount, "Should call operation exactly once with 1 max retry")
}

func TestWithRetry_BackoffIncreases(t *testing.T) {
	ctx := context.Background()
	baseBackoff := 10 * time.Millisecond
	callTimes := make([]time.Time, 0)

	err := withRetry(ctx, 4, baseBackoff, func() error {
		callTimes = append(callTimes, time.Now())
		return &RetryableError{Err: errors.New("failure")}
	})

	assert.Error(t, err)
	assert.Len(t, callTimes, 4)

	// Verify backoff is exponential (each gap should be roughly double the previous)
	if len(callTimes) >= 3 {
		gap1 := callTimes[1].Sub(callTimes[0])
		gap2 := callTimes[2].Sub(callTimes[1])

		// gap2 should be approximately 2x gap1 (with some tolerance)
		assert.Greater(t, gap2, gap1, "Backoff should increase between retries")
	}
}

func TestWithRetry_MixedErrors(t *testing.T) {
	ctx := context.Background()
	callCount := 0

	// Start with retryable errors, then return non-retryable
	err := withRetry(ctx, 5, 10*time.Millisecond, func() error {
		callCount++
		if callCount < 3 {
			return &RetryableError{Err: errors.New("transient")}
		}
		return errors.New("permanent failure")
	})

	assert.Error(t, err)
	assert.Equal(t, 3, callCount, "Should stop on first non-retryable error")
	assert.Equal(t, "permanent failure", err.Error())
}
