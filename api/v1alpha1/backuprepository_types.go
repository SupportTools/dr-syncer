package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// BackupRepositoryState represents the current state of a backup repository
// +kubebuilder:validation:Enum=Initializing;Ready;Error;Maintenance
type BackupRepositoryState string

const (
	// BackupRepositoryStateInitializing indicates the repository is being set up
	BackupRepositoryStateInitializing BackupRepositoryState = "Initializing"
	// BackupRepositoryStateReady indicates the repository is healthy and ready
	BackupRepositoryStateReady BackupRepositoryState = "Ready"
	// BackupRepositoryStateError indicates the repository has an error
	BackupRepositoryStateError BackupRepositoryState = "Error"
	// BackupRepositoryStateMaintenance indicates the repository is undergoing maintenance
	BackupRepositoryStateMaintenance BackupRepositoryState = "Maintenance"
)

// S3Config defines the S3-compatible storage backend configuration
type S3Config struct {
	// Endpoint is the S3 endpoint URL (e.g., "s3.amazonaws.com", "minio.example.com:9000")
	// +kubebuilder:validation:MinLength=1
	Endpoint string `json:"endpoint"`

	// Bucket is the S3 bucket name
	// +kubebuilder:validation:MinLength=1
	Bucket string `json:"bucket"`

	// Region is the S3 region (e.g., "us-west-2")
	// +optional
	Region string `json:"region,omitempty"`

	// PathPrefix is an optional prefix for all objects in the bucket
	// +optional
	PathPrefix string `json:"pathPrefix,omitempty"`

	// CredentialsSecretRef references a secret containing S3 access credentials.
	// The secret must contain keys "accessKeyID" and "secretAccessKey".
	CredentialsSecretRef SecretReference `json:"credentialsSecretRef"`
}

// KopiaConfig defines Kopia repository settings
type KopiaConfig struct {
	// EncryptionSecretRef references a secret containing the Kopia repository password.
	// The secret must contain a "password" key.
	EncryptionSecretRef SecretReference `json:"encryptionSecretRef"`

	// CompressionAlgorithm specifies the compression algorithm for backups.
	// +optional
	// +kubebuilder:default="zstd"
	// +kubebuilder:validation:Enum=zstd;s2;gzip;none
	CompressionAlgorithm string `json:"compressionAlgorithm,omitempty"`
}

// BackupRetentionPolicy defines how many snapshots to retain
type BackupRetentionPolicy struct {
	// KeepLatest is the number of most recent snapshots to keep
	// +optional
	// +kubebuilder:default=10
	// +kubebuilder:validation:Minimum=1
	KeepLatest *int32 `json:"keepLatest,omitempty"`

	// KeepDaily is the number of daily snapshots to keep
	// +optional
	// +kubebuilder:default=7
	// +kubebuilder:validation:Minimum=0
	KeepDaily *int32 `json:"keepDaily,omitempty"`

	// KeepWeekly is the number of weekly snapshots to keep
	// +optional
	// +kubebuilder:default=4
	// +kubebuilder:validation:Minimum=0
	KeepWeekly *int32 `json:"keepWeekly,omitempty"`

	// KeepMonthly is the number of monthly snapshots to keep
	// +optional
	// +kubebuilder:default=12
	// +kubebuilder:validation:Minimum=0
	KeepMonthly *int32 `json:"keepMonthly,omitempty"`
}

// BackupRepositorySpec defines the desired state of BackupRepository
type BackupRepositorySpec struct {
	// S3Config defines the S3-compatible storage backend
	S3Config S3Config `json:"s3Config"`

	// KopiaConfig defines Kopia repository settings
	KopiaConfig KopiaConfig `json:"kopiaConfig"`

	// MaintenanceSchedule is a cron expression for repository maintenance (index compaction, GC).
	// +optional
	// +kubebuilder:default="0 2 * * *"
	// +kubebuilder:validation:Pattern=^(\*|([0-9]|1[0-9]|2[0-9]|3[0-9]|4[0-9]|5[0-9])|\*/[0-9]+)\s+(\*|([0-9]|1[0-9]|2[0-3])|\*/[0-9]+)\s+(\*|([1-9]|1[0-9]|2[0-9]|3[0-1])|\*/[0-9]+)\s+(\*|([1-9]|1[0-2])|\*/[0-9]+)\s+(\*|([0-6])|\*/[0-9]+)$
	MaintenanceSchedule string `json:"maintenanceSchedule,omitempty"`

	// RetentionPolicy defines how many snapshots to retain
	// +optional
	RetentionPolicy *BackupRetentionPolicy `json:"retentionPolicy,omitempty"`
}

