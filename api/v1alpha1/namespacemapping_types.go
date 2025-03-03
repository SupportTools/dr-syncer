package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Selection Mode",type="string",JSONPath=".spec.namespaceSelector",priority=1
// +kubebuilder:printcolumn:name="Source",type="string",JSONPath=".spec.sourceNamespace"
// +kubebuilder:printcolumn:name="Destination",type="string",JSONPath=".spec.destinationNamespace"
// +kubebuilder:printcolumn:name="Cluster Mapping",type="string",JSONPath=".spec.clusterMappingRef.name"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Last Sync",type="string",JSONPath=".status.lastSyncTime"
// +kubebuilder:printcolumn:name="Next Sync",type="string",JSONPath=".status.nextSyncTime"
type NamespaceMapping struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NamespaceMappingSpec   `json:"spec"`
	Status NamespaceMappingStatus `json:"status,omitempty"`
}

type NamespaceMappingSpec struct {
	// Paused defines whether replication is paused
	// When set to true, all replication operations will be skipped
	// +optional
	// +kubebuilder:default=false
	Paused *bool `json:"paused,omitempty"`

	// ReplicationMode defines how replication should be performed
	// +kubebuilder:validation:Enum=Scheduled;Continuous;Manual
	// +kubebuilder:default=Scheduled
	ReplicationMode ReplicationMode `json:"replicationMode,omitempty"`

	// NamespaceConfig defines configuration for namespace handling
	// +optional
	NamespaceConfig *NamespaceConfig `json:"namespaceConfig,omitempty"`

	// Continuous configuration for continuous replication mode
	// +optional
	Continuous *ContinuousConfig `json:"continuous,omitempty"`

	// RetryConfig defines retry behavior for failed operations
	// +optional
	RetryConfig *RetryConfig `json:"retryConfig,omitempty"`

	// TempPodKeySecretRef is a reference to the secret containing SSH keys for temporary pods
	// +optional
	TempPodKeySecretRef *SecretReference `json:"tempPodKeySecretRef,omitempty"`

	// SourceCluster is the name of the source cluster
	// +optional
	SourceCluster string `json:"sourceCluster,omitempty"`

	// DestinationCluster is the name of the destination cluster
	// +optional
	DestinationCluster string `json:"destinationCluster,omitempty"`

	// SourceNamespace is the namespace to replicate from (direct mapping mode)
	// +optional
	SourceNamespace string `json:"sourceNamespace,omitempty"`

	// DestinationNamespace is the namespace to replicate to (direct mapping mode)
	// +optional
	DestinationNamespace string `json:"destinationNamespace,omitempty"`

	// Schedule is the crontab schedule for replication
	// +optional
	// +kubebuilder:validation:Pattern=^(\*|([0-9]|1[0-9]|2[0-9]|3[0-9]|4[0-9]|5[0-9])|\*/[0-9]+|\*\/[1-5][0-9])\s+(\*|([0-9]|1[0-9]|2[0-3])|\*/[0-9]+)\s+(\*|([1-9]|1[0-9]|2[0-9]|3[0-1])|\*/[0-9]+)\s+(\*|([1-9]|1[0-2])|\*/[0-9]+)\s+(\*|([0-6])|\*/[0-9]+)$
	Schedule string `json:"schedule,omitempty"`

	// ResourceTypes is the list of resource types to replicate
	// +optional
	ResourceTypes []string `json:"resourceTypes,omitempty"`

	// ScaleToZero determines whether deployments should be scaled to zero replicas in the destination cluster
	// +optional
	// +kubebuilder:default=true
	ScaleToZero *bool `json:"scaleToZero,omitempty"`

	// NamespaceScopedResources is a list of namespace scoped resources to replicate
	// Format: "resource.group" (e.g. "widgets.example.com")
	// +optional
	NamespaceScopedResources []string `json:"namespaceScopedResources,omitempty"`

	// PVCConfig defines configuration for PVC replication
	// +optional
	PVCConfig *PVCConfig `json:"pvcConfig,omitempty"`

	// ImmutableResourceConfig defines how to handle immutable resources
	// +optional
	ImmutableResourceConfig *ImmutableResourceConfig `json:"immutableResourceConfig,omitempty"`

	// SyncCRDs determines whether to sync Custom Resource Definitions
	// When true, CRDs will be synced along with other resources
	// When false (default), CRDs will be skipped
	// +optional
	// +kubebuilder:default=false
	SyncCRDs *bool `json:"syncCRDs,omitempty"`

	// FailureHandling defines how different types of failures are handled
	// +optional
	FailureHandling *FailureHandlingConfig `json:"failureHandling,omitempty"`

	// IngressConfig defines configuration for ingress replication
	// +optional
	IngressConfig *IngressConfig `json:"ingressConfig,omitempty"`

	// ClusterMappingRef references a ClusterMapping resource for cluster connectivity
	// This is the preferred way to specify source and target clusters
	// +optional
	ClusterMappingRef *ClusterMappingReference `json:"clusterMappingRef,omitempty"`
}

