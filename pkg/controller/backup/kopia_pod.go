package backup

import (
	"fmt"
	"regexp"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
)

var (
	// snapshotIDPattern matches valid Kopia snapshot IDs: alphanumeric and hyphens only.
	snapshotIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]*$`)

	// s3FieldPattern matches safe S3 configuration values: alphanumeric, hyphens, dots, slashes, colons, underscores.
	s3FieldPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.\-/:_]*$`)
)

// ValidateSnapshotID checks that a Kopia snapshot ID contains only safe characters.
// This prevents shell injection when the ID is used in pod command construction.
func ValidateSnapshotID(snapshotID string) error {
	if snapshotID == "" {
		return fmt.Errorf("snapshot ID is empty")
	}
	if len(snapshotID) > 256 {
		return fmt.Errorf("snapshot ID exceeds maximum length of 256")
	}
	if !snapshotIDPattern.MatchString(snapshotID) {
		return fmt.Errorf("snapshot ID contains invalid characters: %q", snapshotID)
	}
	return nil
}

// ValidateS3Field checks that an S3 configuration field contains only safe characters.
func ValidateS3Field(fieldName, value string) error {
	if value == "" {
		return nil
	}
	if len(value) > 1024 {
		return fmt.Errorf("S3 %s exceeds maximum length of 1024", fieldName)
	}
	if !s3FieldPattern.MatchString(value) {
		return fmt.Errorf("S3 %s contains invalid characters: %q", fieldName, value)
	}
	return nil
}

const (
	// DefaultKopiaImage is the default Kopia container image.
	DefaultKopiaImage = "kopia/kopia:0.17"

	// kopiaPodPrefix is used for generating Kopia pod names.
	kopiaPodPrefix = "dr-syncer-kopia"

	// DefaultActiveDeadlineSeconds is the default pod deadline (1 hour).
	DefaultActiveDeadlineSeconds int64 = 3600

	// Volume names used within Kopia pods.
	volumeNameData      = "data"
	volumeNameKopiaHome = "kopia-home"
	volumeNameKopiaTmp  = "kopia-tmp"

	// Mount paths inside the Kopia container.
	mountPathData      = "/data"
	mountPathKopiaHome = "/home/kopia"
	mountPathKopiaTmp  = "/tmp/kopia"

	// Label and annotation keys specific to Kopia pods.
	labelKopiaComponent = "app.kubernetes.io/component"
	labelKopiaName      = "app.kubernetes.io/name"
	labelKopiaInstance  = "app.kubernetes.io/instance"
	labelKopiaManagedBy = "app.kubernetes.io/managed-by"

	annotationKopiaCreatedAt = "dr-syncer.io/created-at"
	annotationKopiaOperation = "dr-syncer.io/operation-name"
	annotationKopiaPVC       = "dr-syncer.io/pvc-name"
)

// KopiaPodConfig holds configurable options for building Kopia pod specs.
type KopiaPodConfig struct {
	// Image is the Kopia container image to use.
	Image string

	// Parallelism controls --parallel for Kopia snapshot operations.
	Parallelism int32

	// CompressionAlgorithm controls --compression for Kopia snapshot create.
	CompressionAlgorithm string

	// ResourceRequirements for the Kopia container.
	Resources corev1.ResourceRequirements

	// NodeName pins the pod to a specific node (used for live-path backup
	// where the PVC is bound to a node).
	NodeName string

	// Tolerations applied to the Kopia pod.
	Tolerations []corev1.Toleration

	// PriorityClassName for the Kopia pod.
	PriorityClassName string

	// ActiveDeadlineSeconds limits how long the pod can run.
	// Prevents runaway pods if the controller crashes.
	ActiveDeadlineSeconds *int64
}

// DefaultKopiaPodConfig returns a KopiaPodConfig with sensible defaults.
func DefaultKopiaPodConfig() KopiaPodConfig {
	deadline := DefaultActiveDeadlineSeconds
	return KopiaPodConfig{
		Image:                DefaultKopiaImage,
		Parallelism:          4,
		CompressionAlgorithm: "zstd",
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("250m"),
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			},
		},
		ActiveDeadlineSeconds: &deadline,
	}
}

// KopiaPodConfigFromBackupConfig creates a KopiaPodConfig from a BackupConfig,
// overlaying user-specified values onto defaults.
func KopiaPodConfigFromBackupConfig(bc *drv1alpha1.BackupConfig) KopiaPodConfig {
	cfg := DefaultKopiaPodConfig()
	if bc == nil {
		return cfg
	}
	if bc.Parallelism != nil {
		cfg.Parallelism = *bc.Parallelism
	}
	if bc.CompressionAlgorithm != "" {
		cfg.CompressionAlgorithm = bc.CompressionAlgorithm
	}
	return cfg
}

