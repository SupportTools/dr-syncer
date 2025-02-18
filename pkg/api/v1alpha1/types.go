package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Source",type="string",JSONPath=".spec.sourceNamespace"
// +kubebuilder:printcolumn:name="Destination",type="string",JSONPath=".spec.destinationNamespace"
// +kubebuilder:printcolumn:name="Source Cluster",type="string",JSONPath=".spec.sourceCluster"
// +kubebuilder:printcolumn:name="Destination Cluster",type="string",JSONPath=".spec.destinationCluster"
// +kubebuilder:printcolumn:name="Schedule",type="string",JSONPath=".spec.schedule"
// +kubebuilder:printcolumn:name="Last Sync",type="date",JSONPath=".status.lastSyncTime"
// +kubebuilder:printcolumn:name="Next Sync",type="date",JSONPath=".status.nextSyncTime"
type Replication struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ReplicationSpec   `json:"spec"`
	Status ReplicationStatus `json:"status,omitempty"`
}

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

type ReplicationSpec struct {
	// SourceCluster is the name of the source cluster
	SourceCluster string `json:"sourceCluster"`

	// DestinationCluster is the name of the destination cluster
	DestinationCluster string `json:"destinationCluster"`

	// SourceNamespace is the namespace to replicate from
	SourceNamespace string `json:"sourceNamespace"`

	// DestinationNamespace is the namespace to replicate to
	DestinationNamespace string `json:"destinationNamespace"`

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
}

// DeepCopyInto copies ReplicationSpec into out
func (in *ReplicationSpec) DeepCopyInto(out *ReplicationSpec) {
	*out = *in
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
}

// DeepCopy creates a deep copy of ReplicationSpec
func (in *ReplicationSpec) DeepCopy() *ReplicationSpec {
	if in == nil {
		return nil
	}
	out := new(ReplicationSpec)
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

type ReplicationStatus struct {
	// LastSyncTime is the last time the replication was synced
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// NextSyncTime is the next scheduled sync time
	// +optional
	NextSyncTime *metav1.Time `json:"nextSyncTime,omitempty"`

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

// +kubebuilder:object:root=true
type ReplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Replication `json:"items"`
}

// DeepCopyObject implements runtime.Object interface
func (r *Replication) DeepCopyObject() runtime.Object {
	if c := r.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopy creates a deep copy of Replication
func (r *Replication) DeepCopy() *Replication {
	if r == nil {
		return nil
	}
	out := new(Replication)
	r.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all properties of Replication into another instance
func (r *Replication) DeepCopyInto(out *Replication) {
	*out = *r
	out.TypeMeta = r.TypeMeta
	r.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	r.Spec.DeepCopyInto(&out.Spec)
	r.Status.DeepCopyInto(&out.Status)
}

// DeepCopyObject implements runtime.Object interface
func (r *ReplicationList) DeepCopyObject() runtime.Object {
	if c := r.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopy creates a deep copy of ReplicationList
func (r *ReplicationList) DeepCopy() *ReplicationList {
	if r == nil {
		return nil
	}
	out := new(ReplicationList)
	r.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all properties of ReplicationList into another instance
func (r *ReplicationList) DeepCopyInto(out *ReplicationList) {
	*out = *r
	out.TypeMeta = r.TypeMeta
	out.ListMeta = r.ListMeta
	if r.Items != nil {
		out.Items = make([]Replication, len(r.Items))
		for i := range r.Items {
			r.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}
