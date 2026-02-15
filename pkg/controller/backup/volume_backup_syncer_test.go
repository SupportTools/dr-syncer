package backup

import (
	"context"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
)

// testScheme returns a scheme with all required types registered.
func testScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = drv1alpha1.AddToScheme(s)
	return s
}

func newTestMapping(name, srcNs, dstNs string, backupEnabled bool) *drv1alpha1.NamespaceMapping {
	mapping := &drv1alpha1.NamespaceMapping{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "dr-syncer-system",
		},
		Spec: drv1alpha1.NamespaceMappingSpec{
			SourceNamespace:      srcNs,
			DestinationNamespace: dstNs,
			PVCConfig: &drv1alpha1.PVCConfig{
				SyncData: true,
				DataSyncConfig: &drv1alpha1.PVCDataSyncConfig{
					BackupConfig: &drv1alpha1.BackupConfig{
						Enabled: backupEnabled,
						BackupRepositoryRef: &drv1alpha1.SecretReference{
							Name:      "test-repo",
							Namespace: "dr-syncer-system",
						},
						DataAccessStrategy: drv1alpha1.DataAccessStrategyAuto,
					},
				},
			},
		},
	}
	return mapping
}

func newTestRepo(name, namespace string, state drv1alpha1.BackupRepositoryState) *drv1alpha1.BackupRepository {
	return &drv1alpha1.BackupRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: drv1alpha1.BackupRepositorySpec{
			S3Config: drv1alpha1.S3Config{
				Endpoint: "s3.example.com",
				Bucket:   "test-bucket",
				CredentialsSecretRef: drv1alpha1.SecretReference{
					Name:      "s3-creds",
					Namespace: namespace,
				},
			},
			KopiaConfig: drv1alpha1.KopiaConfig{
				EncryptionSecretRef: drv1alpha1.SecretReference{
					Name:      "kopia-password",
					Namespace: namespace,
				},
			},
		},
		Status: drv1alpha1.BackupRepositoryStatus{
			State: state,
		},
	}
}

func newTestPVC(name, namespace string) corev1.PersistentVolumeClaim {
	return corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		},
	}
}

func TestNewVolumeBackupSyncer(t *testing.T) {
	scheme := testScheme()
	srcClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	dstClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	syncer := NewVolumeBackupSyncer(srcClient, dstClient)
	if syncer == nil {
		t.Fatal("expected non-nil syncer")
	}
	if syncer.SourceClient != srcClient {
		t.Error("source client not set correctly")
	}
	if syncer.DestinationClient != dstClient {
		t.Error("destination client not set correctly")
	}
	if syncer.Log == nil {
		t.Error("expected non-nil logger")
	}
}

func TestSyncPVCsWithBackup_DisabledConfig(t *testing.T) {
	scheme := testScheme()
	srcClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	dstClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	syncer := NewVolumeBackupSyncer(srcClient, dstClient)

	mapping := newTestMapping("test-mapping", "src-ns", "dst-ns", false)
	pvcs := []corev1.PersistentVolumeClaim{newTestPVC("pvc-1", "src-ns")}

	// Disabled config returns nil.
	results := syncer.SyncPVCsWithBackup(context.Background(), mapping, pvcs, &drv1alpha1.BackupConfig{Enabled: false})
	if results != nil {
		t.Errorf("expected nil results for disabled config, got %v", results)
	}
}

func TestSyncPVCsWithBackup_NilBackupConfig(t *testing.T) {
	scheme := testScheme()
	srcClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	dstClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	syncer := NewVolumeBackupSyncer(srcClient, dstClient)

	mapping := newTestMapping("test-mapping", "src-ns", "dst-ns", true)
	pvcs := []corev1.PersistentVolumeClaim{newTestPVC("pvc-1", "src-ns")}

	results := syncer.SyncPVCsWithBackup(context.Background(), mapping, pvcs, nil)
	if results != nil {
		t.Errorf("expected nil results for nil config, got %v", results)
	}
}

func TestSyncPVCsWithBackup_NilRepoRef(t *testing.T) {
	scheme := testScheme()
	mapping := newTestMapping("test-mapping", "src-ns", "dst-ns", true)
	// Install the mapping in the destination client for annotation updates.
	dstClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mapping).Build()
	srcClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	syncer := NewVolumeBackupSyncer(srcClient, dstClient)

	pvcs := []corev1.PersistentVolumeClaim{newTestPVC("pvc-1", "src-ns")}

	results := syncer.SyncPVCsWithBackup(context.Background(), mapping, pvcs, &drv1alpha1.BackupConfig{
		Enabled:             true,
		BackupRepositoryRef: nil,
	})
	if results != nil {
		t.Errorf("expected nil results for nil repo ref, got %v", results)
	}
}

