package utils

import (
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// IgnoreLabel is used to mark resources that should be ignored during replication
	// Format: "dr-syncer.io/ignore: true"
	IgnoreLabel = "dr-syncer.io/ignore"

	// ScaleOverrideLabel is used to override the scale of a deployment in the destination cluster
	// Format: "dr-syncer.io/scale-override: <number>"
	ScaleOverrideLabel = "dr-syncer.io/scale-override"
)

// ParseInt32 converts a string to int32
func ParseInt32(s string) (int32, error) {
	i, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return 0, err
	}
	return int32(i), nil
}

// ShouldIgnoreResource checks if a resource should be ignored based on labels
func ShouldIgnoreResource(obj metav1.Object) bool {
	if val, exists := obj.GetLabels()[IgnoreLabel]; exists {
		return val == "true"
	}
	return false
}

// SanitizeMetadata removes cluster-specific metadata from a resource
func SanitizeMetadata(obj metav1.Object) {
	obj.SetUID("")
	obj.SetResourceVersion("")
	obj.SetSelfLink("")
	obj.SetCreationTimestamp(metav1.Time{})
	obj.SetManagedFields(nil)
	obj.SetOwnerReferences(nil)
	obj.SetGeneration(0)
	obj.SetFinalizers(nil)
	annotations := obj.GetAnnotations()
	if annotations != nil {
		delete(annotations, "kubectl.kubernetes.io/last-applied-configuration")
		obj.SetAnnotations(annotations)
	}
}
