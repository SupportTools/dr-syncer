package controllers

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
)

// --- Mock RepositoryClient ---

type mockRepoClient struct {
	initErr        error
	healthStats    *RepositoryStats
	healthErr      error
	maintenanceErr error

	initCalled        int
	healthCalled      int
	maintenanceCalled int
}

func (m *mockRepoClient) InitializeRepository(ctx context.Context, s3Config drv1alpha1.S3Config, kopiaConfig drv1alpha1.KopiaConfig, namespace string) error {
	m.initCalled++
	return m.initErr
}

func (m *mockRepoClient) CheckHealth(ctx context.Context, s3Config drv1alpha1.S3Config, kopiaConfig drv1alpha1.KopiaConfig, namespace string) (*RepositoryStats, error) {
	m.healthCalled++
	return m.healthStats, m.healthErr
}

func (m *mockRepoClient) RunMaintenance(ctx context.Context, s3Config drv1alpha1.S3Config, kopiaConfig drv1alpha1.KopiaConfig, namespace string) error {
	m.maintenanceCalled++
	return m.maintenanceErr
}

// --- Test Helpers ---

func newTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = drv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	return scheme
}

func newTestBackupRepository(name, namespace string, state drv1alpha1.BackupRepositoryState) *drv1alpha1.BackupRepository {
	return &drv1alpha1.BackupRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: drv1alpha1.BackupRepositorySpec{
			S3Config: drv1alpha1.S3Config{
				Endpoint: "minio.example.com:9000",
				Bucket:   "backups",
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
			MaintenanceSchedule: "0 2 * * *",
		},
		Status: drv1alpha1.BackupRepositoryStatus{
			State: state,
		},
	}
}

func newS3CredentialsSecret(namespace string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "s3-creds",
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"accessKeyID":     []byte("AKIAIOSFODNN7EXAMPLE"),
			"secretAccessKey": []byte("wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"),
		},
	}
}

func newKopiaPasswordSecret(namespace string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kopia-password",
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"password": []byte("super-secret-passphrase"),
		},
	}
}

func newTestReconciler(scheme *runtime.Scheme, repoClient RepositoryClient, objects ...runtime.Object) *BackupRepositoryReconciler {
	clientObjs := make([]runtime.Object, len(objects))
	copy(clientObjs, objects)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(clientObjs...).
		WithStatusSubresource(&drv1alpha1.BackupRepository{}).
		Build()

	return &BackupRepositoryReconciler{
		Client:     fakeClient,
		Scheme:     scheme,
		Recorder:   record.NewFakeRecorder(100),
		RepoClient: repoClient,
	}
}

// --- Struct and Constants Tests ---

func TestBackupRepositoryReconciler_Struct(t *testing.T) {
	scheme := runtime.NewScheme()
	reconciler := &BackupRepositoryReconciler{
		Scheme: scheme,
	}

	assert.NotNil(t, reconciler)
	assert.NotNil(t, reconciler.Scheme)
	assert.Nil(t, reconciler.RepoClient)
}

func TestBackupRepositoryFinalizerNameConstant(t *testing.T) {
	assert.Equal(t, "dr-syncer.io/cleanup-backuprepository", BackupRepositoryFinalizerName)
}

func TestDefaultConstants(t *testing.T) {
	assert.Equal(t, 5*time.Minute, defaultHealthCheckInterval)
	assert.Equal(t, 1*time.Minute, defaultRequeueOnError)
}

// --- setBackupRepositoryCondition Tests ---

func TestSetBackupRepositoryCondition_NewCondition(t *testing.T) {
	repo := &drv1alpha1.BackupRepository{}

	setBackupRepositoryCondition(repo, "RepositoryHealthy", metav1.ConditionTrue, "HealthCheckPassed", "All good")

	require.Len(t, repo.Status.Conditions, 1)
	assert.Equal(t, "RepositoryHealthy", repo.Status.Conditions[0].Type)
	assert.Equal(t, metav1.ConditionTrue, repo.Status.Conditions[0].Status)
	assert.Equal(t, "HealthCheckPassed", repo.Status.Conditions[0].Reason)
	assert.Equal(t, "All good", repo.Status.Conditions[0].Message)
	assert.False(t, repo.Status.Conditions[0].LastTransitionTime.IsZero())
}