func TestSyncPVCsWithBackup_RepoNotReady(t *testing.T) {
	scheme := testScheme()
	repo := newTestRepo("test-repo", "dr-syncer-system", drv1alpha1.BackupRepositoryStateError)
	mapping := newTestMapping("test-mapping", "src-ns", "dst-ns", true)

	srcClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(repo).Build()
	dstClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mapping).Build()
	syncer := NewVolumeBackupSyncer(srcClient, dstClient)

	pvcs := []corev1.PersistentVolumeClaim{newTestPVC("pvc-1", "src-ns")}
	backupConfig := mapping.Spec.PVCConfig.DataSyncConfig.BackupConfig

	results := syncer.SyncPVCsWithBackup(context.Background(), mapping, pvcs, backupConfig)
	if results != nil {
		t.Errorf("expected nil results when repo not ready, got %v", results)
	}

	// Verify annotations were set to Failed.
	var updated drv1alpha1.NamespaceMapping
	if err := dstClient.Get(context.Background(), client.ObjectKey{
		Name:      mapping.Name,
		Namespace: mapping.Namespace,
	}, &updated); err != nil {
		t.Fatalf("failed to get updated mapping: %v", err)
	}

	if status := updated.Annotations[annotationLastBackupSyncStatus]; status != "Failed" {
		t.Errorf("expected status annotation 'Failed', got %q", status)
	}
}

func TestSyncPVCsWithBackup_RepoNotFound(t *testing.T) {
	scheme := testScheme()
	mapping := newTestMapping("test-mapping", "src-ns", "dst-ns", true)

	srcClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	dstClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mapping).Build()
	syncer := NewVolumeBackupSyncer(srcClient, dstClient)

	pvcs := []corev1.PersistentVolumeClaim{newTestPVC("pvc-1", "src-ns")}
	backupConfig := mapping.Spec.PVCConfig.DataSyncConfig.BackupConfig

	results := syncer.SyncPVCsWithBackup(context.Background(), mapping, pvcs, backupConfig)
	if results != nil {
		t.Errorf("expected nil results when repo not found, got %v", results)
	}
}

func TestBuildBackupOperation(t *testing.T) {
	syncer := NewVolumeBackupSyncer(nil, nil)
	mapping := newTestMapping("my-mapping", "src-ns", "dst-ns", true)
	pvc := newTestPVC("data-pvc", "src-ns")
	repo := newTestRepo("test-repo", "dr-syncer-system", drv1alpha1.BackupRepositoryStateReady)
	backupConfig := mapping.Spec.PVCConfig.DataSyncConfig.BackupConfig

	op := syncer.buildBackupOperation(mapping, &pvc, repo, backupConfig)

	if op.Name != "my-mapping-data-pvc-backup" {
		t.Errorf("unexpected operation name: %s", op.Name)
	}
	if op.Namespace != "src-ns" {
		t.Errorf("unexpected namespace: %s", op.Namespace)
	}
	if op.Spec.OperationType != drv1alpha1.OperationTypeBackup {
		t.Errorf("unexpected operation type: %s", op.Spec.OperationType)
	}
	if op.Spec.SourcePVC.Name != "data-pvc" {
		t.Errorf("unexpected source PVC: %s", op.Spec.SourcePVC.Name)
	}
	if op.Spec.BackupRepositoryRef.Name != "test-repo" {
		t.Errorf("unexpected repo ref: %s", op.Spec.BackupRepositoryRef.Name)
	}
	if op.Labels[labelMapping] != "my-mapping" {
		t.Errorf("unexpected mapping label: %s", op.Labels[labelMapping])
	}
	if op.Labels[labelOperation] != "backup" {
		t.Errorf("unexpected operation label: %s", op.Labels[labelOperation])
	}
}

func TestBuildRestoreOperation(t *testing.T) {
	syncer := NewVolumeBackupSyncer(nil, nil)
	mapping := newTestMapping("my-mapping", "src-ns", "dst-ns", true)
	repo := newTestRepo("test-repo", "dr-syncer-system", drv1alpha1.BackupRepositoryStateReady)
	backupConfig := mapping.Spec.PVCConfig.DataSyncConfig.BackupConfig

	destPVC := &drv1alpha1.PVCReference{Namespace: "dst-ns", Name: "data-pvc"}

	op := syncer.buildRestoreOperation(mapping, destPVC, repo, backupConfig, "snap-123")

	if op.Name != "my-mapping-data-pvc-restore" {
		t.Errorf("unexpected operation name: %s", op.Name)
	}
	if op.Namespace != "dst-ns" {
		t.Errorf("unexpected namespace: %s", op.Namespace)
	}
	if op.Spec.OperationType != drv1alpha1.OperationTypeRestore {
		t.Errorf("unexpected operation type: %s", op.Spec.OperationType)
	}
	if op.Spec.SnapshotID != "snap-123" {
		t.Errorf("unexpected snapshot ID: %s", op.Spec.SnapshotID)
	}
	if op.Spec.DestinationPVC == nil || op.Spec.DestinationPVC.Name != "data-pvc" {
		t.Error("unexpected destination PVC")
	}
	if op.Labels[labelOperation] != "restore" {
		t.Errorf("unexpected operation label: %s", op.Labels[labelOperation])
	}
}

