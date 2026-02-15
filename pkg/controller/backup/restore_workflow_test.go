package backup

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
)

// restoreTestLogger creates a logrus entry for test output.
func restoreTestLogger() *logrus.Entry {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	return logrus.NewEntry(logger)
}

// mockStandbyPVCManager implements StandbyPVCManager for testing.
type mockStandbyPVCManager struct {
	ensureFunc func(ctx context.Context, sourcePVC drv1alpha1.PVCReference, destNamespace string, spec *corev1.PersistentVolumeClaimSpec) (*drv1alpha1.PVCReference, bool, error)
	updateFunc func(ctx context.Context, pvcRef drv1alpha1.PVCReference, snapshotID string) error
}

func (m *mockStandbyPVCManager) EnsureStandbyPVC(ctx context.Context, sourcePVC drv1alpha1.PVCReference, destNamespace string, spec *corev1.PersistentVolumeClaimSpec) (*drv1alpha1.PVCReference, bool, error) {
	if m.ensureFunc != nil {
		return m.ensureFunc(ctx, sourcePVC, destNamespace, spec)
	}
	return &drv1alpha1.PVCReference{
		Namespace: destNamespace,
		Name:      sourcePVC.Name + standbyPVCSuffix,
	}, false, nil
}

func (m *mockStandbyPVCManager) UpdateStandbyPVCStatus(ctx context.Context, pvcRef drv1alpha1.PVCReference, snapshotID string) error {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, pvcRef, snapshotID)
	}
	return nil
}

// restoreTestRepo returns a ready BackupRepository with valid S3 and Kopia config.
func restoreTestRepo(namespace string) *drv1alpha1.BackupRepository {
	return &drv1alpha1.BackupRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-repo",
			Namespace: namespace,
		},
		Spec: drv1alpha1.BackupRepositorySpec{
			S3Config: drv1alpha1.S3Config{
				Endpoint: "s3.example.com",
				Bucket:   "test-bucket",
				Region:   "us-east-1",
				CredentialsSecretRef: drv1alpha1.SecretReference{
					Name:      "s3-creds",
					Namespace: namespace,
				},
			},
			KopiaConfig: drv1alpha1.KopiaConfig{
				EncryptionSecretRef: drv1alpha1.SecretReference{
					Name:      "kopia-enc",
					Namespace: namespace,
				},
			},
		},
		Status: drv1alpha1.BackupRepositoryStatus{
			State: drv1alpha1.BackupRepositoryStateReady,
		},
	}
}

// restoreTestSecrets returns the S3 credentials and Kopia encryption secrets.
func restoreTestSecrets(namespace string) []*corev1.Secret {
	return []*corev1.Secret{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "s3-creds",
				Namespace: namespace,
			},
			Data: map[string][]byte{
				"accessKeyID":     []byte("test-key"),
				"secretAccessKey": []byte("test-secret"),
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kopia-enc",
				Namespace: namespace,
			},
			Data: map[string][]byte{
				"password": []byte("test-password"),
			},
		},
	}
}

