package replication

// This file contains package-level type definitions for context keys
// to ensure they're only defined once but available throughout the package.

// Context key types
type configContextKey string
type syncerContextKey string

// Context key constants
const (
	k8sConfigKey configContextKey = "k8s-config"
	syncerKey    syncerContextKey = "pvcsync"
)
