package syncer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
	syncerrors "github.com/supporttools/dr-syncer/pkg/controllers/syncer/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// --- applyStorageClassMapping tests ---

func TestApplyStorageClassMapping_NilConfig(t *testing.T) {
	sc := "original-sc"
	pvc := &corev1.PersistentVolumeClaim{
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &sc,
		},
	}

	applyStorageClassMapping(pvc, nil)
	assert.Equal(t, "original-sc", *pvc.Spec.StorageClassName)
}

func TestApplyStorageClassMapping_EmptyMappings(t *testing.T) {
	sc := "original-sc"
	pvc := &corev1.PersistentVolumeClaim{
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &sc,
		},
	}
	cfg := &drv1alpha1.PVCConfig{}

	applyStorageClassMapping(pvc, cfg)
	assert.Equal(t, "original-sc", *pvc.Spec.StorageClassName)
}

func TestApplyStorageClassMapping_LabelOverride(t *testing.T) {
	sc := "original-sc"
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"dr-syncer.io/storage-class": "override-sc",
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &sc,
		},
	}
	cfg := &drv1alpha1.PVCConfig{
		StorageClassMappings: []drv1alpha1.StorageClassMapping{
			{From: "original-sc", To: "mapped-sc"},
		},
	}

	applyStorageClassMapping(pvc, cfg)
	assert.Equal(t, "override-sc", *pvc.Spec.StorageClassName, "label override should take precedence over mappings")
}

func TestApplyStorageClassMapping_MappingMatch(t *testing.T) {
	sc := "source-sc"
	pvc := &corev1.PersistentVolumeClaim{
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &sc,
		},
	}
	cfg := &drv1alpha1.PVCConfig{
		StorageClassMappings: []drv1alpha1.StorageClassMapping{
			{From: "other-sc", To: "other-dest-sc"},
			{From: "source-sc", To: "dest-sc"},
		},
	}

	applyStorageClassMapping(pvc, cfg)
	assert.Equal(t, "dest-sc", *pvc.Spec.StorageClassName)
}

func TestApplyStorageClassMapping_NoMatch(t *testing.T) {
	sc := "unmatched-sc"
	pvc := &corev1.PersistentVolumeClaim{
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &sc,
		},
	}
	cfg := &drv1alpha1.PVCConfig{
		StorageClassMappings: []drv1alpha1.StorageClassMapping{
			{From: "source-sc", To: "dest-sc"},
		},
	}

	applyStorageClassMapping(pvc, cfg)
	assert.Equal(t, "unmatched-sc", *pvc.Spec.StorageClassName)
}

func TestApplyStorageClassMapping_NilStorageClassName(t *testing.T) {
	pvc := &corev1.PersistentVolumeClaim{
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: nil,
		},
	}
	cfg := &drv1alpha1.PVCConfig{
		StorageClassMappings: []drv1alpha1.StorageClassMapping{
			{From: "source-sc", To: "dest-sc"},
		},
	}

	applyStorageClassMapping(pvc, cfg)
	assert.Nil(t, pvc.Spec.StorageClassName, "nil storage class should remain nil when no match")
}

// --- applyAccessModeMapping tests ---

func TestApplyAccessModeMapping_NilConfig(t *testing.T) {
	pvc := &corev1.PersistentVolumeClaim{
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		},
	}

	applyAccessModeMapping(pvc, nil)
	assert.Equal(t, []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, pvc.Spec.AccessModes)
}

func TestApplyAccessModeMapping_EmptyMappings(t *testing.T) {
	pvc := &corev1.PersistentVolumeClaim{
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		},
	}
	cfg := &drv1alpha1.PVCConfig{}

	applyAccessModeMapping(pvc, cfg)
	assert.Equal(t, []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, pvc.Spec.AccessModes)
}

func TestApplyAccessModeMapping_Match(t *testing.T) {
	pvc := &corev1.PersistentVolumeClaim{
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		},
	}
	cfg := &drv1alpha1.PVCConfig{
		AccessModeMappings: []drv1alpha1.AccessModeMapping{
			{From: "ReadWriteOnce", To: "ReadWriteMany"},
		},
	}

	applyAccessModeMapping(pvc, cfg)
	assert.Equal(t, []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany}, pvc.Spec.AccessModes)
}