func TestCreateOrUpdateOperation_Create(t *testing.T) {
	scheme := testScheme()
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	syncer := NewVolumeBackupSyncer(k8sClient, nil)

	op := &drv1alpha1.VolumeBackupOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-op",
			Namespace: "test-ns",
			Labels:    map[string]string{"test": "value"},
		},
		Spec: drv1alpha1.VolumeBackupOperationSpec{
			OperationType: drv1alpha1.OperationTypeBackup,
			SourcePVC:     drv1alpha1.PVCReference{Namespace: "test-ns", Name: "pvc-1"},
			BackupRepositoryRef: drv1alpha1.SecretReference{
				Name:      "repo",
				Namespace: "test-ns",
			},
		},
	}

	if err := syncer.createOrUpdateOperation(context.Background(), k8sClient, op); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Verify it was created.
	created := &drv1alpha1.VolumeBackupOperation{}
	if err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(op), created); err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if created.Spec.OperationType != drv1alpha1.OperationTypeBackup {
		t.Error("operation type mismatch")
	}
}

func TestCreateOrUpdateOperation_Update(t *testing.T) {
	scheme := testScheme()

	existing := &drv1alpha1.VolumeBackupOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-op",
			Namespace: "test-ns",
			Labels:    map[string]string{"old": "label"},
		},
		Spec: drv1alpha1.VolumeBackupOperationSpec{
			OperationType: drv1alpha1.OperationTypeBackup,
			SourcePVC:     drv1alpha1.PVCReference{Namespace: "test-ns", Name: "pvc-1"},
			BackupRepositoryRef: drv1alpha1.SecretReference{
				Name:      "old-repo",
				Namespace: "test-ns",
			},
		},
	}

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	syncer := NewVolumeBackupSyncer(k8sClient, nil)

	updated := &drv1alpha1.VolumeBackupOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-op",
			Namespace: "test-ns",
			Labels:    map[string]string{"new": "label"},
		},
		Spec: drv1alpha1.VolumeBackupOperationSpec{
			OperationType: drv1alpha1.OperationTypeRestore,
			SourcePVC:     drv1alpha1.PVCReference{Namespace: "test-ns", Name: "pvc-1"},
			BackupRepositoryRef: drv1alpha1.SecretReference{
				Name:      "new-repo",
				Namespace: "test-ns",
			},
			SnapshotID: "snap-456",
		},
	}

	if err := syncer.createOrUpdateOperation(context.Background(), k8sClient, updated); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	// Verify spec and labels were updated.
	result := &drv1alpha1.VolumeBackupOperation{}
	if err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(updated), result); err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if result.Spec.OperationType != drv1alpha1.OperationTypeRestore {
		t.Error("operation type should have been updated")
	}
	if result.Spec.BackupRepositoryRef.Name != "new-repo" {
		t.Error("repo ref should have been updated")
	}
	if result.Labels["new"] != "label" {
		t.Error("labels should have been updated")
	}
}

func TestWaitForOperationPhase_Success(t *testing.T) {
	scheme := testScheme()

	op := &drv1alpha1.VolumeBackupOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-op",
			Namespace: "test-ns",
		},
		Spec: drv1alpha1.VolumeBackupOperationSpec{
			OperationType: drv1alpha1.OperationTypeBackup,
			SourcePVC:     drv1alpha1.PVCReference{Namespace: "test-ns", Name: "pvc-1"},
			BackupRepositoryRef: drv1alpha1.SecretReference{
				Name:      "repo",
				Namespace: "test-ns",
			},
		},
		Status: drv1alpha1.VolumeBackupOperationStatus{
			Phase:           drv1alpha1.VolumeBackupPhaseBackupComplete,
			KopiaSnapshotID: "snap-789",
		},
	}

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(op).Build()
	syncer := NewVolumeBackupSyncer(k8sClient, nil)
	syncer.PollInterval = 50 * time.Millisecond

	snapshotID, err := syncer.waitForOperationPhase(
		context.Background(), k8sClient, op,
		drv1alpha1.VolumeBackupPhaseBackupComplete, 5*time.Second,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snapshotID != "snap-789" {
		t.Errorf("expected snapshot ID 'snap-789', got %q", snapshotID)
	}
}

