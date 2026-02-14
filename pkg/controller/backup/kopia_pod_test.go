package backup

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
)

func newTestOperation(opType drv1alpha1.OperationType, snapshotID string) *drv1alpha1.VolumeBackupOperation {
	op := &drv1alpha1.VolumeBackupOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-mapping-test-pvc-backup",
			Namespace: "test-ns",
			Labels: map[string]string{
				labelMapping:   "test-mapping",
				labelPVC:       "test-pvc",
				labelOperation: string(opType),
			},
		},
		Spec: drv1alpha1.VolumeBackupOperationSpec{
			OperationType: opType,
			SourcePVC: drv1alpha1.PVCReference{
				Namespace: "test-ns",
				Name:      "test-pvc",
			},
			BackupRepositoryRef: drv1alpha1.SecretReference{
				Name:      "my-backup-repo",
				Namespace: "test-ns",
			},
			SnapshotID:         snapshotID,
			DataAccessStrategy: drv1alpha1.DataAccessStrategyLive,
		},
	}
	if opType == drv1alpha1.OperationTypeRestore {
		op.Spec.DestinationPVC = &drv1alpha1.PVCReference{
			Namespace: "dest-ns",
			Name:      "dest-pvc",
		}
	}
	return op
}

func newTestBackupRepo() *drv1alpha1.BackupRepository {
	return &drv1alpha1.BackupRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-backup-repo",
			Namespace: "test-ns",
		},
		Spec: drv1alpha1.BackupRepositorySpec{
			S3Config: drv1alpha1.S3Config{
				Endpoint: "s3.amazonaws.com",
				Bucket:   "dr-syncer-backups",
				Region:   "us-west-2",
				CredentialsSecretRef: drv1alpha1.SecretReference{
					Name:      "s3-creds",
					Namespace: "test-ns",
				},
			},
			KopiaConfig: drv1alpha1.KopiaConfig{
				EncryptionSecretRef: drv1alpha1.SecretReference{
					Name:      "kopia-enc",
					Namespace: "test-ns",
				},
				CompressionAlgorithm: "zstd",
			},
		},
	}
}

func TestDefaultKopiaPodConfig(t *testing.T) {
	cfg := DefaultKopiaPodConfig()

	if cfg.Image != DefaultKopiaImage {
		t.Errorf("expected image %s, got %s", DefaultKopiaImage, cfg.Image)
	}
	if cfg.Parallelism != 4 {
		t.Errorf("expected parallelism 4, got %d", cfg.Parallelism)
	}
	if cfg.CompressionAlgorithm != "zstd" {
		t.Errorf("expected compression zstd, got %s", cfg.CompressionAlgorithm)
	}
	if cfg.Resources.Limits.Cpu().String() != "2" {
		t.Errorf("expected cpu limit 2, got %s", cfg.Resources.Limits.Cpu().String())
	}
}

func TestKopiaPodConfigFromBackupConfig(t *testing.T) {
	parallelism := int32(8)
	bc := &drv1alpha1.BackupConfig{
		Parallelism:          &parallelism,
		CompressionAlgorithm: "s2",
	}

	cfg := KopiaPodConfigFromBackupConfig(bc)

	if cfg.Parallelism != 8 {
		t.Errorf("expected parallelism 8, got %d", cfg.Parallelism)
	}
	if cfg.CompressionAlgorithm != "s2" {
		t.Errorf("expected compression s2, got %s", cfg.CompressionAlgorithm)
	}
	// Image should still be default.
	if cfg.Image != DefaultKopiaImage {
		t.Errorf("expected default image, got %s", cfg.Image)
	}
}

func TestKopiaPodConfigFromNilBackupConfig(t *testing.T) {
	cfg := KopiaPodConfigFromBackupConfig(nil)

	if cfg.Parallelism != 4 {
		t.Errorf("expected default parallelism 4, got %d", cfg.Parallelism)
	}
}