func TestVerifyRepository(t *testing.T) {
	scheme := testScheme()
	destNS := "dr-namespace"

	tests := []struct {
		name      string
		objects   []client.Object
		wantErr   bool
		errSubstr string
	}{
		{
			name: "repository ready with all secrets",
			objects: func() []client.Object {
				repo := restoreTestRepo(destNS)
				secrets := restoreTestSecrets(destNS)
				objs := []client.Object{repo}
				for _, s := range secrets {
					objs = append(objs, s)
				}
				return objs
			}(),
			wantErr: false,
		},
		{
			name:      "repository not found",
			objects:   []client.Object{},
			wantErr:   true,
			errSubstr: "not found on DR cluster",
		},
		{
			name: "repository not ready",
			objects: func() []client.Object {
				repo := restoreTestRepo(destNS)
				repo.Status.State = drv1alpha1.BackupRepositoryStateError
				secrets := restoreTestSecrets(destNS)
				objs := []client.Object{repo}
				for _, s := range secrets {
					objs = append(objs, s)
				}
				return objs
			}(),
			wantErr:   true,
			errSubstr: "not ready",
		},
		{
			name: "S3 credentials secret missing",
			objects: func() []client.Object {
				repo := restoreTestRepo(destNS)
				// Only add Kopia secret, not S3 secret.
				return []client.Object{repo, restoreTestSecrets(destNS)[1]}
			}(),
			wantErr:   true,
			errSubstr: "S3 credentials secret",
		},
		{
			name: "Kopia encryption secret missing",
			objects: func() []client.Object {
				repo := restoreTestRepo(destNS)
				// Only add S3 secret, not Kopia secret.
				return []client.Object{repo, restoreTestSecrets(destNS)[0]}
			}(),
			wantErr:   true,
			errSubstr: "Kopia encryption secret",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			destClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.objects...).Build()

			rw := &RestoreWorkflow{
				DestClient: destClient,
				Log:        restoreTestLogger(),
			}

			repo := restoreTestRepo(destNS)
			err := rw.verifyRepository(context.Background(), repo)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("expected error containing %q, got: %s", tt.errSubstr, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestResolveSnapshotID(t *testing.T) {
	scheme := testScheme()
	sourceNS := "source-ns"

	tests := []struct {
		name      string
		req       RestoreRequest
		objects   []client.Object
		wantID    string
		wantErr   bool
		errSubstr string
	}{
		{
			name: "explicit snapshot ID",
			req: RestoreRequest{
				SnapshotID: "abc123",
				SourcePVC:  drv1alpha1.PVCReference{Namespace: sourceNS, Name: "my-pvc"},
			},
			wantID:  "abc123",
			wantErr: false,
		},
		{
			name: "invalid explicit snapshot ID",
			req: RestoreRequest{
				SnapshotID: "bad;injection",
				SourcePVC:  drv1alpha1.PVCReference{Namespace: sourceNS, Name: "my-pvc"},
			},
			wantErr:   true,
			errSubstr: "invalid",
		},
		{
			name: "resolve from latest backup operation",
			req: RestoreRequest{
				SourcePVC:   drv1alpha1.PVCReference{Namespace: sourceNS, Name: "my-pvc"},
				MappingName: "test-mapping",
			},
			objects: []client.Object{
				&drv1alpha1.VolumeBackupOperation{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-mapping-my-pvc-backup-old",
						Namespace: sourceNS,
						Labels: map[string]string{
							labelMapping:   "test-mapping",
							labelPVC:       "my-pvc",
							labelOperation: "backup",
						},
					},
					Spec: drv1alpha1.VolumeBackupOperationSpec{
						OperationType: drv1alpha1.OperationTypeBackup,
					},
					Status: drv1alpha1.VolumeBackupOperationStatus{
						Phase:           drv1alpha1.VolumeBackupPhaseBackupComplete,
						KopiaSnapshotID: "older-snap-id",
						CompletionTime:  &metav1.Time{Time: time.Now().Add(-2 * time.Hour)},
					},
				},
				&drv1alpha1.VolumeBackupOperation{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-mapping-my-pvc-backup-new",
						Namespace: sourceNS,
						Labels: map[string]string{
							labelMapping:   "test-mapping",
							labelPVC:       "my-pvc",
							labelOperation: "backup",
						},
					},
					Spec: drv1alpha1.VolumeBackupOperationSpec{
						OperationType: drv1alpha1.OperationTypeBackup,
					},
					Status: drv1alpha1.VolumeBackupOperationStatus{
						Phase:           drv1alpha1.VolumeBackupPhaseBackupComplete,
						KopiaSnapshotID: "latest-snap-id",
						CompletionTime:  &metav1.Time{Time: time.Now().Add(-1 * time.Hour)},
					},
				},
			},
			wantID:  "latest-snap-id",
			wantErr: false,
		},
		{
			name: "no completed backups found",
			req: RestoreRequest{
				SourcePVC:   drv1alpha1.PVCReference{Namespace: sourceNS, Name: "my-pvc"},
				MappingName: "test-mapping",
			},
			objects:   []client.Object{},
			wantErr:   true,
			errSubstr: "no completed backup found",
		},
		{
			name: "backup exists but not complete",
			req: RestoreRequest{
				SourcePVC:   drv1alpha1.PVCReference{Namespace: sourceNS, Name: "my-pvc"},
				MappingName: "test-mapping",
			},
			objects: []client.Object{
				&drv1alpha1.VolumeBackupOperation{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-mapping-my-pvc-backup",
						Namespace: sourceNS,
						Labels: map[string]string{
							labelMapping:   "test-mapping",
							labelPVC:       "my-pvc",
							labelOperation: "backup",
						},
					},
					Status: drv1alpha1.VolumeBackupOperationStatus{
						Phase: drv1alpha1.VolumeBackupPhaseBackupInProgress,
					},
				},
			},
			wantErr:   true,
			errSubstr: "no completed backup found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.objects...).Build()

			rw := &RestoreWorkflow{
				SourceClient: sourceClient,
				Log:          restoreTestLogger(),
			}

			gotID, err := rw.resolveSnapshotID(context.Background(), tt.req)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("expected error containing %q, got: %s", tt.errSubstr, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if gotID != tt.wantID {
					t.Errorf("got snapshot ID %q, want %q", gotID, tt.wantID)
				}
			}
		})
	}
}