func TestWaitForOperationPhase_Failed(t *testing.T) {
	scheme := testScheme()

	op := &drv1alpha1.VolumeBackupOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-op",
			Namespace: "test-ns",
		},
		Spec: drv1alpha1.VolumeBackupOperationSpec{
			OperationType: drv1alpha1.OperationTypeBackup,
			SourcePVC:     drv1alpha1.PVCReference{Namespace: "test-ns", Name: "pvc-1"},
			BackupRepositoryRef: drv1alpha1.SecretReference{
				Name:      "repo",
				Namespace: "test-ns",
			},
		},
		Status: drv1alpha1.VolumeBackupOperationStatus{
			Phase:        drv1alpha1.VolumeBackupPhaseFailed,
			ErrorMessage: "kopia snapshot failed: disk full",
		},
	}

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(op).Build()
	syncer := NewVolumeBackupSyncer(k8sClient, nil)
	syncer.PollInterval = 50 * time.Millisecond

	_, err := syncer.waitForOperationPhase(
		context.Background(), k8sClient, op,
		drv1alpha1.VolumeBackupPhaseBackupComplete, 5*time.Second,
	)
	if err == nil {
		t.Fatal("expected error for failed operation")
	}
	if err.Error() != "operation failed: kopia snapshot failed: disk full" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestWaitForOperationPhase_Timeout(t *testing.T) {
	scheme := testScheme()

	// Operation stays in Pending phase — should timeout.
	op := &drv1alpha1.VolumeBackupOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-op",
			Namespace: "test-ns",
		},
		Spec: drv1alpha1.VolumeBackupOperationSpec{
			OperationType: drv1alpha1.OperationTypeBackup,
			SourcePVC:     drv1alpha1.PVCReference{Namespace: "test-ns", Name: "pvc-1"},
			BackupRepositoryRef: drv1alpha1.SecretReference{
				Name:      "repo",
				Namespace: "test-ns",
			},
		},
		Status: drv1alpha1.VolumeBackupOperationStatus{
			Phase: drv1alpha1.VolumeBackupPhasePending,
		},
	}

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(op).Build()
	syncer := NewVolumeBackupSyncer(k8sClient, nil)

	// Timeout fires before poll interval.
	_, err := syncer.waitForOperationPhase(
		context.Background(), k8sClient, op,
		drv1alpha1.VolumeBackupPhaseBackupComplete, 100*time.Millisecond,
	)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestWaitForOperationPhase_ContextCancelled(t *testing.T) {
	scheme := testScheme()

	op := &drv1alpha1.VolumeBackupOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-op",
			Namespace: "test-ns",
		},
		Spec: drv1alpha1.VolumeBackupOperationSpec{
			OperationType: drv1alpha1.OperationTypeBackup,
			SourcePVC:     drv1alpha1.PVCReference{Namespace: "test-ns", Name: "pvc-1"},
			BackupRepositoryRef: drv1alpha1.SecretReference{
				Name:      "repo",
				Namespace: "test-ns",
			},
		},
		Status: drv1alpha1.VolumeBackupOperationStatus{
			Phase: drv1alpha1.VolumeBackupPhasePending,
		},
	}

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(op).Build()
	syncer := NewVolumeBackupSyncer(k8sClient, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	_, err := syncer.waitForOperationPhase(
		ctx, k8sClient, op,
		drv1alpha1.VolumeBackupPhaseBackupComplete, 5*time.Second,
	)
	if err == nil {
		t.Fatal("expected context cancelled error")
	}
}

func TestWaitForOperationPhase_Deleted(t *testing.T) {
	scheme := testScheme()

	// Object doesn't exist in client — simulates deletion.
	op := &drv1alpha1.VolumeBackupOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "deleted-op",
			Namespace: "test-ns",
		},
		Spec: drv1alpha1.VolumeBackupOperationSpec{
			OperationType: drv1alpha1.OperationTypeBackup,
			SourcePVC:     drv1alpha1.PVCReference{Namespace: "test-ns", Name: "pvc-1"},
			BackupRepositoryRef: drv1alpha1.SecretReference{
				Name:      "repo",
				Namespace: "test-ns",
			},
		},
	}

	// Empty client — op doesn't exist.
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	syncer := NewVolumeBackupSyncer(k8sClient, nil)
	syncer.PollInterval = 50 * time.Millisecond

	// NotFound should be detected on first poll.
	_, err := syncer.waitForOperationPhase(
		context.Background(), k8sClient, op,
		drv1alpha1.VolumeBackupPhaseBackupComplete, 5*time.Second,
	)
	if err == nil {
		t.Fatal("expected error for deleted operation")
	}
	if err.Error() != "operation test-ns/deleted-op was deleted" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGetBackupRepository(t *testing.T) {
	scheme := testScheme()
	repo := newTestRepo("my-repo", "dr-syncer-system", drv1alpha1.BackupRepositoryStateReady)
	srcClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(repo).Build()
	syncer := NewVolumeBackupSyncer(srcClient, nil)

	ref := &drv1alpha1.SecretReference{Name: "my-repo", Namespace: "dr-syncer-system"}

	got, err := syncer.getBackupRepository(context.Background(), ref, "default-ns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "my-repo" {
		t.Errorf("expected repo name 'my-repo', got %q", got.Name)
	}
}

func TestGetBackupRepository_DefaultNamespace(t *testing.T) {
	scheme := testScheme()
	repo := newTestRepo("my-repo", "default-ns", drv1alpha1.BackupRepositoryStateReady)
	srcClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(repo).Build()
	syncer := NewVolumeBackupSyncer(srcClient, nil)

	// Empty namespace in ref should use default.
	ref := &drv1alpha1.SecretReference{Name: "my-repo", Namespace: ""}

	got, err := syncer.getBackupRepository(context.Background(), ref, "default-ns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "my-repo" {
		t.Errorf("expected repo name 'my-repo', got %q", got.Name)
	}
}