func TestBuildBackupPod(t *testing.T) {
	op := newTestOperation(drv1alpha1.OperationTypeBackup, "")
	repo := newTestBackupRepo()
	cfg := DefaultKopiaPodConfig()
	cfg.NodeName = "worker-1"

	pod := BuildBackupPod(op, repo, cfg)

	// Basic metadata.
	if pod.Namespace != "test-ns" {
		t.Errorf("expected namespace test-ns, got %s", pod.Namespace)
	}
	if !strings.HasPrefix(pod.Name, kopiaPodPrefix+"-backup-") {
		t.Errorf("expected pod name prefix %s-backup-, got %s", kopiaPodPrefix, pod.Name)
	}

	// Labels.
	if pod.Labels[labelKopiaName] != "dr-syncer-kopia" {
		t.Errorf("expected label %s=dr-syncer-kopia, got %s", labelKopiaName, pod.Labels[labelKopiaName])
	}
	if pod.Labels[labelKopiaComponent] != "backup" {
		t.Errorf("expected component label backup, got %s", pod.Labels[labelKopiaComponent])
	}
	if pod.Labels[labelKopiaManagedBy] != "dr-syncer" {
		t.Errorf("expected managed-by label dr-syncer, got %s", pod.Labels[labelKopiaManagedBy])
	}

	// Pod spec.
	if pod.Spec.RestartPolicy != corev1.RestartPolicyNever {
		t.Errorf("expected RestartPolicyNever, got %s", pod.Spec.RestartPolicy)
	}
	if pod.Spec.NodeName != "worker-1" {
		t.Errorf("expected NodeName worker-1, got %s", pod.Spec.NodeName)
	}

	// Container.
	if len(pod.Spec.Containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(pod.Spec.Containers))
	}
	c := pod.Spec.Containers[0]

	if c.Image != DefaultKopiaImage {
		t.Errorf("expected image %s, got %s", DefaultKopiaImage, c.Image)
	}

	// Verify command runs via shell.
	if len(c.Command) != 2 || c.Command[0] != "/bin/sh" || c.Command[1] != "-c" {
		t.Errorf("expected command [/bin/sh -c], got %v", c.Command)
	}

	// Verify args contain the kopia commands.
	if len(c.Args) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(c.Args))
	}
	cmdStr := c.Args[0]
	if !strings.Contains(cmdStr, "kopia repository connect s3") {
		t.Error("expected 'kopia repository connect s3' in command")
	}
	if !strings.Contains(cmdStr, "kopia snapshot create /data") {
		t.Error("expected 'kopia snapshot create /data' in command")
	}
	if !strings.Contains(cmdStr, "--parallel=4") {
		t.Error("expected --parallel=4 in command")
	}
	if !strings.Contains(cmdStr, "--compression=zstd") {
		t.Error("expected --compression=zstd in command")
	}
	if !strings.Contains(cmdStr, "--bucket=dr-syncer-backups") {
		t.Error("expected --bucket=dr-syncer-backups in command")
	}
	if !strings.Contains(cmdStr, "--endpoint=s3.amazonaws.com") {
		t.Error("expected --endpoint=s3.amazonaws.com in command")
	}
	if !strings.Contains(cmdStr, "--region=us-west-2") {
		t.Error("expected --region=us-west-2 in command")
	}
	if !strings.Contains(cmdStr, "--json") {
		t.Error("expected --json in command")
	}

	// Verify volumes.
	if len(pod.Spec.Volumes) != 3 {
		t.Fatalf("expected 3 volumes, got %d", len(pod.Spec.Volumes))
	}
	verifyVolume(t, pod.Spec.Volumes[0], volumeNameData, "test-pvc")
	verifyEmptyDirVolume(t, pod.Spec.Volumes[1], volumeNameKopiaHome)
	verifyEmptyDirVolume(t, pod.Spec.Volumes[2], volumeNameKopiaTmp)

	// Verify volume mounts.
	if len(c.VolumeMounts) != 3 {
		t.Fatalf("expected 3 volume mounts, got %d", len(c.VolumeMounts))
	}
	verifyVolumeMount(t, c.VolumeMounts[0], volumeNameData, mountPathData)
	verifyVolumeMount(t, c.VolumeMounts[1], volumeNameKopiaHome, mountPathKopiaHome)
	verifyVolumeMount(t, c.VolumeMounts[2], volumeNameKopiaTmp, mountPathKopiaTmp)

	// Verify env vars use SecretKeyRef.
	verifySecretEnvVar(t, c.Env, "AWS_ACCESS_KEY_ID", "s3-creds", "accessKeyID")
	verifySecretEnvVar(t, c.Env, "AWS_SECRET_ACCESS_KEY", "s3-creds", "secretAccessKey")
	verifySecretEnvVar(t, c.Env, "KOPIA_PASSWORD", "kopia-enc", "password")
	verifyPlainEnvVar(t, c.Env, "KOPIA_CHECK_FOR_UPDATES", "false")
	verifyPlainEnvVar(t, c.Env, "HOME", mountPathKopiaHome)
}