func TestEnsureStandbyPVC(t *testing.T) {
	scheme := testScheme()
	destNS := "dr-namespace"
	sourceNS := "source-ns"

	tests := []struct {
		name        string
		standbyMgr  StandbyPVCManager
		destObjects []client.Object
		srcObjects  []client.Object
		wantPVCName string
		wantErr     bool
		errSubstr   string
	}{
		{
			name: "with StandbyPVCManager",
			standbyMgr: &mockStandbyPVCManager{
				ensureFunc: func(ctx context.Context, sourcePVC drv1alpha1.PVCReference, destNamespace string, spec *corev1.PersistentVolumeClaimSpec) (*drv1alpha1.PVCReference, bool, error) {
					return &drv1alpha1.PVCReference{
						Namespace: destNamespace,
						Name:      sourcePVC.Name + standbyPVCSuffix,
					}, true, nil
				},
			},
			srcObjects: []client.Object{
				&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{Name: "my-pvc", Namespace: sourceNS},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("10Gi"),
							},
						},
					},
				},
			},
			wantPVCName: "my-pvc-standby",
			wantErr:     false,
		},
		{
			name:       "without manager, standby PVC exists",
			standbyMgr: nil,
			destObjects: []client.Object{
				&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{Name: "my-pvc-standby", Namespace: destNS},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					},
				},
			},
			wantPVCName: "my-pvc-standby",
			wantErr:     false,
		},
		{
			name:        "without manager, standby PVC missing",
			standbyMgr:  nil,
			destObjects: []client.Object{},
			wantErr:     true,
			errSubstr:   "does not exist",
		},
		{
			name: "StandbyPVCManager returns error",
			standbyMgr: &mockStandbyPVCManager{
				ensureFunc: func(ctx context.Context, sourcePVC drv1alpha1.PVCReference, destNamespace string, spec *corev1.PersistentVolumeClaimSpec) (*drv1alpha1.PVCReference, bool, error) {
					return nil, false, fmt.Errorf("storage class not found")
				},
			},
			srcObjects: []client.Object{
				&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{Name: "my-pvc", Namespace: sourceNS},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					},
				},
			},
			wantErr:   true,
			errSubstr: "storage class",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			destClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.destObjects...).Build()
			sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.srcObjects...).Build()

			rw := &RestoreWorkflow{
				DestClient:    destClient,
				SourceClient:  sourceClient,
				StandbyPVCMgr: tt.standbyMgr,
				Log:           restoreTestLogger(),
			}

			req := RestoreRequest{
				SourcePVC:     drv1alpha1.PVCReference{Namespace: sourceNS, Name: "my-pvc"},
				DestNamespace: destNS,
				MappingName:   "test-mapping",
			}

			pvcRef, err := rw.ensureStandbyPVC(context.Background(), req)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("expected error containing %q, got: %s", tt.errSubstr, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if pvcRef.Name != tt.wantPVCName {
					t.Errorf("got PVC name %q, want %q", pvcRef.Name, tt.wantPVCName)
				}
			}
		})
	}
}

func TestCreateRestoreOperation(t *testing.T) {
	scheme := testScheme()
	destNS := "dr-namespace"

	t.Run("create new operation", func(t *testing.T) {
		destClient := fake.NewClientBuilder().WithScheme(scheme).Build()

		rw := &RestoreWorkflow{
			DestClient: destClient,
			Log:        restoreTestLogger(),
		}

		repo := restoreTestRepo(destNS)
		req := RestoreRequest{
			SourcePVC:   drv1alpha1.PVCReference{Namespace: "source-ns", Name: "my-pvc"},
			MappingName: "test-mapping",
			Repository:  repo,
		}

		destPVCRef := &drv1alpha1.PVCReference{Namespace: destNS, Name: "my-pvc-standby"}

		op, err := rw.createRestoreOperation(context.Background(), req, "snap-123", destPVCRef)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if op.Spec.SnapshotID != "snap-123" {
			t.Errorf("got snapshot ID %q, want %q", op.Spec.SnapshotID, "snap-123")
		}
		if op.Spec.OperationType != drv1alpha1.OperationTypeRestore {
			t.Errorf("got operation type %q, want %q", op.Spec.OperationType, drv1alpha1.OperationTypeRestore)
		}
		if op.Labels[labelOperation] != "restore" {
			t.Errorf("got label %q, want %q", op.Labels[labelOperation], "restore")
		}
	})

	t.Run("update existing operation (idempotent)", func(t *testing.T) {
		existing := &drv1alpha1.VolumeBackupOperation{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-mapping-my-pvc-restore",
				Namespace: destNS,
				Labels: map[string]string{
					labelMapping:   "test-mapping",
					labelPVC:       "my-pvc",
					labelOperation: "restore",
				},
			},
			Spec: drv1alpha1.VolumeBackupOperationSpec{
				OperationType: drv1alpha1.OperationTypeRestore,
				SnapshotID:    "old-snap",
				SourcePVC:     drv1alpha1.PVCReference{Namespace: destNS, Name: "my-pvc-standby"},
			},
			Status: drv1alpha1.VolumeBackupOperationStatus{
				Phase:   drv1alpha1.VolumeBackupPhaseRestoreComplete,
				Message: "old restore",
			},
		}

		destClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()

		rw := &RestoreWorkflow{
			DestClient: destClient,
			Log:        restoreTestLogger(),
		}

		repo := restoreTestRepo(destNS)
		req := RestoreRequest{
			SourcePVC:   drv1alpha1.PVCReference{Namespace: "source-ns", Name: "my-pvc"},
			MappingName: "test-mapping",
			Repository:  repo,
		}

		destPVCRef := &drv1alpha1.PVCReference{Namespace: destNS, Name: "my-pvc-standby"}

		op, err := rw.createRestoreOperation(context.Background(), req, "new-snap-456", destPVCRef)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify the operation was updated with the new snapshot ID.
		if op.Spec.SnapshotID != "new-snap-456" {
			t.Errorf("got snapshot ID %q, want %q", op.Spec.SnapshotID, "new-snap-456")
		}

		// Verify status was reset.
		if op.Status.Phase != "" {
			t.Errorf("expected reset status phase, got %q", op.Status.Phase)
		}
	})
}