func TestGetBackupRepository_NotFound(t *testing.T) {
	scheme := testScheme()
	srcClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	syncer := NewVolumeBackupSyncer(srcClient, nil)

	ref := &drv1alpha1.SecretReference{Name: "missing-repo", Namespace: "test-ns"}

	_, err := syncer.getBackupRepository(context.Background(), ref, "test-ns")
	if err == nil {
		t.Fatal("expected error for missing repo")
	}
}

func TestUpdateMappingAnnotations_Success(t *testing.T) {
	scheme := testScheme()
	mapping := newTestMapping("test-mapping", "src-ns", "dst-ns", true)
	dstClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mapping).Build()
	syncer := NewVolumeBackupSyncer(nil, dstClient)

	syncer.updateMappingAnnotations(context.Background(), mapping, "Completed", nil)

	var updated drv1alpha1.NamespaceMapping
	if err := dstClient.Get(context.Background(), client.ObjectKey{
		Name:      mapping.Name,
		Namespace: mapping.Namespace,
	}, &updated); err != nil {
		t.Fatalf("get failed: %v", err)
	}

	if status := updated.Annotations[annotationLastBackupSyncStatus]; status != "Completed" {
		t.Errorf("expected status 'Completed', got %q", status)
	}
	if _, exists := updated.Annotations[annotationLastBackupSyncTime]; !exists {
		t.Error("expected sync time annotation to be set")
	}
	if _, exists := updated.Annotations[annotationLastBackupSyncError]; exists {
		t.Error("expected no error annotation on success")
	}
}

func TestUpdateMappingAnnotations_WithError(t *testing.T) {
	scheme := testScheme()
	mapping := newTestMapping("test-mapping", "src-ns", "dst-ns", true)
	dstClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mapping).Build()
	syncer := NewVolumeBackupSyncer(nil, dstClient)

	syncer.updateMappingAnnotations(context.Background(), mapping, "Failed", fmt.Errorf("backup failed"))

	var updated drv1alpha1.NamespaceMapping
	_ = dstClient.Get(context.Background(), client.ObjectKey{
		Name:      mapping.Name,
		Namespace: mapping.Namespace,
	}, &updated)

	if status := updated.Annotations[annotationLastBackupSyncStatus]; status != "Failed" {
		t.Errorf("expected status 'Failed', got %q", status)
	}
	if errMsg := updated.Annotations[annotationLastBackupSyncError]; errMsg != "backup failed" {
		t.Errorf("expected error annotation 'backup failed', got %q", errMsg)
	}
}

func TestUpdateMappingAnnotations_ClearsError(t *testing.T) {
	scheme := testScheme()
	mapping := newTestMapping("test-mapping", "src-ns", "dst-ns", true)
	mapping.Annotations = map[string]string{
		annotationLastBackupSyncError: "old error",
	}
	dstClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mapping).Build()
	syncer := NewVolumeBackupSyncer(nil, dstClient)

	// A successful update should clear the error annotation.
	syncer.updateMappingAnnotations(context.Background(), mapping, "Completed", nil)

	var updated drv1alpha1.NamespaceMapping
	_ = dstClient.Get(context.Background(), client.ObjectKey{
		Name:      mapping.Name,
		Namespace: mapping.Namespace,
	}, &updated)

	if _, exists := updated.Annotations[annotationLastBackupSyncError]; exists {
		t.Error("expected error annotation to be cleared on success")
	}
}

// --- Standby PVC wiring tests ---
// These tests verify that SyncPVCsWithBackup correctly constructs (or skips)
// a StandbyPVCManager based on BackupConfig. Since StandbyPVCMgr is now a
// local variable, we verify behavior via the sync results and restore operation
// targeting standby PVCs.

func TestSyncPVCsWithBackup_StandbyPVCManagerConstructed(t *testing.T) {
	scheme := testScheme()
	repo := newTestRepo("test-repo", "dr-syncer-system", drv1alpha1.BackupRepositoryStateReady)
	mapping := newTestMapping("test-mapping", "src-ns", "dst-ns", true)

	srcClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(repo).Build()
	dstClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mapping).Build()
	syncer := NewVolumeBackupSyncer(srcClient, dstClient)
	syncer.PollInterval = 10 * time.Millisecond

	// StandbyPVCConfig nil means standby defaults to enabled.
	backupConfig := mapping.Spec.PVCConfig.DataSyncConfig.BackupConfig
	backupConfig.StandbyPVCConfig = nil

	// Use a short-lived context so the poll loop exits quickly.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// This will fail at the sync step (context timeout during polling),
	// but the fact that it proceeds to sync indicates the standby manager was constructed.
	results := syncer.SyncPVCsWithBackup(ctx, mapping,
		[]corev1.PersistentVolumeClaim{newTestPVC("pvc-1", "src-ns")}, backupConfig)

	// We expect results (even if errored) because the code proceeds past manager construction.
	if len(results) == 0 {
		t.Error("expected results indicating sync was attempted (standby manager should be constructed)")
	}
}

