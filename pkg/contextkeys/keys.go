package contextkeys

// Define context keys to avoid string collisions by using a custom type
type ContextKey string

// String returns the string representation of the context key
func (k ContextKey) String() string {
	return string(k)
}

const (
	// K8sConfigKey is used to store the Kubernetes REST config in context
	K8sConfigKey ContextKey = "k8s-config"

	// ConfigKey is an alias for K8sConfigKey for backward compatibility
	ConfigKey ContextKey = "k8s-config"

	// SyncerKey is used to store the PVCSyncer in context
	SyncerKey ContextKey = "pvcsync"

	// SourceClusterKey is used to store the source cluster name in context
	SourceClusterKey ContextKey = "source-cluster"

	// DestClusterKey is used to store the destination cluster name in context
	DestClusterKey ContextKey = "dest-cluster"

	// ClusterTypeKey is used to store the cluster type (source/destination) in context
	ClusterTypeKey ContextKey = "cluster-type"
)