func TestBuildRestorePod(t *testing.T) {
	op := newTestOperation(drv1alpha1.OperationTypeRestore, "k1234abcdef")
	repo := newTestBackupRepo()
	cfg := DefaultKopiaPodConfig()

	pod := BuildRestorePod(op, repo, cfg)

	// Restore pods should be in the destination PVC's namespace.
	if pod.Namespace != "dest-ns" {
		t.Errorf("expected namespace dest-ns, got %s", pod.Namespace)
	}
	if !strings.HasPrefix(pod.Name, kopiaPodPrefix+"-restore-") {
		t.Errorf("expected pod name prefix %s-restore-, got %s", kopiaPodPrefix, pod.Name)
	}
	if pod.Labels[labelKopiaComponent] != "restore" {
		t.Errorf("expected component label restore, got %s", pod.Labels[labelKopiaComponent])
	}

	// The data volume should reference the destination PVC.
	if len(pod.Spec.Volumes) < 1 {
		t.Fatal("expected at least 1 volume")
	}
	pvcSource := pod.Spec.Volumes[0].VolumeSource.PersistentVolumeClaim
	if pvcSource == nil || pvcSource.ClaimName != "dest-pvc" {
		t.Errorf("expected data volume to reference dest-pvc, got %v", pvcSource)
	}

	// Verify restore command.
	cmdStr := pod.Spec.Containers[0].Args[0]
	if !strings.Contains(cmdStr, "kopia snapshot restore") {
		t.Error("expected 'kopia snapshot restore' in command")
	}
	if !strings.Contains(cmdStr, "k1234abcdef") {
		t.Error("expected snapshot ID k1234abcdef in command")
	}
	if !strings.Contains(cmdStr, mountPathData) {
		t.Errorf("expected %s in restore command", mountPathData)
	}

	// NodeName should be empty when not configured.
	if pod.Spec.NodeName != "" {
		t.Errorf("expected empty NodeName, got %s", pod.Spec.NodeName)
	}
}

func TestBuildBackupPodWithS3PathPrefix(t *testing.T) {
	op := newTestOperation(drv1alpha1.OperationTypeBackup, "")
	repo := newTestBackupRepo()
	repo.Spec.S3Config.PathPrefix = "cluster-a/backups"
	cfg := DefaultKopiaPodConfig()

	pod := BuildBackupPod(op, repo, cfg)
	cmdStr := pod.Spec.Containers[0].Args[0]

	if !strings.Contains(cmdStr, "--prefix=cluster-a/backups") {
		t.Error("expected --prefix=cluster-a/backups in command")
	}
}

func TestBuildBackupPodNoCompression(t *testing.T) {
	op := newTestOperation(drv1alpha1.OperationTypeBackup, "")
	repo := newTestBackupRepo()
	cfg := DefaultKopiaPodConfig()
	cfg.CompressionAlgorithm = "none"

	pod := BuildBackupPod(op, repo, cfg)
	cmdStr := pod.Spec.Containers[0].Args[0]

	if strings.Contains(cmdStr, "--compression") {
		t.Error("expected no --compression flag when compression is 'none'")
	}
}