func TestSetBackupRepositoryCondition_UpdateExisting(t *testing.T) {
	oldTime := metav1.NewTime(time.Now().Add(-1 * time.Hour))
	repo := &drv1alpha1.BackupRepository{
		Status: drv1alpha1.BackupRepositoryStatus{
			Conditions: []metav1.Condition{
				{
					Type:               "RepositoryHealthy",
					Status:             metav1.ConditionTrue,
					Reason:             "HealthCheckPassed",
					Message:            "All good",
					LastTransitionTime: oldTime,
				},
			},
		},
	}

	setBackupRepositoryCondition(repo, "RepositoryHealthy", metav1.ConditionFalse, "HealthCheckFailed", "Connection lost")

	require.Len(t, repo.Status.Conditions, 1)
	assert.Equal(t, metav1.ConditionFalse, repo.Status.Conditions[0].Status)
	assert.Equal(t, "HealthCheckFailed", repo.Status.Conditions[0].Reason)
	assert.Equal(t, "Connection lost", repo.Status.Conditions[0].Message)
	// LastTransitionTime should be updated since status changed
	assert.True(t, repo.Status.Conditions[0].LastTransitionTime.After(oldTime.Time))
}

func TestSetBackupRepositoryCondition_NoChangeSkipsUpdate(t *testing.T) {
	oldTime := metav1.NewTime(time.Now().Add(-1 * time.Hour))
	repo := &drv1alpha1.BackupRepository{
		Status: drv1alpha1.BackupRepositoryStatus{
			Conditions: []metav1.Condition{
				{
					Type:               "RepositoryHealthy",
					Status:             metav1.ConditionTrue,
					Reason:             "HealthCheckPassed",
					Message:            "All good",
					LastTransitionTime: oldTime,
				},
			},
		},
	}

	setBackupRepositoryCondition(repo, "RepositoryHealthy", metav1.ConditionTrue, "HealthCheckPassed", "All good")

	require.Len(t, repo.Status.Conditions, 1)
	// Time should NOT change since nothing changed
	assert.Equal(t, oldTime.Time, repo.Status.Conditions[0].LastTransitionTime.Time)
}

func TestSetBackupRepositoryCondition_MultipleConditions(t *testing.T) {
	repo := &drv1alpha1.BackupRepository{}

	setBackupRepositoryCondition(repo, "RepositoryHealthy", metav1.ConditionTrue, "OK", "healthy")
	setBackupRepositoryCondition(repo, "RepositoryInitialized", metav1.ConditionTrue, "OK", "initialized")

	require.Len(t, repo.Status.Conditions, 2)
	assert.Equal(t, "RepositoryHealthy", repo.Status.Conditions[0].Type)
	assert.Equal(t, "RepositoryInitialized", repo.Status.Conditions[1].Type)
}

// --- isMaintenanceDue Tests ---

func TestIsMaintenanceDue_NeverRun(t *testing.T) {
	scheme := newTestScheme()
	r := &BackupRepositoryReconciler{Scheme: scheme}
	repo := newTestBackupRepository("test", "default", drv1alpha1.BackupRepositoryStateReady)
	repo.Status.LastMaintenanceTime = nil

	assert.True(t, r.isMaintenanceDue(repo))
}

func TestIsMaintenanceDue_PastDue(t *testing.T) {
	scheme := newTestScheme()
	r := &BackupRepositoryReconciler{Scheme: scheme}
	repo := newTestBackupRepository("test", "default", drv1alpha1.BackupRepositoryStateReady)
	// Last maintenance was 2 days ago, schedule is daily at 2am
	lastMaint := metav1.NewTime(time.Now().Add(-48 * time.Hour))
	repo.Status.LastMaintenanceTime = &lastMaint

	assert.True(t, r.isMaintenanceDue(repo))
}

