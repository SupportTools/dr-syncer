/*
Copyright 2024 Support Tools.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// PVCSyncOperationPhase represents the current phase of a PVC sync operation
// +kubebuilder:validation:Enum=Pending;Initializing;Syncing;Verifying;Completed;Failed
type PVCSyncOperationPhase string

const (
	// PVCSyncOperationPhasePending indicates the sync is waiting to start
	PVCSyncOperationPhasePending PVCSyncOperationPhase = "Pending"
	// PVCSyncOperationPhaseInitializing indicates the sync is setting up (SSH, pods, etc.)
	PVCSyncOperationPhaseInitializing PVCSyncOperationPhase = "Initializing"
	// PVCSyncOperationPhaseSyncing indicates rsync is actively transferring data
	PVCSyncOperationPhaseSyncing PVCSyncOperationPhase = "Syncing"
	// PVCSyncOperationPhaseVerifying indicates post-sync verification is in progress
	PVCSyncOperationPhaseVerifying PVCSyncOperationPhase = "Verifying"
	// PVCSyncOperationPhaseCompleted indicates the sync finished successfully
	PVCSyncOperationPhaseCompleted PVCSyncOperationPhase = "Completed"
	// PVCSyncOperationPhaseFailed indicates the sync failed
	PVCSyncOperationPhaseFailed PVCSyncOperationPhase = "Failed"
)

// PVCSyncOperationSpec defines the specification for a PVC sync operation
type PVCSyncOperationSpec struct {
	// SourceNamespace is the namespace of the source PVC
	SourceNamespace string `json:"sourceNamespace"`

	// PVCName is the name of the PVC being synced
	PVCName string `json:"pvcName"`

	// DestinationNamespace is the target namespace for the sync
	DestinationNamespace string `json:"destinationNamespace"`

	// DestinationPVCName is the name of the destination PVC (if different from source)
	// +optional
	DestinationPVCName string `json:"destinationPVCName,omitempty"`

	// NamespaceMappingRef references the parent NamespaceMapping that triggered this sync
	// +optional
	NamespaceMappingRef *PVCSyncNamespaceMappingRef `json:"namespaceMappingRef,omitempty"`
}

// PVCSyncNamespaceMappingRef is a reference to a NamespaceMapping resource
type PVCSyncNamespaceMappingRef struct {
	// Name is the name of the NamespaceMapping
	Name string `json:"name"`

	// Namespace is the namespace of the NamespaceMapping
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// PVCSyncVerificationResult contains the results of post-sync verification
type PVCSyncVerificationResult struct {
	// Mode is the verification mode used (none, sample, full)
	// +optional
	Mode string `json:"mode,omitempty"`

	// FilesVerified is the number of files that were verified
	// +optional
	FilesVerified int `json:"filesVerified,omitempty"`

	// FilesTotal is the total number of files checked
	// +optional
	FilesTotal int `json:"filesTotal,omitempty"`

	// ChecksumMatch indicates whether all checksums matched
	// +optional
	ChecksumMatch bool `json:"checksumMatch,omitempty"`

	// VerifiedAt is when the verification was completed
	// +optional
	VerifiedAt *metav1.Time `json:"verifiedAt,omitempty"`

	// Error contains any verification error message
	// +optional
	Error string `json:"error,omitempty"`
}

// PVCSyncHistoryEntry records a past sync attempt
type PVCSyncHistoryEntry struct {
	// StartTime is when the sync attempt started
	StartTime metav1.Time `json:"startTime"`

	// CompletionTime is when the sync attempt finished
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Phase is the final phase of the sync attempt
	Phase PVCSyncOperationPhase `json:"phase"`

	// BytesTransferred is the total bytes synced
	// +optional
	BytesTransferred int64 `json:"bytesTransferred,omitempty"`

	// FilesTransferred is the total files synced
	// +optional
	FilesTransferred int `json:"filesTransferred,omitempty"`

	// Duration is the human-readable duration (e.g., "5m30s")
	// +optional
	Duration string `json:"duration,omitempty"`

	// Error contains any error message for failed syncs
	// +optional
	Error string `json:"error,omitempty"`
}

// SnapshotSyncInfo tracks snapshot-based sync metadata
type SnapshotSyncInfo struct {
	// SnapshotName is the VolumeSnapshot used for this sync
	// +optional
	SnapshotName string `json:"snapshotName,omitempty"`

	// SnapshotReadyTime when snapshot became ready
	// +optional
	SnapshotReadyTime *metav1.Time `json:"snapshotReadyTime,omitempty"`

	// RestorePVCName is the temporary PVC created from snapshot
	// +optional
	RestorePVCName string `json:"restorePVCName,omitempty"`

	// SnapshotSize in bytes (if reported by CSI driver)
	// +optional
	SnapshotSize *int64 `json:"snapshotSize,omitempty"`

	// UsedLiveFallback indicates snapshot failed and live sync was used
	// +optional
	UsedLiveFallback bool `json:"usedLiveFallback,omitempty"`
}

// PVCSyncOperationStatus defines the observed state of a PVC sync operation
type PVCSyncOperationStatus struct {
	// Phase is the current phase of the sync operation
	// +optional
	Phase PVCSyncOperationPhase `json:"phase,omitempty"`

	// Progress is the sync progress as a percentage string (e.g., "75%")
	// +optional
	Progress string `json:"progress,omitempty"`

	// Duration is the human-readable duration of the current/last sync (e.g., "5m30s")
	// +optional
	Duration string `json:"duration,omitempty"`

	// BytesTransferred is the total bytes transferred
	// +optional
	BytesTransferred int64 `json:"bytesTransferred,omitempty"`

	// FilesTransferred is the total files transferred
	// +optional
	FilesTransferred int `json:"filesTransferred,omitempty"`

	// TotalBytes is the expected total bytes (if known)
	// +optional
	TotalBytes int64 `json:"totalBytes,omitempty"`

	// TotalFiles is the expected total files (if known)
	// +optional
	TotalFiles int `json:"totalFiles,omitempty"`

	// Speed is the current transfer rate as a human-readable string (e.g., "1.5 MB/s")
	// +optional
	Speed string `json:"speed,omitempty"`

	// EstimatedRemaining is the estimated time remaining (e.g., "2m15s")
	// +optional
	EstimatedRemaining string `json:"estimatedRemaining,omitempty"`

	// StartTime is when the current sync started
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// CompletionTime is when the sync finished
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Error contains the error message if the sync failed
	// +optional
	Error string `json:"error,omitempty"`

	// Verification contains the results of post-sync verification
	// +optional
	Verification *PVCSyncVerificationResult `json:"verification,omitempty"`

	// SnapshotInfo contains snapshot-based sync details
	// +optional
	SnapshotInfo *SnapshotSyncInfo `json:"snapshotInfo,omitempty"`

	// History contains the last few sync attempts (max 5)
	// +optional
	History []PVCSyncHistoryEntry `json:"history,omitempty"`

	// Conditions represent the latest available observations of the PVCSync's state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ps
// +kubebuilder:printcolumn:name="Source NS",type="string",JSONPath=".spec.sourceNamespace"
// +kubebuilder:printcolumn:name="PVC",type="string",JSONPath=".spec.pvcName"
// +kubebuilder:printcolumn:name="Dest NS",type="string",JSONPath=".spec.destinationNamespace"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Progress",type="string",JSONPath=".status.progress"
// +kubebuilder:printcolumn:name="Duration",type="string",JSONPath=".status.duration"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// PVCSyncOperation represents a PVC synchronization operation between source and destination clusters.
// It provides first-class visibility into PVC replication status via kubectl.
type PVCSyncOperation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PVCSyncOperationSpec   `json:"spec"`
	Status PVCSyncOperationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PVCSyncOperationList contains a list of PVCSyncOperation resources
type PVCSyncOperationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PVCSyncOperation `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PVCSyncOperation{}, &PVCSyncOperationList{})
}

// DeepCopyInto copies all properties of this object into another object of the same type
func (in *PVCSyncOperationSpec) DeepCopyInto(out *PVCSyncOperationSpec) {
	*out = *in
	if in.NamespaceMappingRef != nil {
		in, out := &in.NamespaceMappingRef, &out.NamespaceMappingRef
		*out = new(PVCSyncNamespaceMappingRef)
		**out = **in
	}
}

// DeepCopy creates a deep copy of PVCSyncOperationSpec
func (in *PVCSyncOperationSpec) DeepCopy() *PVCSyncOperationSpec {
	if in == nil {
		return nil
	}
	out := new(PVCSyncOperationSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all properties of this object into another object of the same type
func (in *PVCSyncNamespaceMappingRef) DeepCopyInto(out *PVCSyncNamespaceMappingRef) {
	*out = *in
}

// DeepCopy creates a deep copy of PVCSyncNamespaceMappingRef
func (in *PVCSyncNamespaceMappingRef) DeepCopy() *PVCSyncNamespaceMappingRef {
	if in == nil {
		return nil
	}
	out := new(PVCSyncNamespaceMappingRef)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all properties of this object into another object of the same type
func (in *PVCSyncVerificationResult) DeepCopyInto(out *PVCSyncVerificationResult) {
	*out = *in
	if in.VerifiedAt != nil {
		in, out := &in.VerifiedAt, &out.VerifiedAt
		*out = (*in).DeepCopy()
	}
}

// DeepCopy creates a deep copy of PVCSyncVerificationResult
func (in *PVCSyncVerificationResult) DeepCopy() *PVCSyncVerificationResult {
	if in == nil {
		return nil
	}
	out := new(PVCSyncVerificationResult)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all properties of this object into another object of the same type
func (in *PVCSyncHistoryEntry) DeepCopyInto(out *PVCSyncHistoryEntry) {
	*out = *in
	in.StartTime.DeepCopyInto(&out.StartTime)
	if in.CompletionTime != nil {
		in, out := &in.CompletionTime, &out.CompletionTime
		*out = (*in).DeepCopy()
	}
}

// DeepCopy creates a deep copy of PVCSyncHistoryEntry
func (in *PVCSyncHistoryEntry) DeepCopy() *PVCSyncHistoryEntry {
	if in == nil {
		return nil
	}
	out := new(PVCSyncHistoryEntry)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all properties of this object into another object of the same type
func (in *SnapshotSyncInfo) DeepCopyInto(out *SnapshotSyncInfo) {
	*out = *in
	if in.SnapshotReadyTime != nil {
		in, out := &in.SnapshotReadyTime, &out.SnapshotReadyTime
		*out = (*in).DeepCopy()
	}
	if in.SnapshotSize != nil {
		in, out := &in.SnapshotSize, &out.SnapshotSize
		*out = new(int64)
		**out = **in
	}
}

// DeepCopy creates a deep copy of SnapshotSyncInfo
func (in *SnapshotSyncInfo) DeepCopy() *SnapshotSyncInfo {
	if in == nil {
		return nil
	}
	out := new(SnapshotSyncInfo)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all properties of this object into another object of the same type
func (in *PVCSyncOperationStatus) DeepCopyInto(out *PVCSyncOperationStatus) {
	*out = *in
	if in.StartTime != nil {
		in, out := &in.StartTime, &out.StartTime
		*out = (*in).DeepCopy()
	}
	if in.CompletionTime != nil {
		in, out := &in.CompletionTime, &out.CompletionTime
		*out = (*in).DeepCopy()
	}
	if in.Verification != nil {
		in, out := &in.Verification, &out.Verification
		*out = new(PVCSyncVerificationResult)
		(*in).DeepCopyInto(*out)
	}
	if in.SnapshotInfo != nil {
		in, out := &in.SnapshotInfo, &out.SnapshotInfo
		*out = new(SnapshotSyncInfo)
		(*in).DeepCopyInto(*out)
	}
	if in.History != nil {
		in, out := &in.History, &out.History
		*out = make([]PVCSyncHistoryEntry, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]metav1.Condition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy creates a deep copy of PVCSyncOperationStatus
func (in *PVCSyncOperationStatus) DeepCopy() *PVCSyncOperationStatus {
	if in == nil {
		return nil
	}
	out := new(PVCSyncOperationStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all properties of this object into another object of the same type
func (in *PVCSyncOperation) DeepCopyInto(out *PVCSyncOperation) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy creates a deep copy of PVCSyncOperation
func (in *PVCSyncOperation) DeepCopy() *PVCSyncOperation {
	if in == nil {
		return nil
	}
	out := new(PVCSyncOperation)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject returns a generically typed copy of an object
func (in *PVCSyncOperation) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto copies all properties of this object into another object of the same type
func (in *PVCSyncOperationList) DeepCopyInto(out *PVCSyncOperationList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]PVCSyncOperation, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy creates a deep copy of PVCSyncOperationList
func (in *PVCSyncOperationList) DeepCopy() *PVCSyncOperationList {
	if in == nil {
		return nil
	}
	out := new(PVCSyncOperationList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject returns a generically typed copy of an object
func (in *PVCSyncOperationList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}