func TestAnnotateStandbyPVC(t *testing.T) {
	scheme := testScheme()
	destNS := "dr-namespace"

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pvc-standby",
			Namespace: destNS,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
	}

	destClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pvc).Build()

	rw := &RestoreWorkflow{
		DestClient: destClient,
		Log:        restoreTestLogger(),
	}

	pvcRef := &drv1alpha1.PVCReference{Namespace: destNS, Name: "my-pvc-standby"}
	rw.annotateStandbyPVC(context.Background(), pvcRef, "snap-789")

	// Verify annotations were set.
	updated := &corev1.PersistentVolumeClaim{}
	_ = destClient.Get(context.Background(), types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace}, updated)

	if updated.Annotations[annotationLastRestoreSnapID] != "snap-789" {
		t.Errorf("got annotation %q, want %q", updated.Annotations[annotationLastRestoreSnapID], "snap-789")
	}
	if updated.Annotations[annotationLastRestoreTime] == "" {
		t.Error("expected annotationLastRestoreTime to be set")
	}
}

func TestAnnotateStandbyPVC_NonExistentPVC(t *testing.T) {
	scheme := testScheme()
	destClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	rw := &RestoreWorkflow{
		DestClient: destClient,
		Log:        restoreTestLogger(),
	}

	// Should not panic; just logs a warning.
	pvcRef := &drv1alpha1.PVCReference{Namespace: "dr-ns", Name: "nonexistent-pvc"}
	rw.annotateStandbyPVC(context.Background(), pvcRef, "snap-123")
}

func TestUpdateCompletionStatus_WithStandbyPVCManager(t *testing.T) {
	scheme := testScheme()
	destNS := "dr-namespace"

	// Create the restore operation and standby PVC on the dest cluster.
	restoreOp := &drv1alpha1.VolumeBackupOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-mapping-my-pvc-restore",
			Namespace: destNS,
		},
		Spec: drv1alpha1.VolumeBackupOperationSpec{
			OperationType: drv1alpha1.OperationTypeRestore,
			SnapshotID:    "snap-999",
		},
		Status: drv1alpha1.VolumeBackupOperationStatus{
			Phase:            drv1alpha1.VolumeBackupPhaseRestoreComplete,
			BytesTransferred: 1024 * 1024,
		},
	}

	destClient := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(restoreOp).
		WithStatusSubresource(restoreOp).
		Build()

	var updateCalled bool
	var capturedSnapshotID string
	mockMgr := &mockStandbyPVCManager{
		updateFunc: func(ctx context.Context, pvcRef drv1alpha1.PVCReference, snapshotID string) error {
			updateCalled = true
			capturedSnapshotID = snapshotID
			return nil
		},
	}

	rw := &RestoreWorkflow{
		DestClient:    destClient,
		StandbyPVCMgr: mockMgr,
		Log:           restoreTestLogger(),
	}

	req := RestoreRequest{
		SourcePVC:   drv1alpha1.PVCReference{Namespace: "source-ns", Name: "my-pvc"},
		MappingName: "test-mapping",
	}
	destPVCRef := &drv1alpha1.PVCReference{Namespace: destNS, Name: "my-pvc-standby"}

	result, err := rw.updateCompletionStatus(context.Background(), req, "snap-999", destPVCRef, restoreOp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !updateCalled {
		t.Error("expected StandbyPVCManager.UpdateStandbyPVCStatus to be called")
	}
	if capturedSnapshotID != "snap-999" {
		t.Errorf("expected snapshot ID 'snap-999', got %q", capturedSnapshotID)
	}
	if result.SnapshotID != "snap-999" {
		t.Errorf("expected result snapshot ID 'snap-999', got %q", result.SnapshotID)
	}
	if result.StandbyPVCRef.Name != "my-pvc-standby" {
		t.Errorf("expected standby PVC name 'my-pvc-standby', got %q", result.StandbyPVCRef.Name)
	}
	if result.BytesRestored != 1024*1024 {
		t.Errorf("expected bytes restored %d, got %d", 1024*1024, result.BytesRestored)
	}
}