func TestIsMaintenanceDue_NotDue(t *testing.T) {
	scheme := newTestScheme()
	r := &BackupRepositoryReconciler{Scheme: scheme}
	repo := newTestBackupRepository("test", "default", drv1alpha1.BackupRepositoryStateReady)
	// Last maintenance was just now
	lastMaint := metav1.NewTime(time.Now())
	repo.Status.LastMaintenanceTime = &lastMaint

	assert.False(t, r.isMaintenanceDue(repo))
}

func TestIsMaintenanceDue_InvalidSchedule(t *testing.T) {
	scheme := newTestScheme()
	r := &BackupRepositoryReconciler{Scheme: scheme}
	repo := newTestBackupRepository("test", "default", drv1alpha1.BackupRepositoryStateReady)
	repo.Spec.MaintenanceSchedule = "not-a-cron"

	// Invalid schedule should return false (logs error internally)
	assert.False(t, r.isMaintenanceDue(repo))
}

func TestIsMaintenanceDue_EmptyScheduleUsesDefault(t *testing.T) {
	scheme := newTestScheme()
	r := &BackupRepositoryReconciler{Scheme: scheme}
	repo := newTestBackupRepository("test", "default", drv1alpha1.BackupRepositoryStateReady)
	repo.Spec.MaintenanceSchedule = ""
	repo.Status.LastMaintenanceTime = nil

	// Empty schedule defaults to daily at 2am, never run = due
	assert.True(t, r.isMaintenanceDue(repo))
}

// --- Reconcile Tests ---

func TestReconcile_NotFound(t *testing.T) {
	scheme := newTestScheme()
	r := newTestReconciler(scheme, &mockRepoClient{})

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "nonexistent", Namespace: "default"},
	})

	assert.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
}

func TestReconcile_AddsFinalizer(t *testing.T) {
	scheme := newTestScheme()
	repo := newTestBackupRepository("test-repo", "default", "")
	s3Secret := newS3CredentialsSecret("default")
	kopiaSecret := newKopiaPasswordSecret("default")

	r := newTestReconciler(scheme, &mockRepoClient{}, repo, s3Secret, kopiaSecret)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-repo", Namespace: "default"},
	})

	assert.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	// Verify finalizer was added
	var updated drv1alpha1.BackupRepository
	err = r.Get(context.Background(), types.NamespacedName{Name: "test-repo", Namespace: "default"}, &updated)
	require.NoError(t, err)
	assert.Contains(t, updated.Finalizers, BackupRepositoryFinalizerName)
}

func TestReconcile_Deletion(t *testing.T) {
	scheme := newTestScheme()
	now := metav1.Now()
	repo := newTestBackupRepository("test-repo", "default", drv1alpha1.BackupRepositoryStateReady)
	repo.DeletionTimestamp = &now
	repo.Finalizers = []string{BackupRepositoryFinalizerName}

	r := newTestReconciler(scheme, &mockRepoClient{}, repo)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-repo", Namespace: "default"},
	})

	assert.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
}

func TestHandleDeletion_WithoutFinalizer(t *testing.T) {
	// Test the handleDeletion method directly when finalizer is already absent.
	// The fake client won't allow creating objects with deletionTimestamp but no finalizers,
	// so we test the method directly.
	scheme := newTestScheme()
	repo := newTestBackupRepository("test-repo", "default", drv1alpha1.BackupRepositoryStateReady)
	// No finalizer set

	r := newTestReconciler(scheme, &mockRepoClient{}, repo)

	result, err := r.handleDeletion(context.Background(), repo)

	assert.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
}

