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
)
