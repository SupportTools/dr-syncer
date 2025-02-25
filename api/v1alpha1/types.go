package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

[Previous content remains the same until ErrorCategory...]

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

// DeepCopyInto copies ErrorCategory into out
func (in *ErrorCategory) DeepCopyInto(out *ErrorCategory) {
	*out = *in
}

// DeepCopy creates a deep copy of ErrorCategory
func (in *ErrorCategory) DeepCopy() *ErrorCategory {
	if in == nil {
		return nil
	}
	out := new(ErrorCategory)
	in.DeepCopyInto(out)
	return out
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
	LastSyncDuration string `json:"lastSyncDuration,omitempty"`
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

// ReplicationStatus defines the observed state of Replication
type ReplicationStatus struct {
	// Phase represents the current phase of the replication
	// +optional
	Phase SyncPhase `json:"phase,omitempty"`

	// LastSyncTime is the last time the replication was synced
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// NextSyncTime is the next scheduled sync time (Scheduled mode only)
	// +optional
	NextSyncTime *metav1.Time `json:"nextSyncTime,omitempty"`

	// LastWatchEvent is the last time a watch event was processed (Continuous mode only)
	// +optional
	LastWatchEvent *metav1.Time `json:"lastWatchEvent,omitempty"`

	// SyncProgress tracks the current progress of the sync operation
	// +optional
	SyncProgress *SyncProgress `json:"syncProgress,omitempty"`

	// SyncStats provides statistics about the last sync operation
	// +optional
	SyncStats *SyncStats `json:"syncStats,omitempty"`

	// ResourceGroups provides status information grouped by resource type
	// +optional
	ResourceGroups []ResourceGroupStatus `json:"resourceGroups,omitempty"`

	// DetailedStatus provides detailed status for specific resources
	// +optional
	DetailedStatus []DetailedResourceStatus `json:"detailedStatus,omitempty"`

	// ErrorCategories tracks errors by category
	// +optional
	ErrorCategories []ErrorCategory `json:"errorCategories,omitempty"`

	// RetryStatus tracks retry information for failed operations
	// +optional
	RetryStatus *RetryStatus `json:"retryStatus,omitempty"`

	// ResourceStatus tracks the sync status of individual resources
	// +optional
	ResourceStatus []ResourceStatus `json:"resourceStatus,omitempty"`

	// LastError contains details about the last error encountered
	// +optional
	LastError *SyncError `json:"lastError,omitempty"`

	// Conditions represent the latest available observations of the replication's state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// DeploymentScales stores the original scale values of deployments
	// +optional
	DeploymentScales []DeploymentScale `json:"deploymentScales,omitempty"`
}

// DeepCopyInto copies ReplicationStatus into out
func (in *ReplicationStatus) DeepCopyInto(out *ReplicationStatus) {
	*out = *in
	if in.LastSyncTime != nil {
		in, out := &in.LastSyncTime, &out.LastSyncTime
		*out = (*in).DeepCopy()
	}
	if in.NextSyncTime != nil {
		in, out := &in.NextSyncTime, &out.NextSyncTime
		*out = (*in).DeepCopy()
	}
	if in.LastWatchEvent != nil {
		in, out := &in.LastWatchEvent, &out.LastWatchEvent
		*out = (*in).DeepCopy()
	}
	if in.SyncProgress != nil {
		in, out := &in.SyncProgress, &out.SyncProgress
		*out = new(SyncProgress)
		(*in).DeepCopyInto(*out)
	}
	if in.SyncStats != nil {
		in, out := &in.SyncStats, &out.SyncStats
		*out = new(SyncStats)
		(*in).DeepCopyInto(*out)
	}
	if in.ResourceGroups != nil {
		in, out := &in.ResourceGroups, &out.ResourceGroups
		*out = make([]ResourceGroupStatus, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.DetailedStatus != nil {
		in, out := &in.DetailedStatus, &out.DetailedStatus
		*out = make([]DetailedResourceStatus, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.ErrorCategories != nil {
		in, out := &in.ErrorCategories, &out.ErrorCategories
		*out = make([]ErrorCategory, len(*in))
		copy(*out, *in)
	}
	if in.RetryStatus != nil {
		in, out := &in.RetryStatus, &out.RetryStatus
		*out = new(RetryStatus)
		(*in).DeepCopyInto(*out)
	}
	if in.ResourceStatus != nil {
		in, out := &in.ResourceStatus, &out.ResourceStatus
		*out = make([]ResourceStatus, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.LastError != nil {
		in, out := &in.LastError, &out.LastError
		*out = new(SyncError)
		(*in).DeepCopyInto(*out)
	}
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]metav1.Condition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.DeploymentScales != nil {
		in, out := &in.DeploymentScales, &out.DeploymentScales
		*out = make([]DeploymentScale, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy creates a deep copy of ReplicationStatus
func (in *ReplicationStatus) DeepCopy() *ReplicationStatus {
	if in == nil {
		return nil
	}
	out := new(ReplicationStatus)
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