func TestSyncPVCsWithBackup_StandbyPVCManagerDisabled(t *testing.T) {
	scheme := testScheme()
	repo := newTestRepo("test-repo", "dr-syncer-system", drv1alpha1.BackupRepositoryStateReady)
	mapping := newTestMapping("test-mapping", "src-ns", "dst-ns", true)

	srcClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(repo).Build()
	dstClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mapping).Build()
	syncer := NewVolumeBackupSyncer(srcClient, dstClient)
	syncer.PollInterval = 10 * time.Millisecond

	disabled := false
	backupConfig := mapping.Spec.PVCConfig.DataSyncConfig.BackupConfig
	backupConfig.StandbyPVCConfig = &drv1alpha1.StandbyPVCConfig{
		Enabled: &disabled,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// When standby is disabled, sync still proceeds but without standby PVC management.
	results := syncer.SyncPVCsWithBackup(ctx, mapping,
		[]corev1.PersistentVolumeClaim{newTestPVC("pvc-1", "src-ns")}, backupConfig)

	// Results should still be returned (sync attempted without standby manager).
	if len(results) == 0 {
		t.Error("expected results indicating sync was attempted")
	}
}

func TestSyncPVCsWithBackup_StandbyPVCManagerEnabledExplicitly(t *testing.T) {
	scheme := testScheme()
	repo := newTestRepo("test-repo", "dr-syncer-system", drv1alpha1.BackupRepositoryStateReady)
	mapping := newTestMapping("test-mapping", "src-ns", "dst-ns", true)

	srcClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(repo).Build()
	dstClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mapping).Build()
	syncer := NewVolumeBackupSyncer(srcClient, dstClient)
	syncer.PollInterval = 10 * time.Millisecond

	enabled := true
	backupConfig := mapping.Spec.PVCConfig.DataSyncConfig.BackupConfig
	backupConfig.StandbyPVCConfig = &drv1alpha1.StandbyPVCConfig{
		Enabled:          &enabled,
		StorageClassName: "fast-storage",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	results := syncer.SyncPVCsWithBackup(ctx, mapping,
		[]corev1.PersistentVolumeClaim{newTestPVC("pvc-1", "src-ns")}, backupConfig)

	// Results should still be returned (sync attempted with standby manager).
	if len(results) == 0 {
		t.Error("expected results indicating sync was attempted (standby manager constructed)")
	}
}

// Note: mockStandbyPVCManager is defined in restore_workflow_test.go (same package).
// It uses ensureFunc/updateFunc function fields.

func TestSyncSinglePVC_WithStandbyPVCManager(t *testing.T) {
	scheme := testScheme()

	backupOp := &drv1alpha1.VolumeBackupOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-mapping-data-pvc-backup",
			Namespace: "src-ns",
		},
		Spec: drv1alpha1.VolumeBackupOperationSpec{
			OperationType: drv1alpha1.OperationTypeBackup,
			SourcePVC:     drv1alpha1.PVCReference{Namespace: "src-ns", Name: "data-pvc"},
			BackupRepositoryRef: drv1alpha1.SecretReference{
				Name: "test-repo", Namespace: "dr-syncer-system",
			},
		},
		Status: drv1alpha1.VolumeBackupOperationStatus{
			Phase:           drv1alpha1.VolumeBackupPhaseBackupComplete,
			KopiaSnapshotID: "snap-abc123",
		},
	}

	restoreOp := &drv1alpha1.VolumeBackupOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-mapping-data-pvc-standby-restore",
			Namespace: "dst-ns",
		},
		Spec: drv1alpha1.VolumeBackupOperationSpec{
			OperationType: drv1alpha1.OperationTypeRestore,
			SourcePVC:     drv1alpha1.PVCReference{Namespace: "dst-ns", Name: "data-pvc-standby"},
			BackupRepositoryRef: drv1alpha1.SecretReference{
				Name: "test-repo", Namespace: "dr-syncer-system",
			},
			SnapshotID: "snap-abc123",
		},
		Status: drv1alpha1.VolumeBackupOperationStatus{
			Phase:           drv1alpha1.VolumeBackupPhaseRestoreComplete,
			KopiaSnapshotID: "snap-abc123",
		},
	}

	srcClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(backupOp).Build()
	dstClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(restoreOp).Build()

	var ensureCalled, updateCalled bool
	var lastUpdatePVCName, lastUpdateSnapID string

	mockMgr := &mockStandbyPVCManager{
		ensureFunc: func(ctx context.Context, sourcePVC drv1alpha1.PVCReference, destNamespace string, spec *corev1.PersistentVolumeClaimSpec) (*drv1alpha1.PVCReference, bool, error) {
			ensureCalled = true
			return &drv1alpha1.PVCReference{Namespace: "dst-ns", Name: "data-pvc-standby"}, true, nil
		},
		updateFunc: func(ctx context.Context, pvcRef drv1alpha1.PVCReference, snapshotID string) error {
			updateCalled = true
			lastUpdatePVCName = pvcRef.Name
			lastUpdateSnapID = snapshotID
			return nil
		},
	}

	syncer := NewVolumeBackupSyncer(srcClient, dstClient)
	syncer.PollInterval = 50 * time.Millisecond

	mapping := newTestMapping("test-mapping", "src-ns", "dst-ns", true)
	repo := newTestRepo("test-repo", "dr-syncer-system", drv1alpha1.BackupRepositoryStateReady)
	pvc := newTestPVC("data-pvc", "src-ns")

	result := syncer.syncSinglePVC(context.Background(), mapping, &pvc,
		mapping.Spec.PVCConfig.DataSyncConfig.BackupConfig, repo, mockMgr)

	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if result.SnapshotID != "snap-abc123" {
		t.Errorf("expected snapshot ID 'snap-abc123', got %q", result.SnapshotID)
	}
	if !ensureCalled {
		t.Error("expected EnsureStandbyPVC to be called")
	}
	if !updateCalled {
		t.Error("expected UpdateStandbyPVCStatus to be called")
	}
	if lastUpdatePVCName != "data-pvc-standby" {
		t.Errorf("expected update on 'data-pvc-standby', got %q", lastUpdatePVCName)
	}
	if lastUpdateSnapID != "snap-abc123" {
		t.Errorf("expected snapshot ID 'snap-abc123' in update, got %q", lastUpdateSnapID)
	}
}

