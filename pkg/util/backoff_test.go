package util

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCalculateBackoff_ZeroFailures(t *testing.T) {
	// Zero failures should return zero backoff
	result := CalculateBackoff(0)
	assert.Equal(t, time.Duration(0), result, "Zero failures should return zero duration")
}

func TestCalculateBackoff_NegativeFailures(t *testing.T) {
	// Negative failures should return zero backoff
	result := CalculateBackoff(-1)
	assert.Equal(t, time.Duration(0), result, "Negative failures should return zero duration")

	result = CalculateBackoff(-100)
	assert.Equal(t, time.Duration(0), result, "Large negative failures should return zero duration")
}

func TestCalculateBackoff_ExponentialGrowth(t *testing.T) {
	// Test that backoff grows exponentially (with jitter tolerance)
	// failures=1 → ~1s, failures=2 → ~2s, failures=3 → ~4s, failures=4 → ~8s

	testCases := []struct {
		failures    int
		expectedMin time.Duration
		expectedMax time.Duration
	}{
		{1, 800 * time.Millisecond, 1200 * time.Millisecond},    // 1s ± 20%
		{2, 1600 * time.Millisecond, 2400 * time.Millisecond},   // 2s ± 20%
		{3, 3200 * time.Millisecond, 4800 * time.Millisecond},   // 4s ± 20%
		{4, 6400 * time.Millisecond, 9600 * time.Millisecond},   // 8s ± 20%
		{5, 12800 * time.Millisecond, 19200 * time.Millisecond}, // 16s ± 20%
	}

	for _, tc := range testCases {
		// Run multiple times to account for jitter
		for i := 0; i < 10; i++ {
			result := CalculateBackoff(tc.failures)
			assert.GreaterOrEqual(t, result, tc.expectedMin,
				"Backoff for %d failures should be >= %v (got %v)", tc.failures, tc.expectedMin, result)
			assert.LessOrEqual(t, result, tc.expectedMax,
				"Backoff for %d failures should be <= %v (got %v)", tc.failures, tc.expectedMax, result)
		}
	}
}

func TestCalculateBackoff_MaximumCap(t *testing.T) {
	// Test that backoff is capped at 5 minutes
	maxDelay := 5 * time.Minute

	// Very high failure count should still be capped
	testFailures := []int{10, 15, 20, 100}

	for _, failures := range testFailures {
		result := CalculateBackoff(failures)
		assert.LessOrEqual(t, result, maxDelay,
			"Backoff for %d failures should be capped at %v (got %v)", failures, maxDelay, result)
	}
}

func TestCalculateBackoff_JitterVariation(t *testing.T) {
	// Test that jitter causes variation in results
	// Run multiple times and ensure we get different values
	failures := 5

	results := make(map[time.Duration]bool)
	for i := 0; i < 20; i++ {
		result := CalculateBackoff(failures)
		results[result] = true
	}

	// With jitter, we should get multiple different values
	// In practice we'd expect variation, but with random jitter
	// we should definitely get more than 1 unique value over 20 runs
	assert.Greater(t, len(results), 1,
		"Jitter should cause variation in backoff values")
}

func TestCalculateBackoff_FirstFailure(t *testing.T) {
	// First failure should be approximately 1 second (± jitter)
	for i := 0; i < 10; i++ {
		result := CalculateBackoff(1)
		assert.GreaterOrEqual(t, result, 800*time.Millisecond,
			"First failure backoff should be at least 0.8s")
		assert.LessOrEqual(t, result, 1200*time.Millisecond,
			"First failure backoff should be at most 1.2s")
	}
}

func TestCalculateBackoff_ProgressionPattern(t *testing.T) {
	// Test that average backoff roughly doubles with each failure level
	// (accounting for jitter)
	samples := 100

	var prev float64
	for failures := 1; failures <= 5; failures++ {
		var total time.Duration
		for i := 0; i < samples; i++ {
			total += CalculateBackoff(failures)
		}
		avg := float64(total) / float64(samples)

		if prev > 0 {
			ratio := avg / prev
			// Should be roughly 2x (allowing for jitter)
			assert.Greater(t, ratio, 1.5, "Backoff should roughly double between %d and %d failures", failures-1, failures)
			assert.Less(t, ratio, 2.5, "Backoff ratio should be roughly 2x between %d and %d failures", failures-1, failures)
		}
		prev = avg
	}
}

func TestCalculateBackoff_BoundaryValues(t *testing.T) {
	// Test boundary where exponential growth hits the cap
	// 2^(n-1) * 1s is the base delay before jitter
	// With jitter (0.8-1.2x), we need base delay well above 5 minutes to guarantee cap

	maxDelay := 5 * time.Minute

	// At 10 failures: 2^9 = 512 seconds = 8m32s base
	// With jitter (0.8x): 6m50s - still above 5 minute cap
	// So 10+ failures should always hit the cap
	for i := 0; i < 10; i++ {
		result := CalculateBackoff(10)
		assert.Equal(t, maxDelay, result, "10 failures should hit the 5 minute cap")
	}

	// At 9 failures: 2^8 = 256 seconds = 4m16s base
	// With jitter: 3m25s to 5m8s - sometimes hits cap, sometimes doesn't
	// So we just verify it's bounded
	result := CalculateBackoff(9)
	assert.LessOrEqual(t, result, maxDelay, "9 failures should not exceed cap")
}