func TestBuildBackupPodTolerations(t *testing.T) {
	op := newTestOperation(drv1alpha1.OperationTypeBackup, "")
	repo := newTestBackupRepo()
	cfg := DefaultKopiaPodConfig()
	cfg.Tolerations = []corev1.Toleration{
		{
			Key:      "dedicated",
			Operator: corev1.TolerationOpEqual,
			Value:    "backup",
			Effect:   corev1.TaintEffectNoSchedule,
		},
	}
	cfg.PriorityClassName = "system-cluster-critical"

	pod := BuildBackupPod(op, repo, cfg)

	if len(pod.Spec.Tolerations) != 1 {
		t.Fatalf("expected 1 toleration, got %d", len(pod.Spec.Tolerations))
	}
	if pod.Spec.Tolerations[0].Key != "dedicated" {
		t.Errorf("expected toleration key 'dedicated', got %s", pod.Spec.Tolerations[0].Key)
	}
	if pod.Spec.PriorityClassName != "system-cluster-critical" {
		t.Errorf("expected priority class system-cluster-critical, got %s", pod.Spec.PriorityClassName)
	}
}

func TestBuildRestorePodFallsBackToSourcePVC(t *testing.T) {
	op := newTestOperation(drv1alpha1.OperationTypeRestore, "snap123")
	op.Spec.DestinationPVC = nil // No destination PVC set.
	repo := newTestBackupRepo()
	cfg := DefaultKopiaPodConfig()

	pod := BuildRestorePod(op, repo, cfg)

	// Should fall back to source PVC.
	pvcSource := pod.Spec.Volumes[0].VolumeSource.PersistentVolumeClaim
	if pvcSource == nil || pvcSource.ClaimName != "test-pvc" {
		t.Errorf("expected fallback to source PVC test-pvc, got %v", pvcSource)
	}
	if pod.Namespace != "test-ns" {
		t.Errorf("expected namespace test-ns, got %s", pod.Namespace)
	}
}

func TestPodNameTruncation(t *testing.T) {
	op := newTestOperation(drv1alpha1.OperationTypeBackup, "")
	op.Name = "very-long-mapping-name-that-exceeds-kubernetes-maximum-name-length-limit"
	repo := newTestBackupRepo()
	cfg := DefaultKopiaPodConfig()

	pod := BuildBackupPod(op, repo, cfg)

	if len(pod.Name) > 63 {
		t.Errorf("pod name exceeds 63 chars: %s (len=%d)", pod.Name, len(pod.Name))
	}
}

func TestBuildBackupPodAnnotations(t *testing.T) {
	op := newTestOperation(drv1alpha1.OperationTypeBackup, "")
	repo := newTestBackupRepo()
	cfg := DefaultKopiaPodConfig()

	pod := BuildBackupPod(op, repo, cfg)

	if pod.Annotations[annotationKopiaOperation] != op.Name {
		t.Errorf("expected annotation %s=%s, got %s", annotationKopiaOperation, op.Name, pod.Annotations[annotationKopiaOperation])
	}
	if pod.Annotations[annotationKopiaPVC] != "test-pvc" {
		t.Errorf("expected annotation %s=test-pvc, got %s", annotationKopiaPVC, pod.Annotations[annotationKopiaPVC])
	}
	if _, ok := pod.Annotations[annotationKopiaCreatedAt]; !ok {
		t.Error("expected created-at annotation to be set")
	}
}

// --- buildBackupCommand / buildRestoreCommand tests ---

func TestBuildBackupCommand(t *testing.T) {
	s3Config := drv1alpha1.S3Config{
		Endpoint: "s3.example.com",
		Bucket:   "my-bucket",
		Region:   "eu-west-1",
	}
	cfg := DefaultKopiaPodConfig()

	cmd := buildBackupCommand(s3Config, cfg)
	joined := joinCommand(cmd)

	expectations := []string{
		"kopia repository connect s3",
		"--bucket=my-bucket",
		"--endpoint=s3.example.com",
		"--region=eu-west-1",
		"--override-hostname=dr-syncer",
		"--override-username=dr-syncer",
		"kopia snapshot create /data",
		"--parallel=4",
		"--json",
		"--compression=zstd",
	}
	for _, exp := range expectations {
		if !strings.Contains(joined, exp) {
			t.Errorf("expected %q in command, got: %s", exp, joined)
		}
	}
}