func TestUpdateCompletionStatus_WithoutStandbyPVCManager(t *testing.T) {
	scheme := testScheme()
	destNS := "dr-namespace"

	restoreOp := &drv1alpha1.VolumeBackupOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-mapping-my-pvc-restore",
			Namespace: destNS,
		},
		Spec: drv1alpha1.VolumeBackupOperationSpec{
			OperationType: drv1alpha1.OperationTypeRestore,
			SnapshotID:    "snap-888",
		},
		Status: drv1alpha1.VolumeBackupOperationStatus{
			Phase:            drv1alpha1.VolumeBackupPhaseRestoreComplete,
			BytesTransferred: 2048,
		},
	}

	standbyPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pvc-standby",
			Namespace: destNS,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
	}

	destClient := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(restoreOp, standbyPVC).
		WithStatusSubresource(restoreOp).
		Build()

	rw := &RestoreWorkflow{
		DestClient:    destClient,
		StandbyPVCMgr: nil, // no manager — should annotate directly
		Log:           restoreTestLogger(),
	}

	req := RestoreRequest{
		SourcePVC:   drv1alpha1.PVCReference{Namespace: "source-ns", Name: "my-pvc"},
		MappingName: "test-mapping",
	}
	destPVCRef := &drv1alpha1.PVCReference{Namespace: destNS, Name: "my-pvc-standby"}

	result, err := rw.updateCompletionStatus(context.Background(), req, "snap-888", destPVCRef, restoreOp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.BytesRestored != 2048 {
		t.Errorf("expected bytes restored 2048, got %d", result.BytesRestored)
	}

	// Verify annotations were set directly on the PVC.
	updated := &corev1.PersistentVolumeClaim{}
	_ = destClient.Get(context.Background(), types.NamespacedName{Name: "my-pvc-standby", Namespace: destNS}, updated)

	if updated.Annotations[annotationLastRestoreSnapID] != "snap-888" {
		t.Errorf("expected annotation 'snap-888', got %q", updated.Annotations[annotationLastRestoreSnapID])
	}
}

func TestUpdateCompletionStatus_ManagerUpdateFailsNonFatal(t *testing.T) {
	scheme := testScheme()
	destNS := "dr-namespace"

	restoreOp := &drv1alpha1.VolumeBackupOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-mapping-my-pvc-restore",
			Namespace: destNS,
		},
		Status: drv1alpha1.VolumeBackupOperationStatus{
			Phase: drv1alpha1.VolumeBackupPhaseRestoreComplete,
		},
	}

	destClient := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(restoreOp).
		WithStatusSubresource(restoreOp).
		Build()

	mockMgr := &mockStandbyPVCManager{
		updateFunc: func(ctx context.Context, pvcRef drv1alpha1.PVCReference, snapshotID string) error {
			return fmt.Errorf("PVC annotation update failed")
		},
	}

	rw := &RestoreWorkflow{
		DestClient:    destClient,
		StandbyPVCMgr: mockMgr,
		Log:           restoreTestLogger(),
	}

	req := RestoreRequest{
		SourcePVC:   drv1alpha1.PVCReference{Namespace: "source-ns", Name: "my-pvc"},
		MappingName: "test-mapping",
	}
	destPVCRef := &drv1alpha1.PVCReference{Namespace: destNS, Name: "my-pvc-standby"}

	// StandbyPVCManager.UpdateStandbyPVCStatus failure is non-fatal.
	result, err := rw.updateCompletionStatus(context.Background(), req, "snap-777", destPVCRef, restoreOp)
	if err != nil {
		t.Fatalf("expected no error (manager failure is non-fatal), got: %v", err)
	}
	if result.SnapshotID != "snap-777" {
		t.Errorf("expected snapshot ID 'snap-777', got %q", result.SnapshotID)
	}
}