// DeepCopyInto copies NamespaceMappingSpec into out
func (in *NamespaceMappingSpec) DeepCopyInto(out *NamespaceMappingSpec) {
	*out = *in
	if in.NamespaceConfig != nil {
		in, out := &in.NamespaceConfig, &out.NamespaceConfig
		*out = new(NamespaceConfig)
		**out = **in
	}
	if in.Continuous != nil {
		in, out := &in.Continuous, &out.Continuous
		*out = new(ContinuousConfig)
		**out = **in
	}
	if in.RetryConfig != nil {
		in, out := &in.RetryConfig, &out.RetryConfig
		*out = new(RetryConfig)
		(*in).DeepCopyInto(*out)
	}
	if in.ResourceTypes != nil {
		in, out := &in.ResourceTypes, &out.ResourceTypes
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.ScaleToZero != nil {
		in, out := &in.ScaleToZero, &out.ScaleToZero
		*out = new(bool)
		**out = **in
	}
	if in.NamespaceScopedResources != nil {
		in, out := &in.NamespaceScopedResources, &out.NamespaceScopedResources
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.PVCConfig != nil {
		in, out := &in.PVCConfig, &out.PVCConfig
		*out = new(PVCConfig)
		(*in).DeepCopyInto(*out)
	}
	if in.ImmutableResourceConfig != nil {
		in, out := &in.ImmutableResourceConfig, &out.ImmutableResourceConfig
		*out = new(ImmutableResourceConfig)
		(*in).DeepCopyInto(*out)
	}
	if in.SyncCRDs != nil {
		in, out := &in.SyncCRDs, &out.SyncCRDs
		*out = new(bool)
		**out = **in
	}
	if in.FailureHandling != nil {
		in, out := &in.FailureHandling, &out.FailureHandling
		*out = new(FailureHandlingConfig)
		(*in).DeepCopyInto(*out)
	}
	if in.IngressConfig != nil {
		in, out := &in.IngressConfig, &out.IngressConfig
		*out = new(IngressConfig)
		(*in).DeepCopyInto(*out)
	}
	if in.ClusterMappingRef != nil {
		in, out := &in.ClusterMappingRef, &out.ClusterMappingRef
		*out = new(ClusterMappingReference)
		**out = **in
	}
	if in.TempPodKeySecretRef != nil {
		in, out := &in.TempPodKeySecretRef, &out.TempPodKeySecretRef
		*out = new(SecretReference)
		**out = **in
	}
}

// DeepCopy creates a deep copy of NamespaceMappingSpec
func (in *NamespaceMappingSpec) DeepCopy() *NamespaceMappingSpec {
	if in == nil {
		return nil
	}
	out := new(NamespaceMappingSpec)
	in.DeepCopyInto(out)
	return out
}

type NamespaceMappingStatus struct {
	// Phase represents the current phase of the namespace mapping
	// +optional
	Phase SyncPhase `json:"phase,omitempty"`

	// LastSyncTime is the last time the namespace mapping was synced
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

	// Conditions represent the latest available observations of the namespace mapping's state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// DeploymentScales stores the original scale values of deployments
	// +optional
	DeploymentScales []DeploymentScale `json:"deploymentScales,omitempty"`
}

// DeepCopyInto copies NamespaceMappingStatus into out
func (in *NamespaceMappingStatus) DeepCopyInto(out *NamespaceMappingStatus) {
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
		**out = **in
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
		**out = **in
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

// DeepCopy creates a deep copy of NamespaceMappingStatus
func (in *NamespaceMappingStatus) DeepCopy() *NamespaceMappingStatus {
	if in == nil {
		return nil
	}
	out := new(NamespaceMappingStatus)
	in.DeepCopyInto(out)
	return out
}

// +kubebuilder:object:root=true
type NamespaceMappingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NamespaceMapping `json:"items"`
}

// DeepCopyObject implements runtime.Object interface
func (n *NamespaceMapping) DeepCopyObject() runtime.Object {
	if c := n.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopy creates a deep copy of NamespaceMapping
func (n *NamespaceMapping) DeepCopy() *NamespaceMapping {
	if n == nil {
		return nil
	}
	out := new(NamespaceMapping)
	n.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all properties of NamespaceMapping into another instance
func (n *NamespaceMapping) DeepCopyInto(out *NamespaceMapping) {
	*out = *n
	out.TypeMeta = n.TypeMeta
	n.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	n.Spec.DeepCopyInto(&out.Spec)
	n.Status.DeepCopyInto(&out.Status)
}

// DeepCopyObject implements runtime.Object interface
func (n *NamespaceMappingList) DeepCopyObject() runtime.Object {
	if c := n.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopy creates a deep copy of NamespaceMappingList
func (n *NamespaceMappingList) DeepCopy() *NamespaceMappingList {
	if n == nil {
		return nil
	}
	out := new(NamespaceMappingList)
	n.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all properties of NamespaceMappingList into another instance
func (n *NamespaceMappingList) DeepCopyInto(out *NamespaceMappingList) {
	*out = *n
	out.TypeMeta = n.TypeMeta
	out.ListMeta = n.ListMeta
	if n.Items != nil {
		out.Items = make([]NamespaceMapping, len(n.Items))
		for i := range n.Items {
			n.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

func init() {
	SchemeBuilder.Register(&NamespaceMapping{}, &NamespaceMappingList{})
}