func TestReconcile_NilRepoClient(t *testing.T) {
	scheme := newTestScheme()
	repo := newTestBackupRepository("test-repo", "default", "")
	repo.Finalizers = []string{BackupRepositoryFinalizerName}
	s3Secret := newS3CredentialsSecret("default")
	kopiaSecret := newKopiaPasswordSecret("default")

	r := newTestReconciler(scheme, nil, repo, s3Secret, kopiaSecret)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-repo", Namespace: "default"},
	})

	assert.NoError(t, err)
	// Should requeue with error interval since client isn't ready
	assert.Equal(t, defaultRequeueOnError, result.RequeueAfter)

	// Check status updated to Initializing
	var updated drv1alpha1.BackupRepository
	err = r.Get(context.Background(), types.NamespacedName{Name: "test-repo", Namespace: "default"}, &updated)
	require.NoError(t, err)
	assert.Equal(t, drv1alpha1.BackupRepositoryStateInitializing, updated.Status.State)
}

func TestReconcile_SecretValidationFailure_MissingS3Secret(t *testing.T) {
	scheme := newTestScheme()
	repo := newTestBackupRepository("test-repo", "default", "")
	repo.Finalizers = []string{BackupRepositoryFinalizerName}
	// Only create kopia secret, not S3 secret
	kopiaSecret := newKopiaPasswordSecret("default")

	mockClient := &mockRepoClient{}
	r := newTestReconciler(scheme, mockClient, repo, kopiaSecret)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-repo", Namespace: "default"},
	})

	assert.NoError(t, err)
	assert.Equal(t, defaultRequeueOnError, result.RequeueAfter)

	// RepoClient should not have been called
	assert.Equal(t, 0, mockClient.initCalled)
}

func TestReconcile_SecretValidationFailure_MissingKey(t *testing.T) {
	scheme := newTestScheme()
	repo := newTestBackupRepository("test-repo", "default", "")
	repo.Finalizers = []string{BackupRepositoryFinalizerName}

	// S3 secret missing accessKeyID
	s3Secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "s3-creds", Namespace: "default"},
		Data:       map[string][]byte{"secretAccessKey": []byte("key")},
	}
	kopiaSecret := newKopiaPasswordSecret("default")

	mockClient := &mockRepoClient{}
	r := newTestReconciler(scheme, mockClient, repo, s3Secret, kopiaSecret)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-repo", Namespace: "default"},
	})

	assert.NoError(t, err)
	assert.Equal(t, defaultRequeueOnError, result.RequeueAfter)
	assert.Equal(t, 0, mockClient.initCalled)
}

func TestReconcile_SecretValidationFailure_MissingKopiaPassword(t *testing.T) {
	scheme := newTestScheme()
	repo := newTestBackupRepository("test-repo", "default", "")
	repo.Finalizers = []string{BackupRepositoryFinalizerName}
	s3Secret := newS3CredentialsSecret("default")

	// Kopia secret missing password key
	kopiaSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "kopia-password", Namespace: "default"},
		Data:       map[string][]byte{"wrong-key": []byte("value")},
	}

	mockClient := &mockRepoClient{}
	r := newTestReconciler(scheme, mockClient, repo, s3Secret, kopiaSecret)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-repo", Namespace: "default"},
	})

	assert.NoError(t, err)
	assert.Equal(t, defaultRequeueOnError, result.RequeueAfter)
	assert.Equal(t, 0, mockClient.initCalled)
}

func TestReconcile_InitializationSuccess(t *testing.T) {
	scheme := newTestScheme()
	repo := newTestBackupRepository("test-repo", "default", "")
	repo.Finalizers = []string{BackupRepositoryFinalizerName}
	s3Secret := newS3CredentialsSecret("default")
	kopiaSecret := newKopiaPasswordSecret("default")

	mockClient := &mockRepoClient{}
	r := newTestReconciler(scheme, mockClient, repo, s3Secret, kopiaSecret)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-repo", Namespace: "default"},
	})

	assert.NoError(t, err)
	assert.Equal(t, defaultHealthCheckInterval, result.RequeueAfter)
	assert.Equal(t, 1, mockClient.initCalled)

	// Verify status updated to Ready
	var updated drv1alpha1.BackupRepository
	err = r.Get(context.Background(), types.NamespacedName{Name: "test-repo", Namespace: "default"}, &updated)
	require.NoError(t, err)
	assert.Equal(t, drv1alpha1.BackupRepositoryStateReady, updated.Status.State)
}