func TestEnsureStandbyPVC_FetchesSourcePVCSpec(t *testing.T) {
	scheme := testScheme()
	sourceNS := "source-ns"
	destNS := "dr-namespace"

	// Source PVC exists in source cluster.
	sourcePVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "my-pvc", Namespace: sourceNS},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("20Gi"),
				},
			},
		},
	}

	var capturedSpec *corev1.PersistentVolumeClaimSpec
	mockMgr := &mockStandbyPVCManager{
		ensureFunc: func(ctx context.Context, src drv1alpha1.PVCReference, destNamespace string, spec *corev1.PersistentVolumeClaimSpec) (*drv1alpha1.PVCReference, bool, error) {
			capturedSpec = spec
			return &drv1alpha1.PVCReference{
				Namespace: destNamespace,
				Name:      src.Name + standbyPVCSuffix,
			}, true, nil
		},
	}

	destClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(sourcePVC).Build()

	rw := &RestoreWorkflow{
		DestClient:    destClient,
		SourceClient:  sourceClient,
		StandbyPVCMgr: mockMgr,
		Log:           restoreTestLogger(),
	}

	req := RestoreRequest{
		SourcePVC:     drv1alpha1.PVCReference{Namespace: sourceNS, Name: "my-pvc"},
		DestNamespace: destNS,
		MappingName:   "test-mapping",
		SourcePVCSpec: nil, // explicitly nil — workflow should fetch from source cluster
	}

	pvcRef, err := rw.ensureStandbyPVC(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pvcRef.Name != "my-pvc-standby" {
		t.Errorf("expected PVC name 'my-pvc-standby', got %q", pvcRef.Name)
	}
	if capturedSpec == nil {
		t.Fatal("expected spec to be fetched from source cluster")
	}
	storageReq := capturedSpec.Resources.Requests[corev1.ResourceStorage]
	if storageReq.Cmp(resource.MustParse("20Gi")) != 0 {
		t.Errorf("expected fetched spec to have 20Gi storage, got %s", storageReq.String())
	}
}

func TestEnsureStandbyPVC_SourcePVCNotFound(t *testing.T) {
	scheme := testScheme()

	mockMgr := &mockStandbyPVCManager{
		ensureFunc: func(ctx context.Context, src drv1alpha1.PVCReference, destNamespace string, spec *corev1.PersistentVolumeClaimSpec) (*drv1alpha1.PVCReference, bool, error) {
			return &drv1alpha1.PVCReference{Namespace: destNamespace, Name: src.Name + standbyPVCSuffix}, true, nil
		},
	}

	destClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	sourceClient := fake.NewClientBuilder().WithScheme(scheme).Build() // no source PVC

	rw := &RestoreWorkflow{
		DestClient:    destClient,
		SourceClient:  sourceClient,
		StandbyPVCMgr: mockMgr,
		Log:           restoreTestLogger(),
	}

	req := RestoreRequest{
		SourcePVC:     drv1alpha1.PVCReference{Namespace: "source-ns", Name: "missing-pvc"},
		DestNamespace: "dr-ns",
		MappingName:   "test-mapping",
		SourcePVCSpec: nil,
	}

	_, err := rw.ensureStandbyPVC(context.Background(), req)
	if err == nil {
		t.Fatal("expected error when source PVC not found")
	}
	if !strings.Contains(err.Error(), "get source PVC spec") {
		t.Errorf("expected 'get source PVC spec' error, got: %v", err)
	}
}

func TestNewRestoreWorkflow(t *testing.T) {
	scheme := testScheme()
	destClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	sourceClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	rw := NewRestoreWorkflow(destClient, sourceClient)
	if rw == nil {
		t.Fatal("expected non-nil RestoreWorkflow")
	}
	if rw.DestClient != destClient {
		t.Error("DestClient not set correctly")
	}
	if rw.SourceClient != sourceClient {
		t.Error("SourceClient not set correctly")
	}
	if rw.BackupWf == nil {
		t.Error("expected non-nil BackupWf")
	}
	if rw.Log == nil {
		t.Error("expected non-nil Log")
	}
}

func TestCleanupRestoreResources_NoOp(t *testing.T) {
	scheme := testScheme()
	destClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	rw := &RestoreWorkflow{
		DestClient: destClient,
		Log:        restoreTestLogger(),
	}

	op := &drv1alpha1.VolumeBackupOperation{
		ObjectMeta: metav1.ObjectMeta{Name: "test-restore-op", Namespace: "dr-ns"},
	}

	// Should not panic — currently a no-op.
	rw.cleanupRestoreResources(context.Background(), op)
}

func TestResolveSnapshotID_BackupWithEmptySnapshotID(t *testing.T) {
	scheme := testScheme()
	sourceNS := "source-ns"

	// Backup exists and is complete but has an empty KopiaSnapshotID.
	sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&drv1alpha1.VolumeBackupOperation{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-mapping-my-pvc-backup",
				Namespace: sourceNS,
				Labels: map[string]string{
					labelMapping:   "test-mapping",
					labelPVC:       "my-pvc",
					labelOperation: "backup",
				},
			},
			Status: drv1alpha1.VolumeBackupOperationStatus{
				Phase:           drv1alpha1.VolumeBackupPhaseBackupComplete,
				KopiaSnapshotID: "", // empty
				CompletionTime:  &metav1.Time{Time: time.Now().Add(-1 * time.Hour)},
			},
		},
	).Build()

	rw := &RestoreWorkflow{
		SourceClient: sourceClient,
		Log:          restoreTestLogger(),
	}

	req := RestoreRequest{
		SourcePVC:   drv1alpha1.PVCReference{Namespace: sourceNS, Name: "my-pvc"},
		MappingName: "test-mapping",
	}

	_, err := rw.resolveSnapshotID(context.Background(), req)
	if err == nil {
		t.Fatal("expected error when backup has empty snapshot ID")
	}
	if !strings.Contains(err.Error(), "no completed backup found") {
		t.Errorf("expected 'no completed backup found' error, got: %v", err)
	}
}