func TestSyncSinglePVC_WithoutStandbyPVCManager(t *testing.T) {
	scheme := testScheme()

	// Backup op already complete.
	backupOp := &drv1alpha1.VolumeBackupOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-mapping-data-pvc-backup",
			Namespace: "src-ns",
		},
		Spec: drv1alpha1.VolumeBackupOperationSpec{
			OperationType: drv1alpha1.OperationTypeBackup,
			SourcePVC:     drv1alpha1.PVCReference{Namespace: "src-ns", Name: "data-pvc"},
			BackupRepositoryRef: drv1alpha1.SecretReference{
				Name:      "test-repo",
				Namespace: "dr-syncer-system",
			},
		},
		Status: drv1alpha1.VolumeBackupOperationStatus{
			Phase:           drv1alpha1.VolumeBackupPhaseBackupComplete,
			KopiaSnapshotID: "snap-def456",
		},
	}

	// Restore op targets direct PVC (not standby) and is complete.
	restoreOp := &drv1alpha1.VolumeBackupOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-mapping-data-pvc-restore",
			Namespace: "dst-ns",
		},
		Spec: drv1alpha1.VolumeBackupOperationSpec{
			OperationType: drv1alpha1.OperationTypeRestore,
			SourcePVC:     drv1alpha1.PVCReference{Namespace: "dst-ns", Name: "data-pvc"},
			BackupRepositoryRef: drv1alpha1.SecretReference{
				Name:      "test-repo",
				Namespace: "dr-syncer-system",
			},
			SnapshotID: "snap-def456",
		},
		Status: drv1alpha1.VolumeBackupOperationStatus{
			Phase:           drv1alpha1.VolumeBackupPhaseRestoreComplete,
			KopiaSnapshotID: "snap-def456",
		},
	}

	srcClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(backupOp).Build()
	dstClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(restoreOp).Build()

	syncer := NewVolumeBackupSyncer(srcClient, dstClient)
	syncer.PollInterval = 50 * time.Millisecond

	mapping := newTestMapping("test-mapping", "src-ns", "dst-ns", true)
	repo := newTestRepo("test-repo", "dr-syncer-system", drv1alpha1.BackupRepositoryStateReady)
	pvc := newTestPVC("data-pvc", "src-ns")

	result := syncer.syncSinglePVC(context.Background(), mapping, &pvc,
		mapping.Spec.PVCConfig.DataSyncConfig.BackupConfig, repo, nil)

	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if result.SnapshotID != "snap-def456" {
		t.Errorf("expected snapshot ID 'snap-def456', got %q", result.SnapshotID)
	}

	// Verify restore targets direct PVC name (not standby).
	var createdRestore drv1alpha1.VolumeBackupOperation
	err := dstClient.Get(context.Background(), client.ObjectKey{
		Name:      "test-mapping-data-pvc-restore",
		Namespace: "dst-ns",
	}, &createdRestore)
	if err != nil {
		t.Fatalf("expected restore operation to exist: %v", err)
	}
	if createdRestore.Spec.DestinationPVC == nil || createdRestore.Spec.DestinationPVC.Name != "data-pvc" {
		t.Error("expected restore to target direct PVC 'data-pvc', not standby")
	}
}

