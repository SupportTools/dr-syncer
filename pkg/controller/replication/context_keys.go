package replication

import (
	"github.com/supporttools/dr-syncer/pkg/contextkeys"
)

// This file contains package-level type definitions for context keys
// to ensure they're only defined once but available throughout the package.

// Internal context key types
type configContextKey string
type syncerContextKey string

// Exported context key types for cross-package use
type ConfigContextKey = contextkeys.ContextKey
type SyncerContextKey = contextkeys.ContextKey
type PVCClusterContextKey string

// Context key constants
const (
	// Internal package keys
	k8sConfigKey configContextKey = "k8s-config"
	syncerKey    syncerContextKey = "pvcsync"

	// Exported keys for cross-package use
	K8sConfigKey  = contextkeys.K8sConfigKey
	SyncerKey     = contextkeys.SyncerKey
	PVCClusterKey PVCClusterContextKey = "pvcCluster"
)