func TestReconcile_InitializationFailure(t *testing.T) {
	scheme := newTestScheme()
	repo := newTestBackupRepository("test-repo", "default", "")
	repo.Finalizers = []string{BackupRepositoryFinalizerName}
	s3Secret := newS3CredentialsSecret("default")
	kopiaSecret := newKopiaPasswordSecret("default")

	mockClient := &mockRepoClient{initErr: fmt.Errorf("S3 connection refused")}
	r := newTestReconciler(scheme, mockClient, repo, s3Secret, kopiaSecret)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-repo", Namespace: "default"},
	})

	assert.NoError(t, err)
	assert.Equal(t, defaultRequeueOnError, result.RequeueAfter)
	assert.Equal(t, 1, mockClient.initCalled)

	var updated drv1alpha1.BackupRepository
	err = r.Get(context.Background(), types.NamespacedName{Name: "test-repo", Namespace: "default"}, &updated)
	require.NoError(t, err)
	assert.Equal(t, drv1alpha1.BackupRepositoryStateError, updated.Status.State)
}

func TestReconcile_ReadyState_HealthCheckSuccess(t *testing.T) {
	scheme := newTestScheme()
	repo := newTestBackupRepository("test-repo", "default", drv1alpha1.BackupRepositoryStateReady)
	repo.Finalizers = []string{BackupRepositoryFinalizerName}
	// Set last maintenance to now so it's not due
	lastMaint := metav1.NewTime(time.Now())
	repo.Status.LastMaintenanceTime = &lastMaint
	s3Secret := newS3CredentialsSecret("default")
	kopiaSecret := newKopiaPasswordSecret("default")

	mockClient := &mockRepoClient{
		healthStats: &RepositoryStats{RepositorySize: 1024000, SnapshotCount: 5},
	}
	r := newTestReconciler(scheme, mockClient, repo, s3Secret, kopiaSecret)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-repo", Namespace: "default"},
	})

	assert.NoError(t, err)
	assert.Equal(t, defaultHealthCheckInterval, result.RequeueAfter)
	assert.Equal(t, 1, mockClient.healthCalled)

	var updated drv1alpha1.BackupRepository
	err = r.Get(context.Background(), types.NamespacedName{Name: "test-repo", Namespace: "default"}, &updated)
	require.NoError(t, err)
	assert.Equal(t, drv1alpha1.BackupRepositoryStateReady, updated.Status.State)
	assert.Equal(t, int64(1024000), updated.Status.RepositorySize)
	assert.Equal(t, int32(5), updated.Status.SnapshotCount)
}

func TestReconcile_ReadyState_HealthCheckFailure(t *testing.T) {
	scheme := newTestScheme()
	repo := newTestBackupRepository("test-repo", "default", drv1alpha1.BackupRepositoryStateReady)
	repo.Finalizers = []string{BackupRepositoryFinalizerName}
	lastMaint := metav1.NewTime(time.Now())
	repo.Status.LastMaintenanceTime = &lastMaint
	s3Secret := newS3CredentialsSecret("default")
	kopiaSecret := newKopiaPasswordSecret("default")

	mockClient := &mockRepoClient{healthErr: fmt.Errorf("connection timeout")}
	r := newTestReconciler(scheme, mockClient, repo, s3Secret, kopiaSecret)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-repo", Namespace: "default"},
	})

	assert.NoError(t, err)
	assert.Equal(t, defaultRequeueOnError, result.RequeueAfter)

	var updated drv1alpha1.BackupRepository
	err = r.Get(context.Background(), types.NamespacedName{Name: "test-repo", Namespace: "default"}, &updated)
	require.NoError(t, err)
	assert.Equal(t, drv1alpha1.BackupRepositoryStateError, updated.Status.State)
}

