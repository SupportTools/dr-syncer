package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// StorageClassMapping defines a mapping between source and destination storage classes
type StorageClassMapping struct {
	// From is the source cluster storage class name
	From string `json:"from"`
	// To is the destination cluster storage class name
	To string `json:"to"`
}

// DeepCopyInto copies StorageClassMapping into out
func (in *StorageClassMapping) DeepCopyInto(out *StorageClassMapping) {
	*out = *in
}

// DeepCopy creates a deep copy of StorageClassMapping
func (in *StorageClassMapping) DeepCopy() *StorageClassMapping {
	if in == nil {
		return nil
	}
	out := new(StorageClassMapping)
	in.DeepCopyInto(out)
	return out
}

// AccessModeMapping defines a mapping between source and destination access modes
type AccessModeMapping struct {
	// From is the source cluster access mode
	From string `json:"from"`
	// To is the destination cluster access mode
	To string `json:"to"`
}

// DeepCopyInto copies AccessModeMapping into out
func (in *AccessModeMapping) DeepCopyInto(out *AccessModeMapping) {
	*out = *in
}

// DeepCopy creates a deep copy of AccessModeMapping
func (in *AccessModeMapping) DeepCopy() *AccessModeMapping {
	if in == nil {
		return nil
	}
	out := new(AccessModeMapping)
	in.DeepCopyInto(out)
	return out
}

// PVCConfig defines configuration for PVC replication
type PVCConfig struct {
	// SyncPersistentVolumes determines whether to sync PVs when StorageClass supports multi-cluster attachment.
	// When true, the PV will be synced to the destination cluster.
	// When false (default), a new PV will be created by the storage provisioner.
	// This can be overridden per-PVC using the 'dr-syncer.io/sync-pv' label.
	// +optional
	// +kubebuilder:default=false
	SyncPersistentVolumes bool `json:"syncPersistentVolumes,omitempty"`

	// StorageClassMappings defines mappings to convert storage classes between clusters.
	// This allows using different storage classes in the destination cluster.
	// If a mapping is not found, the original storage class name will be used.
	// This can be overridden per-PVC using the 'dr-syncer.io/storage-class' label.
	// +optional
	StorageClassMappings []StorageClassMapping `json:"storageClassMappings,omitempty"`

	// AccessModeMappings defines mappings to convert access modes between clusters.
	// This allows using different access modes in the destination cluster.
	// If a mapping is not found, the original access mode will be used.
	// This can be overridden per-PVC using the 'dr-syncer.io/access-mode' label.
	// +optional
	AccessModeMappings []AccessModeMapping `json:"accessModeMappings,omitempty"`

	// PreserveVolumeAttributes determines whether to preserve volume attributes when creating new PVs.
	// When true, volume attributes like filesystem type, mount options, etc. will be preserved.
	// When false (default), the storage class defaults will be used.
	// +optional
	// +kubebuilder:default=false
	PreserveVolumeAttributes bool `json:"preserveVolumeAttributes,omitempty"`

	// SyncData determines whether to sync the data inside PVCs between clusters.
	// When true, the data will be synced from source to destination PVCs.
	// When false (default), only the PVC resources will be synced.
	// +optional
	// +kubebuilder:default=false
	SyncData bool `json:"syncData,omitempty"`

	// DataSyncConfig defines configuration for PVC data synchronization.
	// Only used when SyncData is true.
	// +optional
	DataSyncConfig *PVCDataSyncConfig `json:"dataSyncConfig,omitempty"`
}

// PVCDataSyncConfig defines configuration for PVC data synchronization
type PVCDataSyncConfig struct {
	// ConcurrentSyncs is the maximum number of concurrent PVC data syncs.
	// +optional
	// +kubebuilder:default=2
	ConcurrentSyncs *int32 `json:"concurrentSyncs,omitempty"`

	// ExcludePaths is a list of paths to exclude from synchronization.
	// Paths are relative to the PVC mount point.
	// +optional
	ExcludePaths []string `json:"excludePaths,omitempty"`

	// RsyncOptions is a list of additional options to pass to rsync.
	// +optional
	RsyncOptions []string `json:"rsyncOptions,omitempty"`

	// Timeout is the maximum time to wait for a sync operation to complete.
	// +optional
	// +kubebuilder:default="30m"
	Timeout *metav1.Duration `json:"timeout,omitempty"`
}