func TestBuildBackupCommand_NoRegion(t *testing.T) {
	s3Config := drv1alpha1.S3Config{
		Endpoint: "minio.local:9000",
		Bucket:   "backups",
	}
	cfg := DefaultKopiaPodConfig()

	cmd := buildBackupCommand(s3Config, cfg)
	joined := joinCommand(cmd)

	if strings.Contains(joined, "--region") {
		t.Error("expected no --region flag when region is empty")
	}
}

func TestBuildRestoreCommand(t *testing.T) {
	s3Config := drv1alpha1.S3Config{
		Endpoint:   "s3.example.com",
		Bucket:     "my-bucket",
		PathPrefix: "dr/snapshots",
	}
	cfg := DefaultKopiaPodConfig()
	cfg.Parallelism = 8

	cmd := buildRestoreCommand("snap-abc123", s3Config, cfg)
	joined := joinCommand(cmd)

	expectations := []string{
		"kopia repository connect s3",
		"--bucket=my-bucket",
		"--prefix=dr/snapshots",
		"kopia snapshot restore",
		"snap-abc123",
		"/data",
		"--parallel=8",
	}
	for _, exp := range expectations {
		if !strings.Contains(joined, exp) {
			t.Errorf("expected %q in command, got: %s", exp, joined)
		}
	}
	// Restore should NOT have --compression or --json.
	if strings.Contains(joined, "--compression") {
		t.Error("restore command should not have --compression")
	}
	if strings.Contains(joined, "--json") {
		t.Error("restore command should not have --json")
	}
}

func TestBuildS3ConnectArgs(t *testing.T) {
	tests := []struct {
		name     string
		config   drv1alpha1.S3Config
		contains []string
		excludes []string
	}{
		{
			name: "full config",
			config: drv1alpha1.S3Config{
				Bucket:     "test-bucket",
				Endpoint:   "s3.test.com",
				Region:     "us-east-1",
				PathPrefix: "prefix/path",
			},
			contains: []string{
				"--bucket=test-bucket",
				"--endpoint=s3.test.com",
				"--region=us-east-1",
				"--prefix=prefix/path",
			},
		},
		{
			name: "minimal config",
			config: drv1alpha1.S3Config{
				Bucket:   "minimal",
				Endpoint: "s3.minimal.com",
			},
			contains: []string{
				"--bucket=minimal",
				"--endpoint=s3.minimal.com",
			},
			excludes: []string{"--region", "--prefix"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := buildS3ConnectArgs(tt.config)
			joined := strings.Join(args, " ")
			for _, c := range tt.contains {
				if !strings.Contains(joined, c) {
					t.Errorf("expected %q in args, got: %s", c, joined)
				}
			}
			for _, e := range tt.excludes {
				if strings.Contains(joined, e) {
					t.Errorf("did not expect %q in args, got: %s", e, joined)
				}
			}
		})
	}
}

func TestJoinCommand(t *testing.T) {
	tests := []struct {
		name   string
		parts  []string
		expect string
	}{
		{
			name:   "empty",
			parts:  []string{},
			expect: "",
		},
		{
			name:   "single part",
			parts:  []string{"echo"},
			expect: "echo",
		},
		{
			name:   "multiple parts",
			parts:  []string{"kopia", "snapshot", "create", "/data"},
			expect: "kopia snapshot create /data",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := joinCommand(tt.parts)
			if got != tt.expect {
				t.Errorf("joinCommand(%v) = %q, want %q", tt.parts, got, tt.expect)
			}
		})
	}
}

func TestBuildEnvVars(t *testing.T) {
	repo := newTestBackupRepo()
	envs := buildEnvVars(repo)

	// Should have exactly 5 env vars.
	if len(envs) != 5 {
		t.Fatalf("expected 5 env vars, got %d", len(envs))
	}

	// Verify all expected env vars exist.
	names := make(map[string]bool)
	for _, e := range envs {
		names[e.Name] = true
	}
	for _, expected := range []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "KOPIA_PASSWORD", "KOPIA_CHECK_FOR_UPDATES", "HOME"} {
		if !names[expected] {
			t.Errorf("missing env var %s", expected)
		}
	}
}

