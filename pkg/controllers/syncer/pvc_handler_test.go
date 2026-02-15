package syncer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
	syncerrors "github.com/supporttools/dr-syncer/pkg/controllers/syncer/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// --- isBackupPathEnabled tests ---

func TestIsBackupPathEnabled_NilPVCConfig(t *testing.T) {
	assert.False(t, isBackupPathEnabled(nil))
}

func TestIsBackupPathEnabled_NilDataSyncConfig(t *testing.T) {
	cfg := &drv1alpha1.PVCConfig{}
	assert.False(t, isBackupPathEnabled(cfg))
}

func TestIsBackupPathEnabled_NilBackupConfig(t *testing.T) {
	cfg := &drv1alpha1.PVCConfig{
		DataSyncConfig: &drv1alpha1.PVCDataSyncConfig{},
	}
	assert.False(t, isBackupPathEnabled(cfg))
}

func TestIsBackupPathEnabled_Disabled(t *testing.T) {
	cfg := &drv1alpha1.PVCConfig{
		DataSyncConfig: &drv1alpha1.PVCDataSyncConfig{
			BackupConfig: &drv1alpha1.BackupConfig{
				Enabled: false,
			},
		},
	}
	assert.False(t, isBackupPathEnabled(cfg))
}

func TestIsBackupPathEnabled_Enabled(t *testing.T) {
	cfg := &drv1alpha1.PVCConfig{
		DataSyncConfig: &drv1alpha1.PVCDataSyncConfig{
			BackupConfig: &drv1alpha1.BackupConfig{
				Enabled: true,
			},
		},
	}
	assert.True(t, isBackupPathEnabled(cfg))
}

// --- routePVCSync tests ---

func TestRoutePVCSync_RsyncPath_NilBackupConfig(t *testing.T) {
	// When BackupConfig is nil, should take the rsync path.
	// We verify by checking that backupSyncFunc is NOT called.
	backupCalled := false
	backupFn := func(_ context.Context, _ *drv1alpha1.NamespaceMapping,
		_ []corev1.PersistentVolumeClaim, _ *drv1alpha1.BackupConfig) []BackupPVCSyncResult {
		backupCalled = true
		return nil
	}

	sourceClient := fake.NewSimpleClientset()
	destClient := fake.NewSimpleClientset()
	syncer := &ResourceSyncer{}

	// Create source namespace for the rsync path to find
	_, err := sourceClient.CoreV1().Namespaces().Create(context.Background(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "src-ns"},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	// routePVCSync with nil PVCConfig → rsync path
	// The rsync path will fail because the ResourceSyncer is not fully configured,
	// but we only care that the backup path was NOT taken.
	_ = routePVCSync(context.Background(), syncer, sourceClient, destClient,
		"src-ns", "dst-ns", nil, nil, nil, backupFn)

	assert.False(t, backupCalled, "backup sync function should not be called when BackupConfig is nil")
}

func TestRoutePVCSync_RsyncPath_BackupDisabled(t *testing.T) {
	backupCalled := false
	backupFn := func(_ context.Context, _ *drv1alpha1.NamespaceMapping,
		_ []corev1.PersistentVolumeClaim, _ *drv1alpha1.BackupConfig) []BackupPVCSyncResult {
		backupCalled = true
		return nil
	}

	pvcConfig := &drv1alpha1.PVCConfig{
		DataSyncConfig: &drv1alpha1.PVCDataSyncConfig{
			BackupConfig: &drv1alpha1.BackupConfig{
				Enabled: false,
			},
		},
	}

	sourceClient := fake.NewSimpleClientset()
	destClient := fake.NewSimpleClientset()
	syncer := &ResourceSyncer{}

	_ = routePVCSync(context.Background(), syncer, sourceClient, destClient,
		"src-ns", "dst-ns", pvcConfig, nil, nil, backupFn)

	assert.False(t, backupCalled, "backup sync function should not be called when BackupConfig.Enabled is false")
}

func TestRoutePVCSync_BackupPath_Enabled(t *testing.T) {
	backupCalled := false
	var receivedMapping *drv1alpha1.NamespaceMapping
	var receivedConfig *drv1alpha1.BackupConfig

	backupFn := func(_ context.Context, mapping *drv1alpha1.NamespaceMapping,
		_ []corev1.PersistentVolumeClaim, cfg *drv1alpha1.BackupConfig) []BackupPVCSyncResult {
		backupCalled = true
		receivedMapping = mapping
		receivedConfig = cfg
		return nil
	}

	pvcConfig := &drv1alpha1.PVCConfig{
		SyncData: true,
		DataSyncConfig: &drv1alpha1.PVCDataSyncConfig{
			BackupConfig: &drv1alpha1.BackupConfig{
				Enabled: true,
			},
		},
	}

	mapping := &drv1alpha1.NamespaceMapping{
		ObjectMeta: metav1.ObjectMeta{Name: "test-mapping"},
	}

	// Create fake clients with a source PVC so the backup path has something to process
	sourceClient := fake.NewSimpleClientset(&corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pvc", Namespace: "src-ns"},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		},
	})
	destClient := fake.NewSimpleClientset()
	syncer := &ResourceSyncer{
		destClient: destClient,
	}

	err := routePVCSync(context.Background(), syncer, sourceClient, destClient,
		"src-ns", "dst-ns", pvcConfig, nil, mapping, backupFn)

	assert.NoError(t, err)
	assert.True(t, backupCalled, "backup sync function should be called when BackupConfig.Enabled is true")
	assert.Equal(t, "test-mapping", receivedMapping.Name)
	assert.True(t, receivedConfig.Enabled)
}

func TestRoutePVCSync_BackupPath_NilFunc_ReturnsError(t *testing.T) {
	pvcConfig := &drv1alpha1.PVCConfig{
		DataSyncConfig: &drv1alpha1.PVCDataSyncConfig{
			BackupConfig: &drv1alpha1.BackupConfig{
				Enabled: true,
			},
		},
	}

	sourceClient := fake.NewSimpleClientset()
	destClient := fake.NewSimpleClientset()
	syncer := &ResourceSyncer{}

	err := routePVCSync(context.Background(), syncer, sourceClient, destClient,
		"src-ns", "dst-ns", pvcConfig, nil, nil, nil)

	require.Error(t, err)
	assert.False(t, syncerrors.IsRetryable(err), "should return NonRetryableError when backup func is nil")
	assert.Contains(t, err.Error(), "no backup sync function provided")
}