func TestReconcile_ReadyState_MaintenanceDue(t *testing.T) {
	scheme := newTestScheme()
	repo := newTestBackupRepository("test-repo", "default", drv1alpha1.BackupRepositoryStateReady)
	repo.Finalizers = []string{BackupRepositoryFinalizerName}
	// Set last maintenance to 2 days ago so it's due
	lastMaint := metav1.NewTime(time.Now().Add(-48 * time.Hour))
	repo.Status.LastMaintenanceTime = &lastMaint
	s3Secret := newS3CredentialsSecret("default")
	kopiaSecret := newKopiaPasswordSecret("default")

	mockClient := &mockRepoClient{
		healthStats: &RepositoryStats{RepositorySize: 500, SnapshotCount: 2},
	}
	r := newTestReconciler(scheme, mockClient, repo, s3Secret, kopiaSecret)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-repo", Namespace: "default"},
	})

	assert.NoError(t, err)
	assert.Equal(t, defaultHealthCheckInterval, result.RequeueAfter)
	assert.Equal(t, 1, mockClient.healthCalled)
	assert.Equal(t, 1, mockClient.maintenanceCalled)

	var updated drv1alpha1.BackupRepository
	err = r.Get(context.Background(), types.NamespacedName{Name: "test-repo", Namespace: "default"}, &updated)
	require.NoError(t, err)
	assert.Equal(t, drv1alpha1.BackupRepositoryStateReady, updated.Status.State)
	assert.NotNil(t, updated.Status.LastMaintenanceTime)
}

func TestReconcile_ErrorState_Recovery(t *testing.T) {
	scheme := newTestScheme()
	repo := newTestBackupRepository("test-repo", "default", drv1alpha1.BackupRepositoryStateError)
	repo.Finalizers = []string{BackupRepositoryFinalizerName}
	s3Secret := newS3CredentialsSecret("default")
	kopiaSecret := newKopiaPasswordSecret("default")

	mockClient := &mockRepoClient{
		healthStats: &RepositoryStats{RepositorySize: 2048, SnapshotCount: 3},
	}
	r := newTestReconciler(scheme, mockClient, repo, s3Secret, kopiaSecret)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-repo", Namespace: "default"},
	})

	assert.NoError(t, err)
	assert.Equal(t, defaultHealthCheckInterval, result.RequeueAfter)
	assert.Equal(t, 1, mockClient.healthCalled)

	var updated drv1alpha1.BackupRepository
	err = r.Get(context.Background(), types.NamespacedName{Name: "test-repo", Namespace: "default"}, &updated)
	require.NoError(t, err)
	assert.Equal(t, drv1alpha1.BackupRepositoryStateReady, updated.Status.State)
	assert.Equal(t, "Repository is healthy", updated.Status.Message)
}

func TestReconcile_ErrorState_StillFailing(t *testing.T) {
	scheme := newTestScheme()
	repo := newTestBackupRepository("test-repo", "default", drv1alpha1.BackupRepositoryStateError)
	repo.Finalizers = []string{BackupRepositoryFinalizerName}
	s3Secret := newS3CredentialsSecret("default")
	kopiaSecret := newKopiaPasswordSecret("default")

	mockClient := &mockRepoClient{healthErr: fmt.Errorf("still broken")}
	r := newTestReconciler(scheme, mockClient, repo, s3Secret, kopiaSecret)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-repo", Namespace: "default"},
	})

	assert.NoError(t, err)
	assert.Equal(t, defaultRequeueOnError, result.RequeueAfter)

	var updated drv1alpha1.BackupRepository
	err = r.Get(context.Background(), types.NamespacedName{Name: "test-repo", Namespace: "default"}, &updated)
	require.NoError(t, err)
	assert.Equal(t, drv1alpha1.BackupRepositoryStateError, updated.Status.State)
}