func TestBuildLabels(t *testing.T) {
	op := newTestOperation(drv1alpha1.OperationTypeBackup, "")
	labels := buildLabels(op, "backup")

	if labels[labelKopiaName] != "dr-syncer-kopia" {
		t.Errorf("expected name label dr-syncer-kopia, got %s", labels[labelKopiaName])
	}
	if labels[labelKopiaInstance] != op.Name {
		t.Errorf("expected instance label %s, got %s", op.Name, labels[labelKopiaInstance])
	}
	if labels[labelKopiaComponent] != "backup" {
		t.Errorf("expected component label backup, got %s", labels[labelKopiaComponent])
	}
	if labels[labelKopiaManagedBy] != "dr-syncer" {
		t.Errorf("expected managed-by label dr-syncer, got %s", labels[labelKopiaManagedBy])
	}
}

func TestBuildAnnotations(t *testing.T) {
	op := newTestOperation(drv1alpha1.OperationTypeBackup, "")
	annotations := buildAnnotations(op)

	if annotations[annotationKopiaOperation] != op.Name {
		t.Errorf("expected operation annotation %s, got %s", op.Name, annotations[annotationKopiaOperation])
	}
	if annotations[annotationKopiaPVC] != "test-pvc" {
		t.Errorf("expected PVC annotation test-pvc, got %s", annotations[annotationKopiaPVC])
	}
	if _, ok := annotations[annotationKopiaCreatedAt]; !ok {
		t.Error("expected created-at annotation")
	}
}

// --- helpers ---

func verifyVolume(t *testing.T, vol corev1.Volume, expectedName, expectedClaimName string) {
	t.Helper()
	if vol.Name != expectedName {
		t.Errorf("expected volume name %s, got %s", expectedName, vol.Name)
	}
	if vol.PersistentVolumeClaim == nil {
		t.Fatalf("expected PVC volume source for %s", expectedName)
	}
	if vol.PersistentVolumeClaim.ClaimName != expectedClaimName {
		t.Errorf("expected claim name %s, got %s", expectedClaimName, vol.PersistentVolumeClaim.ClaimName)
	}
}

func verifyEmptyDirVolume(t *testing.T, vol corev1.Volume, expectedName string) {
	t.Helper()
	if vol.Name != expectedName {
		t.Errorf("expected volume name %s, got %s", expectedName, vol.Name)
	}
	if vol.EmptyDir == nil {
		t.Fatalf("expected EmptyDir volume source for %s", expectedName)
	}
}

func verifyVolumeMount(t *testing.T, vm corev1.VolumeMount, expectedName, expectedPath string) {
	t.Helper()
	if vm.Name != expectedName {
		t.Errorf("expected mount name %s, got %s", expectedName, vm.Name)
	}
	if vm.MountPath != expectedPath {
		t.Errorf("expected mount path %s, got %s", expectedPath, vm.MountPath)
	}
}

func verifySecretEnvVar(t *testing.T, envs []corev1.EnvVar, name, secretName, key string) {
	t.Helper()
	for _, env := range envs {
		if env.Name == name {
			if env.ValueFrom == nil || env.ValueFrom.SecretKeyRef == nil {
				t.Errorf("env %s: expected SecretKeyRef, got plain value", name)
				return
			}
			if env.ValueFrom.SecretKeyRef.Name != secretName {
				t.Errorf("env %s: expected secret %s, got %s", name, secretName, env.ValueFrom.SecretKeyRef.Name)
			}
			if env.ValueFrom.SecretKeyRef.Key != key {
				t.Errorf("env %s: expected key %s, got %s", name, key, env.ValueFrom.SecretKeyRef.Key)
			}
			return
		}
	}
	t.Errorf("env var %s not found", name)
}

func verifyPlainEnvVar(t *testing.T, envs []corev1.EnvVar, name, expected string) {
	t.Helper()
	for _, env := range envs {
		if env.Name == name {
			if env.Value != expected {
				t.Errorf("env %s: expected %s, got %s", name, expected, env.Value)
			}
			return
		}
	}
	t.Errorf("env var %s not found", name)
}