func TestResolveSnapshotID_BackupWithNilCompletionTime(t *testing.T) {
	scheme := testScheme()
	sourceNS := "source-ns"

	sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&drv1alpha1.VolumeBackupOperation{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-mapping-my-pvc-backup",
				Namespace: sourceNS,
				Labels: map[string]string{
					labelMapping:   "test-mapping",
					labelPVC:       "my-pvc",
					labelOperation: "backup",
				},
			},
			Status: drv1alpha1.VolumeBackupOperationStatus{
				Phase:           drv1alpha1.VolumeBackupPhaseBackupComplete,
				KopiaSnapshotID: "snap-ok",
				CompletionTime:  nil, // nil completion time
			},
		},
	).Build()

	rw := &RestoreWorkflow{
		SourceClient: sourceClient,
		Log:          restoreTestLogger(),
	}

	req := RestoreRequest{
		SourcePVC:   drv1alpha1.PVCReference{Namespace: sourceNS, Name: "my-pvc"},
		MappingName: "test-mapping",
	}

	_, err := rw.resolveSnapshotID(context.Background(), req)
	if err == nil {
		t.Fatal("expected error when backup has nil completion time")
	}
	if !strings.Contains(err.Error(), "no completed backup found") {
		t.Errorf("expected 'no completed backup found' error, got: %v", err)
	}
}

// --- ExecuteRestore integration tests ---