// DeepCopyInto copies PVCDataSyncConfig into out
func (in *PVCDataSyncConfig) DeepCopyInto(out *PVCDataSyncConfig) {
	*out = *in
	if in.ConcurrentSyncs != nil {
		in, out := &in.ConcurrentSyncs, &out.ConcurrentSyncs
		*out = new(int32)
		**out = **in
	}
	if in.ExcludePaths != nil {
		in, out := &in.ExcludePaths, &out.ExcludePaths
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.RsyncOptions != nil {
		in, out := &in.RsyncOptions, &out.RsyncOptions
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Timeout != nil {
		in, out := &in.Timeout, &out.Timeout
		*out = new(metav1.Duration)
		**out = **in
	}
}

// DeepCopy creates a deep copy of PVCDataSyncConfig
func (in *PVCDataSyncConfig) DeepCopy() *PVCDataSyncConfig {
	if in == nil {
		return nil
	}
	out := new(PVCDataSyncConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies PVCConfig into out
func (in *PVCConfig) DeepCopyInto(out *PVCConfig) {
	*out = *in
	if in.StorageClassMappings != nil {
		in, out := &in.StorageClassMappings, &out.StorageClassMappings
		*out = make([]StorageClassMapping, len(*in))
		copy(*out, *in)
	}
	if in.AccessModeMappings != nil {
		in, out := &in.AccessModeMappings, &out.AccessModeMappings
		*out = make([]AccessModeMapping, len(*in))
		copy(*out, *in)
	}
	if in.DataSyncConfig != nil {
		in, out := &in.DataSyncConfig, &out.DataSyncConfig
		*out = new(PVCDataSyncConfig)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy creates a deep copy of PVCConfig
func (in *PVCConfig) DeepCopy() *PVCConfig {
	if in == nil {
		return nil
	}
	out := new(PVCConfig)
	in.DeepCopyInto(out)
	return out
}

// ImmutableResourceHandling defines how to handle immutable resources
// +kubebuilder:validation:Enum=NoChange;Recreate;RecreateWithPodDrain;PartialUpdate;ForceUpdate
type ImmutableResourceHandling string

const (
	// NoChange skips updating immutable resources and logs a warning
	NoChange ImmutableResourceHandling = "NoChange"
	// Recreate deletes and recreates the resource with new values
	Recreate ImmutableResourceHandling = "Recreate"
	// RecreateWithPodDrain safely drains pods before recreating the resource
	RecreateWithPodDrain ImmutableResourceHandling = "RecreateWithPodDrain"
	// PartialUpdate applies only mutable field changes
	PartialUpdate ImmutableResourceHandling = "PartialUpdate"
	// ForceUpdate force deletes (with cascading) and recreates the resource
	ForceUpdate ImmutableResourceHandling = "ForceUpdate"
)

// ImmutableResourceConfig defines configuration for handling immutable resources
type ImmutableResourceConfig struct {
	// DefaultHandling determines how immutable resources are handled by default
	// +optional
	// +kubebuilder:default=NoChange
	DefaultHandling ImmutableResourceHandling `json:"defaultHandling,omitempty"`

	// ResourceOverrides allows specifying handling for specific resource types
	// Format: "resource.group" (e.g. "statefulsets.apps")
	// +optional
	ResourceOverrides map[string]ImmutableResourceHandling `json:"resourceOverrides,omitempty"`

	// DrainTimeout specifies how long to wait for pod draining when using RecreateWithPodDrain
	// +optional
	// +kubebuilder:default="5m"
	DrainTimeout *metav1.Duration `json:"drainTimeout,omitempty"`

	// ForceDeleteTimeout specifies how long to wait for force deletion to complete
	// +optional
	// +kubebuilder:default="2m"
	ForceDeleteTimeout *metav1.Duration `json:"forceDeleteTimeout,omitempty"`
}

// ReplicationMode defines the type of replication
type ReplicationMode string

const (
	// ScheduledMode uses cron schedule for replication
	ScheduledMode ReplicationMode = "Scheduled"
	// ContinuousMode uses watchers and background sync
	ContinuousMode ReplicationMode = "Continuous"
	// ManualMode requires manual trigger via CRD updates
	ManualMode ReplicationMode = "Manual"
)

// ContinuousConfig defines configuration for continuous replication mode
type ContinuousConfig struct {
	// WatchResources enables real-time resource watching
	// +optional
	// +kubebuilder:default=true
	WatchResources *bool `json:"watchResources,omitempty"`

	// BackgroundSyncInterval defines the interval for full sync
	// +optional
	// +kubebuilder:default="1h"
	// +kubebuilder:validation:Pattern=^([0-9]+h)?([0-9]+m)?([0-9]+s)?$
	BackgroundSyncInterval string `json:"backgroundSyncInterval,omitempty"`
}

// RetryConfig defines configuration for retry behavior
type RetryConfig struct {
	// MaxRetries is the maximum number of retries before giving up
	// +optional
	// +kubebuilder:default=5
	MaxRetries *int32 `json:"maxRetries,omitempty"`

	// InitialBackoff is the initial backoff duration after first failure
	// +optional
	// +kubebuilder:default="5s"
	// +kubebuilder:validation:Pattern=^([0-9]+h)?([0-9]+m)?([0-9]+s)?$
	InitialBackoff string `json:"initialBackoff,omitempty"`

	// MaxBackoff is the maximum backoff duration
	// +optional
	// +kubebuilder:default="5m"
	// +kubebuilder:validation:Pattern=^([0-9]+h)?([0-9]+m)?([0-9]+s)?$
	MaxBackoff string `json:"maxBackoff,omitempty"`

	// BackoffMultiplier is the multiplier for backoff duration after each failure (as percentage)
	// +optional
	// +kubebuilder:default=200
	// +kubebuilder:validation:Minimum=100
	// +kubebuilder:validation:Maximum=1000
	BackoffMultiplier *int32 `json:"backoffMultiplier,omitempty"`
}

// DeepCopyInto copies RetryConfig into out
func (in *RetryConfig) DeepCopyInto(out *RetryConfig) {
	*out = *in
	if in.MaxRetries != nil {
		in, out := &in.MaxRetries, &out.MaxRetries
		*out = new(int32)
		**out = **in
	}
	if in.BackoffMultiplier != nil {
		in, out := &in.BackoffMultiplier, &out.BackoffMultiplier
		*out = new(int32)
		**out = **in
	}
}

// DeepCopy creates a deep copy of RetryConfig
func (in *RetryConfig) DeepCopy() *RetryConfig {
	if in == nil {
		return nil
	}
	out := new(RetryConfig)
	in.DeepCopyInto(out)
	return out
}

// FailureHandlingMode defines how to handle different types of failures
// +kubebuilder:validation:Enum=RetryAndWait;RetryOnly;WaitForNextSync;FailFast
type FailureHandlingMode string

const (
	// RetryAndWait retries with backoff, then waits for next scheduled sync
	RetryAndWait FailureHandlingMode = "RetryAndWait"
	// RetryOnly retries with backoff until success or max retries
	RetryOnly FailureHandlingMode = "RetryOnly"
	// WaitForNextSync skips retries and waits for next scheduled sync
	WaitForNextSync FailureHandlingMode = "WaitForNextSync"
	// FailFast fails immediately without retrying
	FailFast FailureHandlingMode = "FailFast"
)

// FailureHandlingConfig defines how different types of failures are handled
type FailureHandlingConfig struct {
	// DefaultMode determines how failures are handled by default
	// +optional
	// +kubebuilder:default=RetryAndWait
	DefaultMode FailureHandlingMode `json:"defaultMode,omitempty"`

	// StorageClassNotFound determines how to handle missing storage classes
	// +optional
	// +kubebuilder:default=WaitForNextSync
	StorageClassNotFound FailureHandlingMode `json:"storageClassNotFound,omitempty"`

	// ResourceNotFound determines how to handle missing resource types
	// +optional
	// +kubebuilder:default=FailFast
	ResourceNotFound FailureHandlingMode `json:"resourceNotFound,omitempty"`

	// ValidationFailure determines how to handle resource validation failures
	// +optional
	// +kubebuilder:default=FailFast
	ValidationFailure FailureHandlingMode `json:"validationFailure,omitempty"`

	// NetworkError determines how to handle network/connectivity issues
	// +optional
	// +kubebuilder:default=RetryAndWait
	NetworkError FailureHandlingMode `json:"networkError,omitempty"`
}

// DeepCopyInto copies FailureHandlingConfig into out
func (in *FailureHandlingConfig) DeepCopyInto(out *FailureHandlingConfig) {
	*out = *in
}

// DeepCopy creates a deep copy of FailureHandlingConfig
func (in *FailureHandlingConfig) DeepCopy() *FailureHandlingConfig {
	if in == nil {
		return nil
	}
	out := new(FailureHandlingConfig)
	in.DeepCopyInto(out)
	return out
}

// IngressConfig defines configuration for ingress replication
type IngressConfig struct {
	// PreserveAnnotations determines whether to maintain all ingress annotations
	// +optional
	// +kubebuilder:default=true
	PreserveAnnotations *bool `json:"preserveAnnotations,omitempty"`

	// PreserveTLS determines whether to maintain TLS configurations
	// +optional
	// +kubebuilder:default=true
	PreserveTLS *bool `json:"preserveTLS,omitempty"`

	// PreserveBackends determines whether to preserve backend service references
	// +optional
	// +kubebuilder:default=true
	PreserveBackends *bool `json:"preserveBackends,omitempty"`
}

// DeepCopyInto copies IngressConfig into out
func (in *IngressConfig) DeepCopyInto(out *IngressConfig) {
	*out = *in
	if in.PreserveAnnotations != nil {
		in, out := &in.PreserveAnnotations, &out.PreserveAnnotations
		*out = new(bool)
		**out = **in
	}
	if in.PreserveTLS != nil {
		in, out := &in.PreserveTLS, &out.PreserveTLS
		*out = new(bool)
		**out = **in
	}
	if in.PreserveBackends != nil {
		in, out := &in.PreserveBackends, &out.PreserveBackends
		*out = new(bool)
		**out = **in
	}
}

// DeepCopy creates a deep copy of IngressConfig
func (in *IngressConfig) DeepCopy() *IngressConfig {
	if in == nil {
		return nil
	}
	out := new(IngressConfig)
	in.DeepCopyInto(out)
	return out
}

// NamespaceConfig defines configuration for namespace handling
type NamespaceConfig struct {
	// CreateNamespace determines whether to create destination namespace if it doesn't exist
	// +optional
	// +kubebuilder:default=true
	CreateNamespace bool `json:"createNamespace,omitempty"`

	// PreserveLabels determines whether to maintain namespace labels
	// +optional
	// +kubebuilder:default=true
	PreserveLabels bool `json:"preserveLabels,omitempty"`

	// PreserveAnnotations determines whether to maintain namespace annotations
	// +optional
	// +kubebuilder:default=true
	PreserveAnnotations bool `json:"preserveAnnotations,omitempty"`
}

// DeepCopyInto copies NamespaceConfig into out
func (in *NamespaceConfig) DeepCopyInto(out *NamespaceConfig) {
	*out = *in
}

// DeepCopy creates a deep copy of NamespaceConfig
func (in *NamespaceConfig) DeepCopy() *NamespaceConfig {
	if in == nil {
		return nil
	}
	out := new(NamespaceConfig)
	in.DeepCopyInto(out)
	return out
}

// SecretReference references a secret
type SecretReference struct {
	// Name is the name of the secret
	Name string `json:"name"`

	// Namespace is the namespace of the secret
	Namespace string `json:"namespace"`
}

// ClusterMappingReference references a ClusterMapping resource
type ClusterMappingReference struct {
	// Name is the name of the ClusterMapping
	Name string `json:"name"`

	// Namespace is the namespace of the ClusterMapping
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// DeepCopyInto copies ClusterMappingReference into out
func (in *ClusterMappingReference) DeepCopyInto(out *ClusterMappingReference) {
	*out = *in
}

// DeepCopy creates a deep copy of ClusterMappingReference
func (in *ClusterMappingReference) DeepCopy() *ClusterMappingReference {
	if in == nil {
		return nil
	}
	out := new(ClusterMappingReference)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies SecretReference into out
func (in *SecretReference) DeepCopyInto(out *SecretReference) {
	*out = *in
}

// DeepCopy creates a deep copy of SecretReference
func (in *SecretReference) DeepCopy() *SecretReference {
	if in == nil {
		return nil
	}
	out := new(SecretReference)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies ImmutableResourceConfig into out
func (in *ImmutableResourceConfig) DeepCopyInto(out *ImmutableResourceConfig) {
	*out = *in
	if in.ResourceOverrides != nil {
		in, out := &in.ResourceOverrides, &out.ResourceOverrides
		*out = make(map[string]ImmutableResourceHandling, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.DrainTimeout != nil {
		in, out := &in.DrainTimeout, &out.DrainTimeout
		*out = new(metav1.Duration)
		**out = **in
	}
	if in.ForceDeleteTimeout != nil {
		in, out := &in.ForceDeleteTimeout, &out.ForceDeleteTimeout
		*out = new(metav1.Duration)
		**out = **in
	}
}

// DeepCopy creates a deep copy of ImmutableResourceConfig
func (in *ImmutableResourceConfig) DeepCopy() *ImmutableResourceConfig {
	if in == nil {
		return nil
	}
	out := new(ImmutableResourceConfig)
	in.DeepCopyInto(out)
	return out
}

// SyncPhase represents the current phase of replication
// +kubebuilder:validation:Enum=Pending;Running;Completed;Failed
type SyncPhase string

const (
	// SyncPhasePending indicates the replication is pending
	SyncPhasePending SyncPhase = "Pending"
	// SyncPhaseRunning indicates the replication is running
	SyncPhaseRunning SyncPhase = "Running"
	// SyncPhaseCompleted indicates the replication completed successfully
	SyncPhaseCompleted SyncPhase = "Completed"
	// SyncPhaseFailed indicates the replication failed
	SyncPhaseFailed SyncPhase = "Failed"
)

// SyncProgress tracks the progress of a sync operation
type SyncProgress struct {
	// PercentComplete indicates the percentage of completion for the current sync
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	PercentComplete int32 `json:"percentComplete"`

	// EstimatedTimeRemaining is the estimated time until sync completion
	// +optional
	// +kubebuilder:validation:Pattern=^([0-9]+h)?([0-9]+m)?([0-9]+s)?$
	EstimatedTimeRemaining string `json:"estimatedTimeRemaining,omitempty"`

	// CurrentOperation describes the current sync operation being performed
	// +optional
	CurrentOperation string `json:"currentOperation,omitempty"`

	// ResourcesRemaining is the count of resources still pending sync
	// +optional
	// +kubebuilder:validation:Minimum=0
	ResourcesRemaining int32 `json:"resourcesRemaining,omitempty"`
}

// DeepCopyInto copies SyncProgress into out
func (in *SyncProgress) DeepCopyInto(out *SyncProgress) {
	*out = *in
}

// DeepCopy creates a deep copy of SyncProgress
func (in *SyncProgress) DeepCopy() *SyncProgress {
	if in == nil {
		return nil
	}
	out := new(SyncProgress)
	in.DeepCopyInto(out)
	return out
}

// ResourceGroupStatus provides status information for a group of resources
type ResourceGroupStatus struct {
	// GroupKind is the group/kind of the resources (e.g. "apps/Deployment")
	GroupKind string `json:"groupKind"`

	// TotalCount is the total number of resources in this group
	// +kubebuilder:validation:Minimum=0
	TotalCount int32 `json:"totalCount"`

	// SyncedCount is the number of successfully synced resources
	// +kubebuilder:validation:Minimum=0
	SyncedCount int32 `json:"syncedCount"`

	// FailedCount is the number of resources that failed to sync
	// +kubebuilder:validation:Minimum=0
	FailedCount int32 `json:"failedCount"`

	// PendingCount is the number of resources waiting to be synced
	// +kubebuilder:validation:Minimum=0
	PendingCount int32 `json:"pendingCount"`
}

// DeepCopyInto copies ResourceGroupStatus into out
func (in *ResourceGroupStatus) DeepCopyInto(out *ResourceGroupStatus) {
	*out = *in
}

// DeepCopy creates a deep copy of ResourceGroupStatus
func (in *ResourceGroupStatus) DeepCopy() *ResourceGroupStatus {
	if in == nil {
		return nil
	}
	out := new(ResourceGroupStatus)
	in.DeepCopyInto(out)
	return out
}

// DetailedResourceStatus provides detailed status for a specific resource
type DetailedResourceStatus struct {
	// Name of the resource
	Name string `json:"name"`

	// Version of the resource
	Version string `json:"version"`

	// SyncState represents the current state of sync (Pending, InProgress, Synced, Failed)
	// +kubebuilder:validation:Enum=Pending;InProgress;Synced;Failed
	SyncState string `json:"syncState"`

	// Dependencies tracks the status of resource dependencies
	// +optional
	Dependencies []ResourceDependency `json:"dependencies,omitempty"`

	// LastAttempt contains information about the last sync attempt
	// +optional
	LastAttempt *SyncAttempt `json:"lastAttempt,omitempty"`
}

// ResourceDependency tracks dependency information
type ResourceDependency struct {
	// Kind of the dependent resource
	Kind string `json:"kind"`

	// Name of the dependent resource
	Name string `json:"name"`

	// Status of the dependency
	// +kubebuilder:validation:Enum=Pending;InProgress;Synced;Failed
	Status string `json:"status"`
}

// SyncAttempt contains information about a sync attempt
type SyncAttempt struct {
	// Time of the attempt
	Time metav1.Time `json:"time"`

	// Result of the attempt
	// +kubebuilder:validation:Enum=Success;Failed;Skipped;Retrying
	Result string `json:"result"`
}

// DeepCopyInto copies DetailedResourceStatus into out
func (in *DetailedResourceStatus) DeepCopyInto(out *DetailedResourceStatus) {
	*out = *in
	if in.Dependencies != nil {
		in, out := &in.Dependencies, &out.Dependencies
		*out = make([]ResourceDependency, len(*in))
		copy(*out, *in)
	}
	if in.LastAttempt != nil {
		in, out := &in.LastAttempt, &out.LastAttempt
		*out = new(SyncAttempt)
		**out = **in
	}
}

// DeepCopy creates a deep copy of DetailedResourceStatus
func (in *DetailedResourceStatus) DeepCopy() *DetailedResourceStatus {
	if in == nil {
		return nil
	}
	out := new(DetailedResourceStatus)
	in.DeepCopyInto(out)
	return out
}

// ErrorCategory tracks errors by category
type ErrorCategory struct {
	// Category of the error
	// +kubebuilder:validation:MinLength=1
	Category string `json:"category"`

	// Count of errors in this category
	// +kubebuilder:validation:Minimum=0
	Count int32 `json:"count"`

	// LastOccurred is when the error last happened
	LastOccurred metav1.Time `json:"lastOccurred"`
}

// RetryStatus tracks retry information
type RetryStatus struct {
	// NextRetryTime is when the next retry will occur
	// +optional
	NextRetryTime *metav1.Time `json:"nextRetryTime,omitempty"`

	// RetriesRemaining is the number of retries left
	// +kubebuilder:validation:Minimum=0
	RetriesRemaining int32 `json:"retriesRemaining"`

	// BackoffDuration is the current backoff duration
	// +kubebuilder:validation:Pattern=^([0-9]+h)?([0-9]+m)?([0-9]+s)?$
	BackoffDuration string `json:"backoffDuration"`
}

// DeepCopyInto copies RetryStatus into out
func (in *RetryStatus) DeepCopyInto(out *RetryStatus) {
	*out = *in
	if in.NextRetryTime != nil {
		in, out := &in.NextRetryTime, &out.NextRetryTime
		*out = (*in).DeepCopy()
	}
}

// DeepCopy creates a deep copy of RetryStatus
func (in *RetryStatus) DeepCopy() *RetryStatus {
	if in == nil {
		return nil
	}
	out := new(RetryStatus)
	in.DeepCopyInto(out)
	return out
}

// SyncStats provides statistics about the sync operation
type SyncStats struct {
	// TotalResources is the total number of resources processed
	// +kubebuilder:validation:Minimum=0
	TotalResources int32 `json:"totalResources"`

	// SuccessfulSyncs is the number of resources successfully synced
	// +kubebuilder:validation:Minimum=0
	SuccessfulSyncs int32 `json:"successfulSyncs"`

	// FailedSyncs is the number of resources that failed to sync
	// +kubebuilder:validation:Minimum=0
	FailedSyncs int32 `json:"failedSyncs"`

	// LastSyncDuration is the duration of the last sync operation
	// +kubebuilder:validation:Pattern=^([0-9]+h)?([0-9]+m)?([0-9]+s)?$
	LastSyncDuration string `json:"lastSyncDuration"`
}

// DeepCopyInto copies SyncStats into out
func (in *SyncStats) DeepCopyInto(out *SyncStats) {
	*out = *in
}

// DeepCopy creates a deep copy of SyncStats
func (in *SyncStats) DeepCopy() *SyncStats {
	if in == nil {
		return nil
	}
	out := new(SyncStats)
	in.DeepCopyInto(out)
	return out
}

// ResourceStatus tracks the sync status of individual resources
type ResourceStatus struct {
	// Kind of the resource
	Kind string `json:"kind"`

	// Name of the resource
	Name string `json:"name"`

	// Namespace of the resource
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Status of the sync operation
	// +kubebuilder:validation:Enum=Pending;InProgress;Synced;Failed
	Status string `json:"status"`

	// LastSyncTime is the time of last sync attempt
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// Error message if sync failed
	// +optional
	Error string `json:"error,omitempty"`
}

// DeepCopyInto copies ResourceStatus into out
func (in *ResourceStatus) DeepCopyInto(out *ResourceStatus) {
	*out = *in
	if in.LastSyncTime != nil {
		in, out := &in.LastSyncTime, &out.LastSyncTime
		*out = (*in).DeepCopy()
	}
}

// DeepCopy creates a deep copy of ResourceStatus
func (in *ResourceStatus) DeepCopy() *ResourceStatus {
	if in == nil {
		return nil
	}
	out := new(ResourceStatus)
	in.DeepCopyInto(out)
	return out
}

// SyncError contains details about a sync error
type SyncError struct {
	// Message is the error message
	Message string `json:"message"`

	// Time when the error occurred
	Time metav1.Time `json:"time"`

	// Resource that caused the error (if applicable)
	// +optional
	Resource string `json:"resource,omitempty"`
}

// DeepCopyInto copies SyncError into out
func (in *SyncError) DeepCopyInto(out *SyncError) {
	*out = *in
}

// DeepCopy creates a deep copy of SyncError
func (in *SyncError) DeepCopy() *SyncError {
	if in == nil {
		return nil
	}
	out := new(SyncError)
	in.DeepCopyInto(out)
	return out
}

// DeploymentScale stores information about a deployment's scale
type DeploymentScale struct {
	// Name is the name of the deployment
	Name string `json:"name"`

	// OriginalReplicas is the number of replicas in the source cluster
	OriginalReplicas int32 `json:"originalReplicas"`

	// LastSyncedAt is when the scale was last synced
	LastSyncedAt *metav1.Time `json:"lastSyncedAt,omitempty"`
}

// DeepCopyInto copies DeploymentScale into out
func (in *DeploymentScale) DeepCopyInto(out *DeploymentScale) {
	*out = *in
	if in.LastSyncedAt != nil {
		in, out := &in.LastSyncedAt, &out.LastSyncedAt
		*out = (*in).DeepCopy()
	}
}

// DeepCopy creates a deep copy of DeploymentScale
func (in *DeploymentScale) DeepCopy() *DeploymentScale {
	if in == nil {
		return nil
	}
	out := new(DeploymentScale)
	in.DeepCopyInto(out)
	return out
}