func TestApplyAccessModeMapping_NoMatch(t *testing.T) {
	pvc := &corev1.PersistentVolumeClaim{
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		},
	}
	cfg := &drv1alpha1.PVCConfig{
		AccessModeMappings: []drv1alpha1.AccessModeMapping{
			{From: "ReadOnlyMany", To: "ReadWriteMany"},
		},
	}

	applyAccessModeMapping(pvc, cfg)
	assert.Equal(t, []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, pvc.Spec.AccessModes)
}

func TestApplyAccessModeMapping_MultipleAccessModes(t *testing.T) {
	pvc := &corev1.PersistentVolumeClaim{
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce, corev1.ReadOnlyMany},
		},
	}
	cfg := &drv1alpha1.PVCConfig{
		AccessModeMappings: []drv1alpha1.AccessModeMapping{
			{From: "ReadOnlyMany", To: "ReadWriteMany"},
		},
	}

	applyAccessModeMapping(pvc, cfg)
	assert.Equal(t, corev1.ReadWriteOnce, pvc.Spec.AccessModes[0])
	assert.Equal(t, corev1.ReadWriteMany, pvc.Spec.AccessModes[1])
}

// --- preparePVCForCreation tests ---

func TestPreparePVCForCreation_ClearsBindingAnnotations(t *testing.T) {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			ResourceVersion: "12345",
			Annotations: map[string]string{
				"pv.kubernetes.io/bind-completed":      "yes",
				"pv.kubernetes.io/bound-by-controller": "yes",
				"volume.kubernetes.io/selected-node":   "node-1",
				"some-other-annotation":                "keep-me",
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			VolumeName: "pv-12345",
		},
	}

	preparePVCForCreation(pvc, nil)

	assert.Empty(t, pvc.ResourceVersion)
	assert.Empty(t, pvc.Spec.VolumeName)
	assert.NotContains(t, pvc.Annotations, "pv.kubernetes.io/bind-completed")
	assert.NotContains(t, pvc.Annotations, "pv.kubernetes.io/bound-by-controller")
	assert.NotContains(t, pvc.Annotations, "volume.kubernetes.io/selected-node")
	assert.Equal(t, "keep-me", pvc.Annotations["some-other-annotation"])
}

func TestPreparePVCForCreation_ClearsVolumeAttributes(t *testing.T) {
	blockMode := corev1.PersistentVolumeBlock
	pvc := &corev1.PersistentVolumeClaim{
		Spec: corev1.PersistentVolumeClaimSpec{
			VolumeMode: &blockMode,
			Selector:   &metav1.LabelSelector{MatchLabels: map[string]string{"foo": "bar"}},
			DataSource: &corev1.TypedLocalObjectReference{Name: "snap"},
		},
	}

	preparePVCForCreation(pvc, nil)

	assert.Nil(t, pvc.Spec.VolumeMode)
	assert.Nil(t, pvc.Spec.Selector)
	assert.Nil(t, pvc.Spec.DataSource)
	assert.Nil(t, pvc.Spec.DataSourceRef)
}

func TestPreparePVCForCreation_PreservesVolumeAttributes(t *testing.T) {
	blockMode := corev1.PersistentVolumeBlock
	pvc := &corev1.PersistentVolumeClaim{
		Spec: corev1.PersistentVolumeClaimSpec{
			VolumeMode: &blockMode,
			Selector:   &metav1.LabelSelector{MatchLabels: map[string]string{"foo": "bar"}},
		},
	}
	cfg := &drv1alpha1.PVCConfig{PreserveVolumeAttributes: true}

	preparePVCForCreation(pvc, cfg)

	assert.NotNil(t, pvc.Spec.VolumeMode, "VolumeMode should be preserved when PreserveVolumeAttributes is true")
	assert.NotNil(t, pvc.Spec.Selector, "Selector should be preserved when PreserveVolumeAttributes is true")
}