func TestSyncSinglePVC_StandbyPVCEnsureError(t *testing.T) {
	scheme := testScheme()

	backupOp := &drv1alpha1.VolumeBackupOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-mapping-data-pvc-backup",
			Namespace: "src-ns",
		},
		Spec: drv1alpha1.VolumeBackupOperationSpec{
			OperationType: drv1alpha1.OperationTypeBackup,
			SourcePVC:     drv1alpha1.PVCReference{Namespace: "src-ns", Name: "data-pvc"},
			BackupRepositoryRef: drv1alpha1.SecretReference{
				Name:      "test-repo",
				Namespace: "dr-syncer-system",
			},
		},
		Status: drv1alpha1.VolumeBackupOperationStatus{
			Phase:           drv1alpha1.VolumeBackupPhaseBackupComplete,
			KopiaSnapshotID: "snap-ghi789",
		},
	}

	srcClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(backupOp).Build()
	dstClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	ensureCalled := false
	updateCalled := false
	mockMgr := &mockStandbyPVCManager{
		ensureFunc: func(ctx context.Context, sourcePVC drv1alpha1.PVCReference, destNamespace string, spec *corev1.PersistentVolumeClaimSpec) (*drv1alpha1.PVCReference, bool, error) {
			ensureCalled = true
			return nil, false, fmt.Errorf("storage class not found")
		},
		updateFunc: func(ctx context.Context, pvcRef drv1alpha1.PVCReference, snapshotID string) error {
			updateCalled = true
			return nil
		},
	}

	syncer := NewVolumeBackupSyncer(srcClient, dstClient)
	syncer.PollInterval = 50 * time.Millisecond

	mapping := newTestMapping("test-mapping", "src-ns", "dst-ns", true)
	repo := newTestRepo("test-repo", "dr-syncer-system", drv1alpha1.BackupRepositoryStateReady)
	pvc := newTestPVC("data-pvc", "src-ns")

	result := syncer.syncSinglePVC(context.Background(), mapping, &pvc,
		mapping.Spec.PVCConfig.DataSyncConfig.BackupConfig, repo, mockMgr)

	if result.Err == nil {
		t.Fatal("expected error when EnsureStandbyPVC fails")
	}
	if result.SnapshotID != "snap-ghi789" {
		t.Errorf("expected snapshot ID preserved even on standby error, got %q", result.SnapshotID)
	}
	if !ensureCalled {
		t.Error("expected EnsureStandbyPVC to be called")
	}
	if updateCalled {
		t.Error("expected UpdateStandbyPVCStatus NOT to be called on ensure failure")
	}
}

func TestSyncSinglePVC_StandbyPVCUpdateError_NonFatal(t *testing.T) {
	scheme := testScheme()

	backupOp := &drv1alpha1.VolumeBackupOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-mapping-data-pvc-backup",
			Namespace: "src-ns",
		},
		Spec: drv1alpha1.VolumeBackupOperationSpec{
			OperationType: drv1alpha1.OperationTypeBackup,
			SourcePVC:     drv1alpha1.PVCReference{Namespace: "src-ns", Name: "data-pvc"},
			BackupRepositoryRef: drv1alpha1.SecretReference{
				Name:      "test-repo",
				Namespace: "dr-syncer-system",
			},
		},
		Status: drv1alpha1.VolumeBackupOperationStatus{
			Phase:           drv1alpha1.VolumeBackupPhaseBackupComplete,
			KopiaSnapshotID: "snap-jkl012",
		},
	}

	restoreOp := &drv1alpha1.VolumeBackupOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-mapping-data-pvc-standby-restore",
			Namespace: "dst-ns",
		},
		Spec: drv1alpha1.VolumeBackupOperationSpec{
			OperationType: drv1alpha1.OperationTypeRestore,
			SourcePVC:     drv1alpha1.PVCReference{Namespace: "dst-ns", Name: "data-pvc-standby"},
			BackupRepositoryRef: drv1alpha1.SecretReference{
				Name:      "test-repo",
				Namespace: "dr-syncer-system",
			},
			SnapshotID: "snap-jkl012",
		},
		Status: drv1alpha1.VolumeBackupOperationStatus{
			Phase:           drv1alpha1.VolumeBackupPhaseRestoreComplete,
			KopiaSnapshotID: "snap-jkl012",
		},
	}

	srcClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(backupOp).Build()
	dstClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(restoreOp).Build()

	updateCalled := false
	mockMgr := &mockStandbyPVCManager{
		ensureFunc: func(ctx context.Context, sourcePVC drv1alpha1.PVCReference, destNamespace string, spec *corev1.PersistentVolumeClaimSpec) (*drv1alpha1.PVCReference, bool, error) {
			return &drv1alpha1.PVCReference{Namespace: "dst-ns", Name: "data-pvc-standby"}, false, nil
		},
		updateFunc: func(ctx context.Context, pvcRef drv1alpha1.PVCReference, snapshotID string) error {
			updateCalled = true
			return fmt.Errorf("annotation update conflict")
		},
	}

	syncer := NewVolumeBackupSyncer(srcClient, dstClient)
	syncer.PollInterval = 50 * time.Millisecond

	mapping := newTestMapping("test-mapping", "src-ns", "dst-ns", true)
	repo := newTestRepo("test-repo", "dr-syncer-system", drv1alpha1.BackupRepositoryStateReady)
	pvc := newTestPVC("data-pvc", "src-ns")

	result := syncer.syncSinglePVC(context.Background(), mapping, &pvc,
		mapping.Spec.PVCConfig.DataSyncConfig.BackupConfig, repo, mockMgr)

	// UpdateStandbyPVCStatus error should be non-fatal.
	if result.Err != nil {
		t.Fatalf("expected no error (update status is non-fatal), got: %v", result.Err)
	}
	if !updateCalled {
		t.Error("expected UpdateStandbyPVCStatus to be called")
	}
}
