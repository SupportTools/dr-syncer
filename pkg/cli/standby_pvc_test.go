package cli

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
)

func TestListStandbyPVCs_NoResults(t *testing.T) {
	client := fakeclientset.NewSimpleClientset()
	pvcs, err := listStandbyPVCs(context.Background(), client, "test-ns")
	require.NoError(t, err)
	assert.Empty(t, pvcs)
}

func TestListStandbyPVCs_FindsLabeledPVCs(t *testing.T) {
	restoreTime := time.Now().UTC().Format(time.RFC3339)
	objs := []runtime.Object{
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "data-standby",
				Namespace: "dr-ns",
				Labels: map[string]string{
					labelStandby:   "true",
					labelManagedBy: managedByValue,
					labelSourcePVC: "data",
				},
				Annotations: map[string]string{
					annoLastRestoreTime: restoreTime,
					annoLastRestoreSnap: "snap-abc123",
				},
			},
			Status: corev1.PersistentVolumeClaimStatus{
				Phase: corev1.ClaimBound,
			},
		},
		// A non-standby PVC that should NOT be returned.
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "regular-pvc",
				Namespace: "dr-ns",
			},
			Status: corev1.PersistentVolumeClaimStatus{
				Phase: corev1.ClaimBound,
			},
		},
	}

	client := fakeclientset.NewSimpleClientset(objs...)
	pvcs, err := listStandbyPVCs(context.Background(), client, "dr-ns")
	require.NoError(t, err)
	require.Len(t, pvcs, 1)

	assert.Equal(t, "data-standby", pvcs[0].Name)
	assert.Equal(t, "data", pvcs[0].SourcePVCName)
	assert.Equal(t, "snap-abc123", pvcs[0].SnapshotID)
	assert.NotNil(t, pvcs[0].LastRestoreTime)
	assert.Equal(t, corev1.ClaimBound, pvcs[0].Phase)
}

func TestListStandbyPVCs_DifferentNamespace(t *testing.T) {
	objs := []runtime.Object{
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "data-standby",
				Namespace: "other-ns",
				Labels: map[string]string{
					labelStandby:   "true",
					labelManagedBy: managedByValue,
				},
			},
		},
	}

	client := fakeclientset.NewSimpleClientset(objs...)
	pvcs, err := listStandbyPVCs(context.Background(), client, "dr-ns")
	require.NoError(t, err)
	assert.Empty(t, pvcs)
}

func TestRewriteVolumes_SwapsClaims(t *testing.T) {
	podSpec := &corev1.PodSpec{
		Volumes: []corev1.Volume{
			{
				Name: "data-vol",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "data",
					},
				},
			},
			{
				Name: "config-vol",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "config"},
					},
				},
			},
			{
				Name: "logs-vol",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "logs",
					},
				},
			},
		},
	}

	mapping := map[string]string{
		"data": "data-standby",
		"logs": "logs-standby",
	}

	changed := rewriteVolumes(podSpec, mapping)
	assert.True(t, changed)
	assert.Equal(t, "data-standby", podSpec.Volumes[0].PersistentVolumeClaim.ClaimName)
	assert.Equal(t, "logs-standby", podSpec.Volumes[2].PersistentVolumeClaim.ClaimName)
}

func TestRewriteVolumes_NoMatch(t *testing.T) {
	podSpec := &corev1.PodSpec{
		Volumes: []corev1.Volume{
			{
				Name: "data-vol",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "other-pvc",
					},
				},
			},
		},
	}

	mapping := map[string]string{
		"data": "data-standby",
	}

	changed := rewriteVolumes(podSpec, mapping)
	assert.False(t, changed)
	assert.Equal(t, "other-pvc", podSpec.Volumes[0].PersistentVolumeClaim.ClaimName)
}

func TestRewriteVolumes_EmptyVolumes(t *testing.T) {
	podSpec := &corev1.PodSpec{
		Volumes: []corev1.Volume{},
	}

	mapping := map[string]string{
		"data": "data-standby",
	}

	changed := rewriteVolumes(podSpec, mapping)
	assert.False(t, changed)
}

func TestBuildStandbyPVCName(t *testing.T) {
	assert.Equal(t, "data-standby", buildStandbyPVCName("data"))
	assert.Equal(t, "my-app-data-standby", buildStandbyPVCName("my-app-data"))
}

func TestIsStandbyPVCName(t *testing.T) {
	assert.True(t, isStandbyPVCName("data-standby"))
	assert.True(t, isStandbyPVCName("my-app-data-standby"))
	assert.False(t, isStandbyPVCName("data"))
	assert.False(t, isStandbyPVCName("standby-data"))
}

func TestGetSourcePVCName(t *testing.T) {
	assert.Equal(t, "data", getSourcePVCName("data-standby"))
	assert.Equal(t, "my-app-data", getSourcePVCName("my-app-data-standby"))
}

func TestPodSpecHasPVCs(t *testing.T) {
	withPVC := &corev1.PodSpec{
		Volumes: []corev1.Volume{
			{
				Name: "data",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "data",
					},
				},
			},
		},
	}
	assert.True(t, podSpecHasPVCs(withPVC))

	withoutPVC := &corev1.PodSpec{
		Volumes: []corev1.Volume{
			{
				Name: "config",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "config"},
					},
				},
			},
		},
	}
	assert.False(t, podSpecHasPVCs(withoutPVC))

	empty := &corev1.PodSpec{}
	assert.False(t, podSpecHasPVCs(empty))
}

func TestReportStandbyPVCStatus_NoPVCs(t *testing.T) {
	// Should not panic with empty slice.
	reportStandbyPVCStatus(nil)
	reportStandbyPVCStatus([]StandbyPVCInfo{})
}

func TestReportStandbyPVCStatus_WithPVCs(t *testing.T) {
	now := time.Now().UTC()
	pvcs := []StandbyPVCInfo{
		{
			Name:            "data-standby",
			SourcePVCName:   "data",
			LastRestoreTime: &now,
			SnapshotID:      "snap-123",
			Phase:           corev1.ClaimBound,
		},
		{
			Name:          "logs-standby",
			SourcePVCName: "logs",
			Phase:         corev1.ClaimPending,
		},
	}
	// Should not panic; just exercises the log output paths.
	reportStandbyPVCStatus(pvcs)
}

func TestConfig_UseStandbyPVCs(t *testing.T) {
	config := &Config{
		UseStandbyPVCs: true,
	}
	assert.True(t, config.UseStandbyPVCs)

	config2 := &Config{}
	assert.False(t, config2.UseStandbyPVCs)
}