// BackupRepositoryStatus defines the observed state of BackupRepository
type BackupRepositoryStatus struct {
	// State represents the current state of the backup repository
	// +optional
	State BackupRepositoryState `json:"state,omitempty"`

	// LastMaintenanceTime is when maintenance was last run
	// +optional
	LastMaintenanceTime *metav1.Time `json:"lastMaintenanceTime,omitempty"`

	// LastHealthCheckTime is when the health check was last run
	// +optional
	LastHealthCheckTime *metav1.Time `json:"lastHealthCheckTime,omitempty"`

	// RepositorySize is the total size of the repository in bytes
	// +optional
	RepositorySize int64 `json:"repositorySize,omitempty"`

	// SnapshotCount is the total number of snapshots in the repository
	// +optional
	SnapshotCount int32 `json:"snapshotCount,omitempty"`

	// Message provides additional status information
	// +optional
	Message string `json:"message,omitempty"`

	// Conditions represent the latest available observations of the repository's state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="State",type="string",JSONPath=".status.state",description="Current state of the backup repository"
// +kubebuilder:printcolumn:name="Bucket",type="string",JSONPath=".spec.s3Config.bucket",description="S3 bucket name"
// +kubebuilder:printcolumn:name="Snapshots",type="integer",JSONPath=".status.snapshotCount",description="Number of snapshots"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:shortName=br

// BackupRepository represents an S3-backed Kopia repository for storing
// deduplicated, encrypted backups of PVC data.
type BackupRepository struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BackupRepositorySpec   `json:"spec"`
	Status BackupRepositoryStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// BackupRepositoryList contains a list of BackupRepository resources
type BackupRepositoryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BackupRepository `json:"items"`
}

func init() {
	SchemeBuilder.Register(&BackupRepository{}, &BackupRepositoryList{})
}

// --- DeepCopy methods ---

// DeepCopyInto copies S3Config into out
func (in *S3Config) DeepCopyInto(out *S3Config) {
	*out = *in
}

// DeepCopy creates a deep copy of S3Config
func (in *S3Config) DeepCopy() *S3Config {
	if in == nil {
		return nil
	}
	out := new(S3Config)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies KopiaConfig into out
func (in *KopiaConfig) DeepCopyInto(out *KopiaConfig) {
	*out = *in
}

// DeepCopy creates a deep copy of KopiaConfig
func (in *KopiaConfig) DeepCopy() *KopiaConfig {
	if in == nil {
		return nil
	}
	out := new(KopiaConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies BackupRetentionPolicy into out
func (in *BackupRetentionPolicy) DeepCopyInto(out *BackupRetentionPolicy) {
	*out = *in
	if in.KeepLatest != nil {
		in, out := &in.KeepLatest, &out.KeepLatest
		*out = new(int32)
		**out = **in
	}
	if in.KeepDaily != nil {
		in, out := &in.KeepDaily, &out.KeepDaily
		*out = new(int32)
		**out = **in
	}
	if in.KeepWeekly != nil {
		in, out := &in.KeepWeekly, &out.KeepWeekly
		*out = new(int32)
		**out = **in
	}
	if in.KeepMonthly != nil {
		in, out := &in.KeepMonthly, &out.KeepMonthly
		*out = new(int32)
		**out = **in
	}
}

// DeepCopy creates a deep copy of BackupRetentionPolicy
func (in *BackupRetentionPolicy) DeepCopy() *BackupRetentionPolicy {
	if in == nil {
		return nil
	}
	out := new(BackupRetentionPolicy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies BackupRepositorySpec into out
func (in *BackupRepositorySpec) DeepCopyInto(out *BackupRepositorySpec) {
	*out = *in
	in.S3Config.DeepCopyInto(&out.S3Config)
	in.KopiaConfig.DeepCopyInto(&out.KopiaConfig)
	if in.RetentionPolicy != nil {
		in, out := &in.RetentionPolicy, &out.RetentionPolicy
		*out = new(BackupRetentionPolicy)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy creates a deep copy of BackupRepositorySpec
func (in *BackupRepositorySpec) DeepCopy() *BackupRepositorySpec {
	if in == nil {
		return nil
	}
	out := new(BackupRepositorySpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies BackupRepositoryStatus into out
func (in *BackupRepositoryStatus) DeepCopyInto(out *BackupRepositoryStatus) {
	*out = *in
	if in.LastMaintenanceTime != nil {
		in, out := &in.LastMaintenanceTime, &out.LastMaintenanceTime
		*out = (*in).DeepCopy()
	}
	if in.LastHealthCheckTime != nil {
		in, out := &in.LastHealthCheckTime, &out.LastHealthCheckTime
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

// DeepCopy creates a deep copy of BackupRepositoryStatus
func (in *BackupRepositoryStatus) DeepCopy() *BackupRepositoryStatus {
	if in == nil {
		return nil
	}
	out := new(BackupRepositoryStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies BackupRepository into out
func (in *BackupRepository) DeepCopyInto(out *BackupRepository) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy creates a deep copy of BackupRepository
func (in *BackupRepository) DeepCopy() *BackupRepository {
	if in == nil {
		return nil
	}
	out := new(BackupRepository)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject implements runtime.Object interface
func (in *BackupRepository) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto copies BackupRepositoryList into out
func (in *BackupRepositoryList) DeepCopyInto(out *BackupRepositoryList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]BackupRepository, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy creates a deep copy of BackupRepositoryList
func (in *BackupRepositoryList) DeepCopy() *BackupRepositoryList {
	if in == nil {
		return nil
	}
	out := new(BackupRepositoryList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject implements runtime.Object interface
func (in *BackupRepositoryList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}
