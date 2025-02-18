package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RemoteCluster is the Schema for the remoteclusters API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Health",type="string",JSONPath=".status.health",description="Health status of the remote cluster connection"
// +kubebuilder:printcolumn:name="Last Sync",type="date",JSONPath=".status.lastSyncTime",description="Time of last successful synchronization"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:shortName=rc
type RemoteCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RemoteClusterSpec   `json:"spec"`
	Status RemoteClusterStatus `json:"status,omitempty"`
}

type RemoteClusterSpec struct {
	// KubeconfigSecretRef references a secret containing the kubeconfig for this cluster
	KubeconfigSecretRef KubeconfigSecretRef `json:"kubeconfigSecretRef"`

	// DefaultSchedule is the default schedule for replications using this cluster
	// +optional
	DefaultSchedule string `json:"defaultSchedule,omitempty"`

	// DefaultResourceTypes is the default list of resource types to replicate
	// +optional
	DefaultResourceTypes []string `json:"defaultResourceTypes,omitempty"`
}

type KubeconfigSecretRef struct {
	// Name is the name of the secret
	Name string `json:"name"`

	// Namespace is the namespace of the secret
	Namespace string `json:"namespace"`

	// Key is the key in the secret containing the kubeconfig
	// +optional
	Key string `json:"key,omitempty"`
}

type RemoteClusterStatus struct {
	// Health represents the current health status of the remote cluster connection
	// +optional
	Health string `json:"health,omitempty"`

	// LastSyncTime is the last time the cluster was synced
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// Conditions represent the latest available observations of the cluster's state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// RemoteClusterList contains a list of RemoteCluster
// +kubebuilder:object:root=true
type RemoteClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RemoteCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RemoteCluster{}, &RemoteClusterList{})
}