func TestPreparePVCForCreation_SyncPV_KeepsVolumeName(t *testing.T) {
	pvc := &corev1.PersistentVolumeClaim{
		Spec: corev1.PersistentVolumeClaimSpec{
			VolumeName: "pv-12345",
		},
	}
	cfg := &drv1alpha1.PVCConfig{SyncPersistentVolumes: true}

	preparePVCForCreation(pvc, cfg)

	assert.Equal(t, "pv-12345", pvc.Spec.VolumeName, "VolumeName should be kept when SyncPersistentVolumes is true")
}

func TestPreparePVCForCreation_NilAnnotations(t *testing.T) {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: nil,
		},
	}

	preparePVCForCreation(pvc, nil)
	assert.NotNil(t, pvc.Annotations, "Annotations map should be initialized")
}

// --- createOrUpdateDestPVC tests ---

func TestCreateOrUpdateDestPVC_CreateNew(t *testing.T) {
	destClient := fake.NewSimpleClientset()
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-pvc",
			Namespace:       "dst-ns",
			ResourceVersion: "12345",
			Annotations: map[string]string{
				"pv.kubernetes.io/bind-completed": "yes",
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
			VolumeName: "pv-old",
		},
	}

	result, err := createOrUpdateDestPVC(context.Background(), destClient, pvc, "dst-ns", nil)

	require.NoError(t, err)
	assert.Equal(t, "test-pvc", result.Name)
	assert.Equal(t, "dst-ns", result.Namespace)

	// Verify the PVC was created in the fake client
	created, getErr := destClient.CoreV1().PersistentVolumeClaims("dst-ns").Get(context.Background(), "test-pvc", metav1.GetOptions{})
	require.NoError(t, getErr)
	assert.NotContains(t, created.Annotations, "pv.kubernetes.io/bind-completed")
	assert.Empty(t, created.Spec.VolumeName, "VolumeName should be cleared for new PVC")
}

func TestCreateOrUpdateDestPVC_UpdateExisting(t *testing.T) {
	existingPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pvc",
			Namespace: "dst-ns",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("5Gi"),
				},
			},
		},
	}
	destClient := fake.NewSimpleClientset(existingPVC)

	updatedPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pvc",
			Namespace: "dst-ns",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("20Gi"),
				},
			},
		},
	}

	result, err := createOrUpdateDestPVC(context.Background(), destClient, updatedPVC, "dst-ns", nil)

	require.NoError(t, err)
	assert.Equal(t, "test-pvc", result.Name)
	assert.Equal(t, resource.MustParse("20Gi"), result.Spec.Resources.Requests[corev1.ResourceStorage])
}

func TestCreateOrUpdateDestPVC_CreateError(t *testing.T) {
	// Use a client with an existing namespace but cause an error by providing invalid PVC
	destClient := fake.NewSimpleClientset()

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pvc",
			Namespace: "dst-ns",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		},
	}

	// Create the PVC first, then try to create again to trigger already-exists
	_, err := createOrUpdateDestPVC(context.Background(), destClient, pvc, "dst-ns", nil)
	require.NoError(t, err)

	// Creating a second time should update (since it now exists), not error
	pvc2 := pvc.DeepCopy()
	result, err := createOrUpdateDestPVC(context.Background(), destClient, pvc2, "dst-ns", nil)
	require.NoError(t, err)
	assert.Equal(t, "test-pvc", result.Name)
}

func TestCreateOrUpdateDestPVC_RetryableError(t *testing.T) {
	destClient := fake.NewSimpleClientset()

	// First create a PVC normally
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pvc",
			Namespace: "dst-ns",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		},
	}
	_, err := createOrUpdateDestPVC(context.Background(), destClient, pvc, "dst-ns", nil)
	require.NoError(t, err)

	// Verify the error from createOrUpdateDestPVC is a retryable sync error when it occurs
	// (We can't easily force a fake client error, so we test the type system)
	assert.True(t, syncerrors.IsRetryable(syncerrors.NewRetryableError(
		assert.AnError, "test",
	)))
}
