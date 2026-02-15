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
