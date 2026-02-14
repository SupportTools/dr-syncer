package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// OperationType defines the type of backup operation
// +kubebuilder:validation:Enum=Backup;Restore
type OperationType string

const (
	// OperationTypeBackup indicates a backup operation (source PVC → S3)
	OperationTypeBackup OperationType = "Backup"
	// OperationTypeRestore indicates a restore operation (S3 → standby PVC)
	OperationTypeRestore OperationType = "Restore"
)

// DataAccessStrategy defines how the backup workflow accesses PVC data
// +kubebuilder:validation:Enum=Auto;Snapshot;Live
type DataAccessStrategy string

const (
	// DataAccessStrategyAuto auto-detects CSI snapshot support; falls back to live
	DataAccessStrategyAuto DataAccessStrategy = "Auto"
	// DataAccessStrategySnapshot uses CSI VolumeSnapshot for point-in-time consistency
	DataAccessStrategySnapshot DataAccessStrategy = "Snapshot"
	// DataAccessStrategyLive reads directly from the live PVC mount
	DataAccessStrategyLive DataAccessStrategy = "Live"
)

// VolumeBackupOperationPhase represents the current phase of a backup/restore operation
// +kubebuilder:validation:Enum=Pending;SnapshotCreated;DataAccessReady;BackupInProgress;BackupComplete;RestoreInProgress;RestoreComplete;Failed
type VolumeBackupOperationPhase string

const (
	// VolumeBackupPhasePending indicates the operation is waiting to start
	VolumeBackupPhasePending VolumeBackupOperationPhase = "Pending"
	// VolumeBackupPhaseSnapshotCreated indicates a CSI snapshot has been created
	VolumeBackupPhaseSnapshotCreated VolumeBackupOperationPhase = "SnapshotCreated"
	// VolumeBackupPhaseDataAccessReady indicates the data source is ready for Kopia
	VolumeBackupPhaseDataAccessReady VolumeBackupOperationPhase = "DataAccessReady"
	// VolumeBackupPhaseBackupInProgress indicates Kopia backup is running
	VolumeBackupPhaseBackupInProgress VolumeBackupOperationPhase = "BackupInProgress"
	// VolumeBackupPhaseBackupComplete indicates Kopia backup completed successfully
	VolumeBackupPhaseBackupComplete VolumeBackupOperationPhase = "BackupComplete"
	// VolumeBackupPhaseRestoreInProgress indicates Kopia restore is running
	VolumeBackupPhaseRestoreInProgress VolumeBackupOperationPhase = "RestoreInProgress"
	// VolumeBackupPhaseRestoreComplete indicates Kopia restore completed successfully
	VolumeBackupPhaseRestoreComplete VolumeBackupOperationPhase = "RestoreComplete"
	// VolumeBackupPhaseFailed indicates the operation failed
	VolumeBackupPhaseFailed VolumeBackupOperationPhase = "Failed"
)

