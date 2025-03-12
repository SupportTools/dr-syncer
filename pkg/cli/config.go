package cli

// Config represents the configuration for the CLI
type Config struct {
	// Required fields
	SourceKubeconfig string
	DestKubeconfig   string
	SourceNamespace  string
	DestNamespace    string
	Mode             string // Stage, Cutover, Failback

	// Optional fields
	IncludeCustomResources bool
	MigratePVCData         bool
	ReverseMigratePVCData  bool
	ResourceTypes          []string // If empty, defaults will be used
	ExcludeResourceTypes   []string

	// PV-migrate options
	PVMigrateFlags string // Additional flags to pass to pv-migrate
}

// Standard Kubernetes resources to sync by default
var DefaultResourceTypes = []string{
	"configmaps",
	"secrets",
	"deployments",
	"statefulsets",
	"daemonsets",
	"services",
	"ingresses",
	"serviceaccounts",
	"roles",
	"rolebindings",
	"persistentvolumeclaims",
	"horizontalpodautoscalers",
	"networkpolicies",
}

// ShouldSyncResourceType determines if a resource type should be synchronized
// based on the configuration
func (c *Config) ShouldSyncResourceType(resourceType string, isCustomResource bool) bool {
	// If it's a custom resource and we're not including custom resources, skip it
	if isCustomResource && !c.IncludeCustomResources {
		return false
	}

	// If resource is in the exclude list, skip it
	for _, excludeType := range c.ExcludeResourceTypes {
		if excludeType == resourceType {
			return false
		}
	}

	// If specific resource types are provided, check if this type is included
	if len(c.ResourceTypes) > 0 {
		for _, includeType := range c.ResourceTypes {
			if includeType == resourceType {
				return true
			}
		}
		// If specific types are provided and this type isn't in the list, skip it
		return false
	}

	// If no specific types are provided, use the default list for standard resources
	if !isCustomResource {
		for _, defaultType := range DefaultResourceTypes {
			if defaultType == resourceType {
				return true
			}
		}
	}

	// If it's a custom resource and we're including custom resources, but no specific
	// types were listed, then include it (we've already checked IncludeCustomResources above)
	return isCustomResource
}