func TestReconcile_MaintenanceState_Success(t *testing.T) {
	scheme := newTestScheme()
	repo := newTestBackupRepository("test-repo", "default", drv1alpha1.BackupRepositoryStateMaintenance)
	repo.Finalizers = []string{BackupRepositoryFinalizerName}
	s3Secret := newS3CredentialsSecret("default")
	kopiaSecret := newKopiaPasswordSecret("default")

	mockClient := &mockRepoClient{}
	r := newTestReconciler(scheme, mockClient, repo, s3Secret, kopiaSecret)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-repo", Namespace: "default"},
	})

	assert.NoError(t, err)
	assert.Equal(t, defaultHealthCheckInterval, result.RequeueAfter)
	assert.Equal(t, 1, mockClient.maintenanceCalled)

	var updated drv1alpha1.BackupRepository
	err = r.Get(context.Background(), types.NamespacedName{Name: "test-repo", Namespace: "default"}, &updated)
	require.NoError(t, err)
	assert.Equal(t, drv1alpha1.BackupRepositoryStateReady, updated.Status.State)
	assert.NotNil(t, updated.Status.LastMaintenanceTime)
}

func TestReconcile_MaintenanceState_Failure(t *testing.T) {
	scheme := newTestScheme()
	repo := newTestBackupRepository("test-repo", "default", drv1alpha1.BackupRepositoryStateMaintenance)
	repo.Finalizers = []string{BackupRepositoryFinalizerName}
	s3Secret := newS3CredentialsSecret("default")
	kopiaSecret := newKopiaPasswordSecret("default")

	mockClient := &mockRepoClient{maintenanceErr: fmt.Errorf("maintenance error")}
	r := newTestReconciler(scheme, mockClient, repo, s3Secret, kopiaSecret)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-repo", Namespace: "default"},
	})

	assert.NoError(t, err)
	assert.Equal(t, defaultRequeueOnError, result.RequeueAfter)

	var updated drv1alpha1.BackupRepository
	err = r.Get(context.Background(), types.NamespacedName{Name: "test-repo", Namespace: "default"}, &updated)
	require.NoError(t, err)
	assert.Equal(t, drv1alpha1.BackupRepositoryStateError, updated.Status.State)
}

func TestReconcile_UnknownState_FallsBackToInit(t *testing.T) {
	scheme := newTestScheme()
	repo := newTestBackupRepository("test-repo", "default", drv1alpha1.BackupRepositoryState("Unknown"))
	repo.Finalizers = []string{BackupRepositoryFinalizerName}
	s3Secret := newS3CredentialsSecret("default")
	kopiaSecret := newKopiaPasswordSecret("default")

	mockClient := &mockRepoClient{}
	r := newTestReconciler(scheme, mockClient, repo, s3Secret, kopiaSecret)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-repo", Namespace: "default"},
	})

	assert.NoError(t, err)
	assert.Equal(t, defaultHealthCheckInterval, result.RequeueAfter)
	// Should fall through to initialization
	assert.Equal(t, 1, mockClient.initCalled)
}

func TestReconcile_InitializingState_Explicit(t *testing.T) {
	scheme := newTestScheme()
	repo := newTestBackupRepository("test-repo", "default", drv1alpha1.BackupRepositoryStateInitializing)
	repo.Finalizers = []string{BackupRepositoryFinalizerName}
	s3Secret := newS3CredentialsSecret("default")
	kopiaSecret := newKopiaPasswordSecret("default")

	mockClient := &mockRepoClient{}
	r := newTestReconciler(scheme, mockClient, repo, s3Secret, kopiaSecret)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-repo", Namespace: "default"},
	})

	assert.NoError(t, err)
	assert.Equal(t, defaultHealthCheckInterval, result.RequeueAfter)
	assert.Equal(t, 1, mockClient.initCalled)
}
