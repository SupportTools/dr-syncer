package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RemoteCluster is the Schema for the remoteclusters API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Health",type="string",JSONPath=".status.health",description="Health status of the remote cluster connection"
// +kubebuilder:printcolumn:name="Last Sync",type="date",JSONPath=".status.lastSyncTime",description="Time of last successful synchronization"
// +kubebuilder:printcolumn:name="PVC Sync",type="string",JSONPath=".status.pvcSync.phase",description="PVC sync status"
// +kubebuilder:printcolumn:name="Ready Agents",type="string",JSONPath=".status.pvcSync.agentStatus.readyNodes",description="Number of ready PVC sync agents"
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

	// PVCSync configures PVC synchronization for this cluster
	// +optional
	PVCSync *PVCSyncSpec `json:"pvcSync,omitempty"`
}

// PVCSyncSpec defines the configuration for PVC synchronization
type PVCSyncSpec struct {
	// Enabled indicates whether PVC synchronization is enabled
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Image specifies the PVC sync agent image
	// +optional
	Image *PVCSyncImage `json:"image,omitempty"`

	// SSH configures the SSH service for rsync
	// +optional
	SSH *PVCSyncSSH `json:"ssh,omitempty"`

	// Concurrency is the maximum number of concurrent PVC syncs
	// +optional
	Concurrency *int32 `json:"concurrency,omitempty"`

	// RetryConfig configures retry behavior for failed syncs
	// +optional
	RetryConfig *PVCSyncRetryConfig `json:"retryConfig,omitempty"`
}

// PVCSyncImage defines the container image for the PVC sync agent
type PVCSyncImage struct {
	// Repository is the image repository
	// +optional
	Repository string `json:"repository,omitempty"`

	// Tag is the image tag
	// +optional
	Tag string `json:"tag,omitempty"`

	// PullPolicy defines the image pull policy
	// +optional
	PullPolicy string `json:"pullPolicy,omitempty"`
}

// PVCSyncSSH defines SSH configuration for the PVC sync agent
type PVCSyncSSH struct {
	// Port is the SSH service port
	// +optional
	Port int32 `json:"port,omitempty"`

	// KeySecretRef references a secret containing SSH keys
	// +optional
	KeySecretRef *SSHKeySecretRef `json:"keySecretRef,omitempty"`
}

// SSHKeySecretRef references a secret containing SSH keys
type SSHKeySecretRef struct {
	// Name is the name of the secret
	Name string `json:"name"`

	// Namespace is the namespace of the secret
	Namespace string `json:"namespace"`
}

// PVCSyncRetryConfig defines retry behavior for failed syncs
type PVCSyncRetryConfig struct {
	// MaxRetries is the maximum number of retry attempts
	// +optional
	MaxRetries int32 `json:"maxRetries,omitempty"`

	// InitialDelay is the initial delay between retries
	// +optional
	InitialDelay string `json:"initialDelay,omitempty"`

	// MaxDelay is the maximum delay between retries
	// +optional
	MaxDelay string `json:"maxDelay,omitempty"`
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

	// PVCSync represents the status of PVC synchronization
	// +optional
	PVCSync *PVCSyncStatus `json:"pvcSync,omitempty"`
}

// PVCSyncStatus defines the observed state of PVC synchronization
type PVCSyncStatus struct {
	// Phase is the current phase of PVC sync agent deployment
	// +optional
	Phase string `json:"phase,omitempty"`

	// AgentStatus contains the status of PVC sync agents
	// +optional
	AgentStatus *PVCSyncAgentStatus `json:"agentStatus,omitempty"`

	// LastSuccessfulSync is the last time a PVC sync was successful
	// +optional
	LastSuccessfulSync *metav1.Time `json:"lastSuccessfulSync,omitempty"`

	// FailedSyncs is the number of failed sync attempts
	// +optional
	FailedSyncs int32 `json:"failedSyncs,omitempty"`

	// Message provides additional status information
	// +optional
	Message string `json:"message,omitempty"`
}

// PVCSyncAgentStatus contains status information for PVC sync agents
type PVCSyncAgentStatus struct {
	// ReadyNodes is the number of nodes with ready agents
	// +optional
	ReadyNodes int32 `json:"readyNodes,omitempty"`

	// TotalNodes is the total number of nodes that should have agents
	// +optional
	TotalNodes int32 `json:"totalNodes,omitempty"`

	// NodeStatuses contains per-node agent status
	// +optional
	NodeStatuses map[string]PVCSyncNodeStatus `json:"nodeStatuses,omitempty"`
}

// PVCSyncNodeStatus contains status information for a node's PVC sync agent
type PVCSyncNodeStatus struct {
	// Ready indicates whether the agent is ready
	// +optional
	Ready bool `json:"ready,omitempty"`

	// LastHeartbeat is the last time the agent reported status
	// +optional
	LastHeartbeat *metav1.Time `json:"lastHeartbeat,omitempty"`

	// Message provides additional status information
	// +optional
	Message string `json:"message,omitempty"`
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
