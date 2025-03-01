package util

import (
	"math/rand"
	"time"
)

// CalculateBackoff calculates a backoff duration based on the number of previous failures
// using Kubernetes-style exponential backoff (1s, 2s, 4s, 8s, etc.)
func CalculateBackoff(consecutiveFailures int) time.Duration {
	if consecutiveFailures <= 0 {
		return 0
	}

	// Base delay is 1 second (following k8s pattern)
	baseDelay := 1 * time.Second

	// Maximum delay to cap at (5 minutes)
	maxDelay := 5 * time.Minute

	// Calculate exponential backoff: 2^(failures-1) * baseDelay
	// failures=1 → 1s, failures=2 → 2s, failures=3 → 4s, failures=4 → 8s, etc.
	backoffSeconds := 1 << uint(consecutiveFailures-1)
	backoff := time.Duration(backoffSeconds) * baseDelay

	// Add jitter to prevent thundering herd problem (±20%)
	// Use a predictable source of randomness for testing
	jitterFactor := 0.8 + 0.4*rand.Float64() // random value between 0.8 and 1.2
	jitteredBackoff := time.Duration(float64(backoff) * jitterFactor)

	// Cap at maximum delay
	if jitteredBackoff > maxDelay {
		return maxDelay
	}

	return jitteredBackoff
}

// Init initializes the random source
// In Go 1.20+ we don't need to explicitly set the seed as it's done automatically
func init() {
	// Modern approach - Go 1.20+ automatically initializes the global
	// random source with a secure random seed
}