// BuildBackupPod creates a Pod spec for a Kopia backup (snapshot create) operation.
// The pod mounts the source PVC at /data and runs `kopia snapshot create /data`.
// Returns an error if S3 config fields contain unsafe characters.
func BuildBackupPod(
	operation *drv1alpha1.VolumeBackupOperation,
	repo *drv1alpha1.BackupRepository,
	cfg KopiaPodConfig,
) (*corev1.Pod, error) {
	if err := validateS3Config(repo.Spec.S3Config); err != nil {
		return nil, fmt.Errorf("invalid S3 config: %w", err)
	}

	podName := fmt.Sprintf("%s-backup-%s", kopiaPodPrefix, operation.Name)
	if len(podName) > 63 {
		podName = podName[:63]
	}

	pvcRef := operation.Spec.SourcePVC
	command := buildBackupCommand(repo.Spec.S3Config, cfg)

	return buildKopiaPod(podName, operation, repo, pvcRef, cfg, command, "backup"), nil
}

// BuildRestorePod creates a Pod spec for a Kopia restore (snapshot restore) operation.
// The pod mounts the destination PVC at /data and runs `kopia snapshot restore <snapshotID> /data`.
// Returns an error if the snapshot ID or S3 config fields contain unsafe characters.
func BuildRestorePod(
	operation *drv1alpha1.VolumeBackupOperation,
	repo *drv1alpha1.BackupRepository,
	cfg KopiaPodConfig,
) (*corev1.Pod, error) {
	if err := ValidateSnapshotID(operation.Spec.SnapshotID); err != nil {
		return nil, fmt.Errorf("invalid snapshot ID: %w", err)
	}
	if err := validateS3Config(repo.Spec.S3Config); err != nil {
		return nil, fmt.Errorf("invalid S3 config: %w", err)
	}

	podName := fmt.Sprintf("%s-restore-%s", kopiaPodPrefix, operation.Name)
	if len(podName) > 63 {
		podName = podName[:63]
	}

	// For restore, mount the destination PVC. Fall back to source PVC if no destination is set.
	pvcRef := operation.Spec.SourcePVC
	if operation.Spec.DestinationPVC != nil {
		pvcRef = *operation.Spec.DestinationPVC
	}

	command := buildRestoreCommand(operation.Spec.SnapshotID, repo.Spec.S3Config, cfg)

	return buildKopiaPod(podName, operation, repo, pvcRef, cfg, command, "restore"), nil
}

// validateS3Config checks that all S3 configuration fields used in shell commands are safe.
func validateS3Config(s3Config drv1alpha1.S3Config) error {
	if err := ValidateS3Field("bucket", s3Config.Bucket); err != nil {
		return err
	}
	if err := ValidateS3Field("endpoint", s3Config.Endpoint); err != nil {
		return err
	}
	if err := ValidateS3Field("region", s3Config.Region); err != nil {
		return err
	}
	if err := ValidateS3Field("pathPrefix", s3Config.PathPrefix); err != nil {
		return err
	}
	return nil
}

// buildKopiaPod constructs the core Pod resource shared between backup and restore.
func buildKopiaPod(
	podName string,
	operation *drv1alpha1.VolumeBackupOperation,
	repo *drv1alpha1.BackupRepository,
	pvcRef drv1alpha1.PVCReference,
	cfg KopiaPodConfig,
	command []string,
	component string,
) *corev1.Pod {
	labels := buildLabels(operation, component)
	annotations := buildAnnotations(operation)

	allowPrivEsc := false

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        podName,
			Namespace:   pvcRef.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{
			RestartPolicy:         corev1.RestartPolicyNever,
			ActiveDeadlineSeconds: cfg.ActiveDeadlineSeconds,
			SecurityContext: &corev1.PodSecurityContext{
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
			Containers: []corev1.Container{
				{
					Name:    "kopia",
					Image:   cfg.Image,
					Command: []string{"/bin/sh", "-c"},
					Args:    []string{joinCommand(command)},
					Env:     buildEnvVars(repo),
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      volumeNameData,
							MountPath: mountPathData,
						},
						{
							Name:      volumeNameKopiaHome,
							MountPath: mountPathKopiaHome,
						},
						{
							Name:      volumeNameKopiaTmp,
							MountPath: mountPathKopiaTmp,
						},
					},
					Resources:                cfg.Resources,
					TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &allowPrivEsc,
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{"ALL"},
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: volumeNameData,
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvcRef.Name,
						},
					},
				},
				{
					Name: volumeNameKopiaHome,
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
				{
					Name: volumeNameKopiaTmp,
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
			},
		},
	}

	// Pin to specific node for live-path access.
	if cfg.NodeName != "" {
		pod.Spec.NodeName = cfg.NodeName
	}

	if len(cfg.Tolerations) > 0 {
		pod.Spec.Tolerations = cfg.Tolerations
	}

	if cfg.PriorityClassName != "" {
		pod.Spec.PriorityClassName = cfg.PriorityClassName
	}

	return pod
}