// TestExecuteRestore_HappyPath_FullWorkflow exercises the complete 8-step restore
// workflow end-to-end with simulated pods and a mock StandbyPVCManager.
//
// Steps verified:
//  1. Verify BackupRepository accessible on DR cluster
//  2. Resolve snapshot ID from latest completed backup on source cluster
//  3. Ensure standby PVC via StandbyPVCManager
//  4. Create VolumeBackupOperation CR for restore on DR cluster
//  5. Execute Kopia restore pod (simulated via goroutine)
//  6. Progress monitoring (implicit in BackupWorkflow)
//  7. Cleanup restore resources
//  8. Update completion status and standby PVC annotations
func TestExecuteRestore_HappyPath_FullWorkflow(t *testing.T) {
	scheme := testScheme()
	sourceNS := "source-ns"
	destNS := "dr-namespace"

	// --- Source cluster objects ---
	// A completed backup operation with a valid snapshot ID.
	completedBackup := &drv1alpha1.VolumeBackupOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-mapping-my-pvc-backup",
			Namespace: sourceNS,
			Labels: map[string]string{
				labelMapping:   "test-mapping",
				labelPVC:       "my-pvc",
				labelOperation: "backup",
			},
		},
		Spec: drv1alpha1.VolumeBackupOperationSpec{
			OperationType: drv1alpha1.OperationTypeBackup,
		},
		Status: drv1alpha1.VolumeBackupOperationStatus{
			Phase:           drv1alpha1.VolumeBackupPhaseBackupComplete,
			KopiaSnapshotID: "snap-restore-001",
			CompletionTime:  &metav1.Time{Time: time.Now().Add(-1 * time.Hour)},
		},
	}

	sourceClient := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(completedBackup).
		Build()

	// --- Destination cluster objects ---
	repo := restoreTestRepo(destNS)
	secrets := restoreTestSecrets(destNS)

	destObjs := []client.Object{repo}
	for _, s := range secrets {
		destObjs = append(destObjs, s)
	}

	destClient := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(destObjs...).
		WithStatusSubresource(
			&drv1alpha1.VolumeBackupOperation{},
			&corev1.Pod{},
		).
		Build()

	// --- Mock StandbyPVCManager ---
	var ensureCalled bool
	var updateCalled bool
	var capturedSnapshotID string
	mockMgr := &mockStandbyPVCManager{
		ensureFunc: func(ctx context.Context, sourcePVC drv1alpha1.PVCReference, destNamespace string, spec *corev1.PersistentVolumeClaimSpec) (*drv1alpha1.PVCReference, bool, error) {
			ensureCalled = true
			return &drv1alpha1.PVCReference{
				Namespace: destNamespace,
				Name:      sourcePVC.Name + standbyPVCSuffix,
			}, true, nil
		},
		updateFunc: func(ctx context.Context, pvcRef drv1alpha1.PVCReference, snapshotID string) error {
			updateCalled = true
			capturedSnapshotID = snapshotID
			return nil
		},
	}

	// --- Build RestoreWorkflow ---
	// Create BackupWorkflow with fast poll interval pointing at the dest cluster.
	backupWf := &BackupWorkflow{
		Client:          destClient,
		Log:             restoreTestLogger(),
		PodPollInterval: 50 * time.Millisecond,
	}

	rw := &RestoreWorkflow{
		DestClient:    destClient,
		SourceClient:  sourceClient,
		BackupWf:      backupWf,
		StandbyPVCMgr: mockMgr,
		Log:           restoreTestLogger(),
	}

	// Source PVC in source cluster (needed for fetching spec when SourcePVCSpec is nil).
	sourcePVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "my-pvc", Namespace: sourceNS},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
	}
	if err := sourceClient.Create(context.Background(), sourcePVC); err != nil {
		t.Fatalf("failed to create source PVC: %v", err)
	}

	req := RestoreRequest{
		SourcePVC:     drv1alpha1.PVCReference{Namespace: sourceNS, Name: "my-pvc"},
		DestNamespace: destNS,
		MappingName:   "test-mapping",
		Repository:    repo,
		PodConfig:     DefaultKopiaPodConfig(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Simulate the Kopia restore pod reaching Succeeded state with restore summary.
	// The restore operation name will be "test-mapping-my-pvc-restore",
	// so the pod name is "dr-syncer-kopia-restore-test-mapping-my-pvc-restore".
	go simulatePodCompletion(ctx, destClient, destNS, "dr-syncer-kopia-restore-",
		corev1.PodSucceeded, "Restored 15 files, 3 directories and 0 symbolic links (1.5 MB)")

	// --- Execute ---
	result, err := rw.ExecuteRestore(ctx, req)
	if err != nil {
		t.Fatalf("ExecuteRestore failed: %v", err)
	}

	// --- Verify result ---
	if result.SnapshotID != "snap-restore-001" {
		t.Errorf("expected snapshot ID 'snap-restore-001', got %q", result.SnapshotID)
	}
	if result.StandbyPVCRef == nil {
		t.Fatal("expected non-nil StandbyPVCRef")
	}
	if result.StandbyPVCRef.Name != "my-pvc-standby" {
		t.Errorf("expected standby PVC name 'my-pvc-standby', got %q", result.StandbyPVCRef.Name)
	}
	if result.StandbyPVCRef.Namespace != destNS {
		t.Errorf("expected standby PVC namespace %q, got %q", destNS, result.StandbyPVCRef.Namespace)
	}

	// --- Verify Step 3: StandbyPVCManager.EnsureStandbyPVC was called ---
	if !ensureCalled {
		t.Error("expected StandbyPVCManager.EnsureStandbyPVC to be called")
	}

	// --- Verify Step 4: VolumeBackupOperation CR created on dest cluster ---
	restoreOp := &drv1alpha1.VolumeBackupOperation{}
	opKey := types.NamespacedName{
		Name:      "test-mapping-my-pvc-restore",
		Namespace: destNS,
	}
	if err := destClient.Get(ctx, opKey, restoreOp); err != nil {
		t.Fatalf("expected restore VolumeBackupOperation to exist: %v", err)
	}
	if restoreOp.Spec.OperationType != drv1alpha1.OperationTypeRestore {
		t.Errorf("expected operation type Restore, got %s", restoreOp.Spec.OperationType)
	}
	if restoreOp.Spec.SnapshotID != "snap-restore-001" {
		t.Errorf("expected operation snapshot ID 'snap-restore-001', got %q", restoreOp.Spec.SnapshotID)
	}
	if restoreOp.Labels[labelOperation] != "restore" {
		t.Errorf("expected label 'restore', got %q", restoreOp.Labels[labelOperation])
	}

	// --- Verify Step 5: Operation status after BackupWorkflow.Execute completed ---
	if restoreOp.Status.Phase != drv1alpha1.VolumeBackupPhaseRestoreComplete {
		t.Errorf("expected operation phase RestoreComplete, got %s", restoreOp.Status.Phase)
	}
	if restoreOp.Status.CompletionTime == nil {
		t.Error("expected CompletionTime to be set on restore operation")
	}
	if restoreOp.Status.ProgressPercentage != 100 {
		t.Errorf("expected progress 100, got %d", restoreOp.Status.ProgressPercentage)
	}

	// BytesRestored is extracted from the Kopia restore pod's summary output.
	// "1.5 MB" = 1.5 * 1024 * 1024 = 1572864 bytes
	expectedBytes := int64(1572864)
	if result.BytesRestored != expectedBytes {
		t.Errorf("expected BytesRestored %d, got %d", expectedBytes, result.BytesRestored)
	}

	// --- Verify Step 8: StandbyPVCManager.UpdateStandbyPVCStatus was called ---
	if !updateCalled {
		t.Error("expected StandbyPVCManager.UpdateStandbyPVCStatus to be called")
	}
	if capturedSnapshotID != "snap-restore-001" {
		t.Errorf("expected update snapshot ID 'snap-restore-001', got %q", capturedSnapshotID)
	}
}
