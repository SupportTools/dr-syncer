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

// HealthCheckConfig defines configuration for health checking
type HealthCheckConfig struct {
	// Interval is the time between health checks
	// +optional
	Interval string `json:"interval,omitempty"`

	// SSHTimeout is the timeout for SSH connection attempts
	// +optional
	SSHTimeout string `json:"sshTimeout,omitempty"`

	// RetryAttempts is the number of times to retry failed health checks
	// +optional
	RetryAttempts int32 `json:"retryAttempts,omitempty"`

	// RetryInterval is the time to wait between retry attempts
	// +optional
	RetryInterval string `json:"retryInterval,omitempty"`
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

	// Deployment configures the agent deployment options
	// +optional
	Deployment *PVCSyncDeployment `json:"deployment,omitempty"`

	// HealthCheck configures health checking behavior
	// +optional
	HealthCheck *HealthCheckConfig `json:"healthCheck,omitempty"`

	// DefaultVerificationMode sets the default verification mode for all PVC syncs
	// using this cluster. Can be overridden at NamespaceMapping or per-PVC level.
	// Options: none (default), sample, full
	// +optional
	// +kubebuilder:default=none
	DefaultVerificationMode VerificationMode `json:"defaultVerificationMode,omitempty"`

	// DefaultSamplePercent sets the default sample percentage for 'sample' mode.
	// Only used when DefaultVerificationMode or inherited mode is 'sample'.
	// +optional
	// +kubebuilder:default=10
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	DefaultSamplePercent *int32 `json:"defaultSamplePercent,omitempty"`
}

// PVCSyncDeployment defines deployment configuration for the PVC sync agent
type PVCSyncDeployment struct {
	// HostNetwork determines whether to use host network for agent pods
	// +optional
	// +kubebuilder:default=true
	HostNetwork *bool `json:"hostNetwork,omitempty"`

	// NodeSelector is a map of node selector labels
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations is a list of pod tolerations in JSON format
	// +optional
	Tolerations []map[string]string `json:"tolerations,omitempty"`

	// Resources defines resource requirements for agent containers
	// +optional
	Resources *ResourceRequirements `json:"resources,omitempty"`

	// Annotations is a map of pod annotations
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`

	// Labels is a map of pod labels
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// PriorityClassName is the priority class for agent pods
	// +optional
	PriorityClassName string `json:"priorityClassName,omitempty"`

	// Privileged determines whether the container should run in privileged mode
	// +optional
	// +kubebuilder:default=true
	Privileged *bool `json:"privileged,omitempty"`

	// ExtraEnv is a list of additional environment variables
	// +optional
	ExtraEnv []EnvVar `json:"extraEnv,omitempty"`
}

// ResourceRequirements describes the compute resource requirements
type ResourceRequirements struct {
	// Limits describes the maximum amount of compute resources allowed
	// +optional
	Limits map[string]string `json:"limits,omitempty"`

	// Requests describes the minimum amount of compute resources required
	// +optional
	Requests map[string]string `json:"requests,omitempty"`
}

// EnvVar represents an environment variable
type EnvVar struct {
	// Name of the environment variable
	Name string `json:"name"`

	// Value of the environment variable
	// +optional
	Value string `json:"value,omitempty"`

	// ValueFrom source for the environment variable's value
	// +optional
	ValueFrom *EnvVarSource `json:"valueFrom,omitempty"`
}

// EnvVarSource represents a source for the value of an EnvVar
type EnvVarSource struct {
	// FieldRef selects a field of the pod
	// +optional
	FieldRef *ObjectFieldSelector `json:"fieldRef,omitempty"`
}

// ObjectFieldSelector selects a field of the pod
type ObjectFieldSelector struct {
	// Path of the field to select in the specified API version
	FieldPath string `json:"fieldPath"`
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
	// +kubebuilder:default=2222
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

	// LastDeploymentTime is when the agent was last deployed
	// +optional
	LastDeploymentTime *metav1.Time `json:"lastDeploymentTime,omitempty"`

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

// SSHConnectionStatus contains SSH connectivity information
type SSHConnectionStatus struct {
	// LastCheckTime is when SSH connectivity was last verified
	// +optional
	LastCheckTime *metav1.Time `json:"lastCheckTime,omitempty"`

	// Connected indicates if SSH connection was successful
	Connected bool `json:"connected"`

	// Error provides error information if connection failed
	// +optional
	Error string `json:"error,omitempty"`
}

// PodStatus contains agent pod status information
type PodStatus struct {
	// Phase is the current phase of the pod
	Phase string `json:"phase"`

	// Ready indicates if the pod is ready
	Ready bool `json:"ready"`

	// RestartCount is the number of times the pod has restarted
	// +optional
	RestartCount int32 `json:"restartCount,omitempty"`

	// LastTransitionTime is when the pod last changed state
	// +optional
	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`
}

// PVCSyncNodeStatus contains status information for a node's PVC sync agent
type PVCSyncNodeStatus struct {
	// Ready indicates whether the agent is ready
	// +optional
	Ready bool `json:"ready,omitempty"`

	// LastHeartbeat is the last time the agent reported status
	// +optional
	LastHeartbeat *metav1.Time `json:"lastHeartbeat,omitempty"`

	// SSHStatus contains SSH connectivity information
	// +optional
	SSHStatus *SSHConnectionStatus `json:"sshStatus,omitempty"`

	// PodStatus contains the agent pod status
	// +optional
	PodStatus *PodStatus `json:"podStatus,omitempty"`

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