// buildBackupCommand constructs the shell command sequence for a Kopia backup:
// 1. Connect to the repository
// 2. Create a snapshot of /data
func buildBackupCommand(s3Config drv1alpha1.S3Config, cfg KopiaPodConfig) []string {
	parts := []string{}

	// Step 1: Connect to the repository.
	connectArgs := []string{
		"kopia", "repository", "connect", "s3",
	}
	connectArgs = append(connectArgs, buildS3ConnectArgs(s3Config)...)
	connectArgs = append(connectArgs,
		"--override-hostname=dr-syncer",
		"--override-username=dr-syncer",
	)
	parts = append(parts, connectArgs...)

	// Step 2: Create snapshot.
	snapshotArgs := []string{
		"&&", "kopia", "snapshot", "create", mountPathData,
		"--parallel=" + strconv.Itoa(int(cfg.Parallelism)),
		"--json",
	}
	if cfg.CompressionAlgorithm != "" && cfg.CompressionAlgorithm != "none" {
		snapshotArgs = append(snapshotArgs, "--compression="+cfg.CompressionAlgorithm)
	}
	parts = append(parts, snapshotArgs...)

	return parts
}

// buildRestoreCommand constructs the shell command sequence for a Kopia restore:
// 1. Connect to the repository
// 2. Restore a snapshot to /data
func buildRestoreCommand(snapshotID string, s3Config drv1alpha1.S3Config, cfg KopiaPodConfig) []string {
	parts := []string{}

	// Step 1: Connect to the repository.
	connectArgs := []string{
		"kopia", "repository", "connect", "s3",
	}
	connectArgs = append(connectArgs, buildS3ConnectArgs(s3Config)...)
	connectArgs = append(connectArgs,
		"--override-hostname=dr-syncer",
		"--override-username=dr-syncer",
	)
	parts = append(parts, connectArgs...)

	// Step 2: Restore snapshot.
	restoreArgs := []string{
		"&&", "kopia", "snapshot", "restore",
		snapshotID,
		mountPathData,
		"--parallel=" + strconv.Itoa(int(cfg.Parallelism)),
	}
	parts = append(parts, restoreArgs...)

	return parts
}

// buildS3ConnectArgs returns the S3 backend arguments for `kopia repository connect s3`.
func buildS3ConnectArgs(s3Config drv1alpha1.S3Config) []string {
	args := []string{
		"--bucket=" + s3Config.Bucket,
		"--endpoint=" + s3Config.Endpoint,
	}
	if s3Config.Region != "" {
		args = append(args, "--region="+s3Config.Region)
	}
	if s3Config.PathPrefix != "" {
		args = append(args, "--prefix="+s3Config.PathPrefix)
	}
	return args
}

// buildEnvVars creates the environment variables for the Kopia container.
// Credentials are injected from Kubernetes secrets via SecretKeyRef.
func buildEnvVars(repo *drv1alpha1.BackupRepository) []corev1.EnvVar {
	s3CredsRef := repo.Spec.S3Config.CredentialsSecretRef
	kopiaEncRef := repo.Spec.KopiaConfig.EncryptionSecretRef

	return []corev1.EnvVar{
		{
			Name: "AWS_ACCESS_KEY_ID",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: s3CredsRef.Name},
					Key:                  "accessKeyID",
				},
			},
		},
		{
			Name: "AWS_SECRET_ACCESS_KEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: s3CredsRef.Name},
					Key:                  "secretAccessKey",
				},
			},
		},
		{
			Name: "KOPIA_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: kopiaEncRef.Name},
					Key:                  "password",
				},
			},
		},
		{
			Name:  "KOPIA_CHECK_FOR_UPDATES",
			Value: "false",
		},
		{
			Name:  "HOME",
			Value: mountPathKopiaHome,
		},
	}
}

// buildLabels returns the standard labels applied to Kopia pods.
func buildLabels(operation *drv1alpha1.VolumeBackupOperation, component string) map[string]string {
	return map[string]string{
		labelKopiaName:      "dr-syncer-kopia",
		labelKopiaInstance:  operation.Name,
		labelKopiaComponent: component,
		labelKopiaManagedBy: "dr-syncer",
		labelMapping:        operation.Labels[labelMapping],
		labelPVC:            operation.Labels[labelPVC],
		labelOperation:      operation.Labels[labelOperation],
	}
}

// buildAnnotations returns annotations for tracking Kopia pods.
func buildAnnotations(operation *drv1alpha1.VolumeBackupOperation) map[string]string {
	return map[string]string{
		annotationKopiaCreatedAt: time.Now().UTC().Format(time.RFC3339),
		annotationKopiaOperation: operation.Name,
		annotationKopiaPVC:       operation.Spec.SourcePVC.Name,
	}
}

// joinCommand joins command parts into a single shell command string.
func joinCommand(parts []string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += " "
		}
		result += p
	}
	return result
}