// PVCReference identifies a PVC by namespace and name
type PVCReference struct {
	// Namespace of the PVC
	// +kubebuilder:validation:MinLength=1
	Namespace string `json:"namespace"`

	// Name of the PVC
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// VolumeBackupOperationSpec defines the desired state of VolumeBackupOperation
type VolumeBackupOperationSpec struct {
	// OperationType specifies whether this is a Backup or Restore operation
	OperationType OperationType `json:"operationType"`

	// SourcePVC references the source PVC (backup reads from this)
	SourcePVC PVCReference `json:"sourcePVC"`

	// DestinationPVC references the destination PVC (restore writes to this)
	// +optional
	DestinationPVC *PVCReference `json:"destinationPVC,omitempty"`

	// BackupRepositoryRef references the BackupRepository to use
	BackupRepositoryRef SecretReference `json:"backupRepositoryRef"`

	// SnapshotID is the Kopia snapshot ID to restore from (required for Restore operations)
	// +optional
	SnapshotID string `json:"snapshotID,omitempty"`

	// DataAccessStrategy specifies how to access PVC data for backup
	// +optional
	// +kubebuilder:default="Auto"
	DataAccessStrategy DataAccessStrategy `json:"dataAccessStrategy,omitempty"`
}

// VolumeBackupOperationStatus defines the observed state of VolumeBackupOperation
type VolumeBackupOperationStatus struct {
	// Phase represents the current phase of the operation
	// +optional
	Phase VolumeBackupOperationPhase `json:"phase,omitempty"`

	// StartTime is when the operation started
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// CompletionTime is when the operation completed (success or failure)
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// BytesTransferred is the total bytes transferred during the operation
	// +optional
	BytesTransferred int64 `json:"bytesTransferred,omitempty"`

	// ProgressPercentage indicates completion progress (0-100)
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	ProgressPercentage int32 `json:"progressPercentage,omitempty"`

	// KopiaSnapshotID is the Kopia snapshot ID created by a backup operation
	// +optional
	KopiaSnapshotID string `json:"kopiaSnapshotID,omitempty"`

	// ErrorMessage contains error details if the operation failed
	// +optional
	ErrorMessage string `json:"errorMessage,omitempty"`

	// Message provides additional status information
	// +optional
	Message string `json:"message,omitempty"`

	// Conditions represent the latest available observations of the operation's state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Type",type="string",JSONPath=".spec.operationType",description="Operation type (Backup or Restore)"
// +kubebuilder:printcolumn:name="PVC",type="string",JSONPath=".spec.sourcePVC.name",description="Source PVC name"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Current phase of the operation"
// +kubebuilder:printcolumn:name="Progress",type="integer",JSONPath=".status.progressPercentage",description="Completion progress percentage"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:shortName=vbo

// VolumeBackupOperation tracks an individual backup or restore operation
// for a PVC using the Kopia backup path.
type VolumeBackupOperation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VolumeBackupOperationSpec   `json:"spec"`
	Status VolumeBackupOperationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VolumeBackupOperationList contains a list of VolumeBackupOperation resources
type VolumeBackupOperationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VolumeBackupOperation `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VolumeBackupOperation{}, &VolumeBackupOperationList{})
}

// --- DeepCopy methods ---

// DeepCopyInto copies PVCReference into out
func (in *PVCReference) DeepCopyInto(out *PVCReference) {
	*out = *in
}

// DeepCopy creates a deep copy of PVCReference
func (in *PVCReference) DeepCopy() *PVCReference {
	if in == nil {
		return nil
	}
	out := new(PVCReference)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies VolumeBackupOperationSpec into out
func (in *VolumeBackupOperationSpec) DeepCopyInto(out *VolumeBackupOperationSpec) {
	*out = *in
	in.SourcePVC.DeepCopyInto(&out.SourcePVC)
	if in.DestinationPVC != nil {
		in, out := &in.DestinationPVC, &out.DestinationPVC
		*out = new(PVCReference)
		(*in).DeepCopyInto(*out)
	}
	in.BackupRepositoryRef.DeepCopyInto(&out.BackupRepositoryRef)
}

// DeepCopy creates a deep copy of VolumeBackupOperationSpec
func (in *VolumeBackupOperationSpec) DeepCopy() *VolumeBackupOperationSpec {
	if in == nil {
		return nil
	}
	out := new(VolumeBackupOperationSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies VolumeBackupOperationStatus into out
func (in *VolumeBackupOperationStatus) DeepCopyInto(out *VolumeBackupOperationStatus) {
	*out = *in
	if in.StartTime != nil {
		in, out := &in.StartTime, &out.StartTime
		*out = (*in).DeepCopy()
	}
	if in.CompletionTime != nil {
		in, out := &in.CompletionTime, &out.CompletionTime
		*out = (*in).DeepCopy()
	}
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]metav1.Condition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy creates a deep copy of VolumeBackupOperationStatus
func (in *VolumeBackupOperationStatus) DeepCopy() *VolumeBackupOperationStatus {
	if in == nil {
		return nil
	}
	out := new(VolumeBackupOperationStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies VolumeBackupOperation into out
func (in *VolumeBackupOperation) DeepCopyInto(out *VolumeBackupOperation) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy creates a deep copy of VolumeBackupOperation
func (in *VolumeBackupOperation) DeepCopy() *VolumeBackupOperation {
	if in == nil {
		return nil
	}
	out := new(VolumeBackupOperation)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject implements runtime.Object interface
func (in *VolumeBackupOperation) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto copies VolumeBackupOperationList into out
func (in *VolumeBackupOperationList) DeepCopyInto(out *VolumeBackupOperationList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]VolumeBackupOperation, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy creates a deep copy of VolumeBackupOperationList
func (in *VolumeBackupOperationList) DeepCopy() *VolumeBackupOperationList {
	if in == nil {
		return nil
	}
	out := new(VolumeBackupOperationList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject implements runtime.Object interface
func (in *VolumeBackupOperationList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}
