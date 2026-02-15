package cli

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
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

// --- Integration tests for Kubernetes API functions ---

// newDeploymentWithPVC creates a deployment with a PVC volume for testing.
func newDeploymentWithPVC(name, namespace, pvcName string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app", Image: "busybox"}},
					Volumes: []corev1.Volume{
						{
							Name: "data-vol",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: pvcName,
								},
							},
						},
					},
				},
			},
		},
	}
}

// newStatefulSetWithPVC creates a statefulset with a PVC volume for testing.
func newStatefulSetWithPVC(name, namespace, pvcName string) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app", Image: "busybox"}},
					Volumes: []corev1.Volume{
						{
							Name: "data-vol",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: pvcName,
								},
							},
						},
					},
				},
			},
		},
	}
}

// newStandbyPVC creates a standby PVC object with standard labels/annotations.
func newStandbyPVC(name, namespace, sourceName string, phase corev1.PersistentVolumeClaimPhase, restored bool) *corev1.PersistentVolumeClaim {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				labelStandby:   "true",
				labelManagedBy: managedByValue,
				labelSourcePVC: sourceName,
			},
		},
		Status: corev1.PersistentVolumeClaimStatus{Phase: phase},
	}
	if restored {
		pvc.Annotations = map[string]string{
			annoLastRestoreTime: time.Now().UTC().Format(time.RFC3339),
			annoLastRestoreSnap: "snap-test-123",
		}
	}
	return pvc
}

func TestUpdateWorkloadPVCReferences_DeploymentAndStatefulSet(t *testing.T) {
	ns := "dr-ns"
	objs := []runtime.Object{
		newDeploymentWithPVC("web", ns, "data"),
		newStatefulSetWithPVC("db", ns, "dbdata"),
	}
	client := fakeclientset.NewSimpleClientset(objs...)
	ctx := context.Background()

	standbyPVCs := []StandbyPVCInfo{
		{Name: "data-standby", SourcePVCName: "data"},
		{Name: "dbdata-standby", SourcePVCName: "dbdata"},
	}

	count, err := updateWorkloadPVCReferences(ctx, client, ns, standbyPVCs)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	// Verify deployment was updated.
	dep, err := client.AppsV1().Deployments(ns).Get(ctx, "web", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "data-standby", dep.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName)

	// Verify statefulset was updated.
	sts, err := client.AppsV1().StatefulSets(ns).Get(ctx, "db", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "dbdata-standby", sts.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName)
}

func TestUpdateWorkloadPVCReferences_EmptyNamespace(t *testing.T) {
	client := fakeclientset.NewSimpleClientset()
	ctx := context.Background()

	standbyPVCs := []StandbyPVCInfo{
		{Name: "data-standby", SourcePVCName: "data"},
	}

	count, err := updateWorkloadPVCReferences(ctx, client, "empty-ns", standbyPVCs)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestUpdateWorkloadPVCReferences_NoMatchingPVCs(t *testing.T) {
	ns := "dr-ns"
	objs := []runtime.Object{
		newDeploymentWithPVC("web", ns, "other-pvc"),
	}
	client := fakeclientset.NewSimpleClientset(objs...)
	ctx := context.Background()

	standbyPVCs := []StandbyPVCInfo{
		{Name: "data-standby", SourcePVCName: "data"},
	}

	count, err := updateWorkloadPVCReferences(ctx, client, ns, standbyPVCs)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestUpdateWorkloadPVCReferences_EmptyMappings(t *testing.T) {
	ns := "dr-ns"
	objs := []runtime.Object{
		newDeploymentWithPVC("web", ns, "data"),
	}
	client := fakeclientset.NewSimpleClientset(objs...)
	ctx := context.Background()

	// Standby PVCs with no source names produce empty mapping.
	standbyPVCs := []StandbyPVCInfo{
		{Name: "data-standby", SourcePVCName: ""},
	}

	count, err := updateWorkloadPVCReferences(ctx, client, ns, standbyPVCs)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestRevertWorkloadPVCReferences_DeploymentAndStatefulSet(t *testing.T) {
	ns := "dr-ns"
	// Start with workloads already pointing to standby PVCs.
	objs := []runtime.Object{
		newDeploymentWithPVC("web", ns, "data-standby"),
		newStatefulSetWithPVC("db", ns, "dbdata-standby"),
	}
	client := fakeclientset.NewSimpleClientset(objs...)
	ctx := context.Background()

	standbyPVCs := []StandbyPVCInfo{
		{Name: "data-standby", SourcePVCName: "data"},
		{Name: "dbdata-standby", SourcePVCName: "dbdata"},
	}

	count, err := revertWorkloadPVCReferences(ctx, client, ns, standbyPVCs)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	// Verify deployment was reverted.
	dep, err := client.AppsV1().Deployments(ns).Get(ctx, "web", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "data", dep.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName)

	// Verify statefulset was reverted.
	sts, err := client.AppsV1().StatefulSets(ns).Get(ctx, "db", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "dbdata", sts.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName)
}

func TestRevertWorkloadPVCReferences_EmptyNamespace(t *testing.T) {
	client := fakeclientset.NewSimpleClientset()
	ctx := context.Background()

	standbyPVCs := []StandbyPVCInfo{
		{Name: "data-standby", SourcePVCName: "data"},
	}

	count, err := revertWorkloadPVCReferences(ctx, client, "empty-ns", standbyPVCs)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestRevertWorkloadPVCReferences_EmptyMappings(t *testing.T) {
	ns := "dr-ns"
	objs := []runtime.Object{
		newDeploymentWithPVC("web", ns, "data-standby"),
	}
	client := fakeclientset.NewSimpleClientset(objs...)
	ctx := context.Background()

	standbyPVCs := []StandbyPVCInfo{
		{Name: "data-standby", SourcePVCName: ""},
	}

	count, err := revertWorkloadPVCReferences(ctx, client, ns, standbyPVCs)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestValidateStandbyPVCsForCutover_AllReady(t *testing.T) {
	ns := "dr-ns"
	objs := []runtime.Object{
		newStandbyPVC("data-standby", ns, "data", corev1.ClaimBound, true),
		newStandbyPVC("logs-standby", ns, "logs", corev1.ClaimBound, true),
	}
	destClient := fakeclientset.NewSimpleClientset(objs...)
	sourceClient := fakeclientset.NewSimpleClientset()
	ctx := context.Background()

	pvcs, err := validateStandbyPVCsForCutover(ctx, sourceClient, destClient, "src-ns", ns)
	require.NoError(t, err)
	assert.Len(t, pvcs, 2)
}

func TestValidateStandbyPVCsForCutover_UnboundPVC(t *testing.T) {
	ns := "dr-ns"
	objs := []runtime.Object{
		newStandbyPVC("data-standby", ns, "data", corev1.ClaimPending, true),
	}
	destClient := fakeclientset.NewSimpleClientset(objs...)
	sourceClient := fakeclientset.NewSimpleClientset()
	ctx := context.Background()

	// Should still return PVCs (warnings are logged, not errors).
	pvcs, err := validateStandbyPVCsForCutover(ctx, sourceClient, destClient, "src-ns", ns)
	require.NoError(t, err)
	assert.Len(t, pvcs, 1)
	assert.Equal(t, corev1.ClaimPending, pvcs[0].Phase)
}

func TestValidateStandbyPVCsForCutover_NeverRestored(t *testing.T) {
	ns := "dr-ns"
	objs := []runtime.Object{
		newStandbyPVC("data-standby", ns, "data", corev1.ClaimBound, false),
	}
	destClient := fakeclientset.NewSimpleClientset(objs...)
	sourceClient := fakeclientset.NewSimpleClientset()
	ctx := context.Background()

	// Should still return PVCs (warnings are logged, not errors).
	pvcs, err := validateStandbyPVCsForCutover(ctx, sourceClient, destClient, "src-ns", ns)
	require.NoError(t, err)
	assert.Len(t, pvcs, 1)
	assert.Nil(t, pvcs[0].LastRestoreTime)
}

func TestValidateStandbyPVCsForCutover_NoStandbyPVCs(t *testing.T) {
	destClient := fakeclientset.NewSimpleClientset()
	sourceClient := fakeclientset.NewSimpleClientset()
	ctx := context.Background()

	pvcs, err := validateStandbyPVCsForCutover(ctx, sourceClient, destClient, "src-ns", "dr-ns")
	assert.Error(t, err)
	assert.Nil(t, pvcs)
	assert.Contains(t, err.Error(), "no standby PVCs found")
}

func TestUpdateAndRevertRoundTrip(t *testing.T) {
	ns := "dr-ns"
	objs := []runtime.Object{
		newDeploymentWithPVC("web", ns, "data"),
		newStatefulSetWithPVC("db", ns, "dbdata"),
	}
	client := fakeclientset.NewSimpleClientset(objs...)
	ctx := context.Background()

	standbyPVCs := []StandbyPVCInfo{
		{Name: "data-standby", SourcePVCName: "data"},
		{Name: "dbdata-standby", SourcePVCName: "dbdata"},
	}

	// Update to standby.
	count, err := updateWorkloadPVCReferences(ctx, client, ns, standbyPVCs)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	// Revert back to original.
	count, err = revertWorkloadPVCReferences(ctx, client, ns, standbyPVCs)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	// Verify originals restored.
	dep, err := client.AppsV1().Deployments(ns).Get(ctx, "web", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "data", dep.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName)

	sts, err := client.AppsV1().StatefulSets(ns).Get(ctx, "db", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "dbdata", sts.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName)
}

// --- getWorkloadsUsingPVCs tests ---

func TestGetWorkloadsUsingPVCs_FiltersCorrectly(t *testing.T) {
	ns := "test-ns"
	objs := []runtime.Object{
		// Deployment with a PVC volume — should be returned.
		newDeploymentWithPVC("web", ns, "data"),
		// Deployment without PVC — should NOT be returned.
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: ns},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "worker"}},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "worker"}},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "app", Image: "busybox"}},
						Volumes: []corev1.Volume{
							{
								Name: "config",
								VolumeSource: corev1.VolumeSource{
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{Name: "cfg"},
									},
								},
							},
						},
					},
				},
			},
		},
		// StatefulSet with a PVC volume — should be returned.
		newStatefulSetWithPVC("db", ns, "dbdata"),
		// StatefulSet without PVC — should NOT be returned.
		&appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: "cache", Namespace: ns},
			Spec: appsv1.StatefulSetSpec{
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "cache"}},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "cache"}},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "app", Image: "redis"}},
					},
				},
			},
		},
	}
	client := fakeclientset.NewSimpleClientset(objs...)
	ctx := context.Background()

	deps, stsList, err := getWorkloadsUsingPVCs(ctx, client, ns)
	require.NoError(t, err)
	assert.Len(t, deps, 1)
	assert.Equal(t, "web", deps[0].Name)
	assert.Len(t, stsList, 1)
	assert.Equal(t, "db", stsList[0].Name)
}

func TestGetWorkloadsUsingPVCs_VCTOnlyStatefulSet(t *testing.T) {
	ns := "test-ns"
	objs := []runtime.Object{
		// VCT-only StatefulSet — should be returned.
		newStatefulSetWithVCT("db", ns),
	}
	client := fakeclientset.NewSimpleClientset(objs...)
	ctx := context.Background()

	deps, stsList, err := getWorkloadsUsingPVCs(ctx, client, ns)
	require.NoError(t, err)
	assert.Empty(t, deps)
	assert.Len(t, stsList, 1)
	assert.Equal(t, "db", stsList[0].Name)
}

func TestGetWorkloadsUsingPVCs_MixedVCTAndStaticPVC(t *testing.T) {
	ns := "test-ns"
	objs := []runtime.Object{
		// StatefulSet with both VCTs and static PVCs — should be returned (only once).
		newStatefulSetWithVCTAndPVC("db", ns, "shared-data"),
	}
	client := fakeclientset.NewSimpleClientset(objs...)
	ctx := context.Background()

	deps, stsList, err := getWorkloadsUsingPVCs(ctx, client, ns)
	require.NoError(t, err)
	assert.Empty(t, deps)
	assert.Len(t, stsList, 1)
	assert.Equal(t, "db", stsList[0].Name)
}

func TestGetWorkloadsUsingPVCs_EmptyNamespace(t *testing.T) {
	client := fakeclientset.NewSimpleClientset()
	ctx := context.Background()

	deps, stsList, err := getWorkloadsUsingPVCs(ctx, client, "empty-ns")
	require.NoError(t, err)
	assert.Empty(t, deps)
	assert.Empty(t, stsList)
}

// --- Multi-volume workload tests ---

func TestUpdateWorkloadPVCReferences_MultipleVolumes(t *testing.T) {
	ns := "dr-ns"
	// Deployment with multiple PVC volumes — only matching ones should be rewritten.
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "multi", Namespace: ns},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "multi"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "multi"}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app", Image: "busybox"}},
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
									LocalObjectReference: corev1.LocalObjectReference{Name: "cfg"},
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
						{
							Name: "unmatched-vol",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: "unrelated-pvc",
								},
							},
						},
					},
				},
			},
		},
	}

	client := fakeclientset.NewSimpleClientset(dep)
	ctx := context.Background()

	standbyPVCs := []StandbyPVCInfo{
		{Name: "data-standby", SourcePVCName: "data"},
		{Name: "logs-standby", SourcePVCName: "logs"},
	}

	count, err := updateWorkloadPVCReferences(ctx, client, ns, standbyPVCs)
	require.NoError(t, err)
	assert.Equal(t, 1, count) // One deployment updated.

	got, err := client.AppsV1().Deployments(ns).Get(ctx, "multi", metav1.GetOptions{})
	require.NoError(t, err)

	volumes := got.Spec.Template.Spec.Volumes
	assert.Equal(t, "data-standby", volumes[0].PersistentVolumeClaim.ClaimName)
	assert.Nil(t, volumes[1].PersistentVolumeClaim) // ConfigMap volume unchanged.
	assert.Equal(t, "logs-standby", volumes[2].PersistentVolumeClaim.ClaimName)
	assert.Equal(t, "unrelated-pvc", volumes[3].PersistentVolumeClaim.ClaimName) // Unmatched stays.
}

func TestRevertWorkloadPVCReferences_PartialMatch(t *testing.T) {
	ns := "dr-ns"
	// Deployment with a mix of standby and non-standby PVC volumes.
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "partial", Namespace: ns},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "partial"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "partial"}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app", Image: "busybox"}},
					Volumes: []corev1.Volume{
						{
							Name: "data-vol",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: "data-standby",
								},
							},
						},
						{
							Name: "other-vol",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: "other-pvc",
								},
							},
						},
					},
				},
			},
		},
	}

	client := fakeclientset.NewSimpleClientset(dep)
	ctx := context.Background()

	standbyPVCs := []StandbyPVCInfo{
		{Name: "data-standby", SourcePVCName: "data"},
	}

	count, err := revertWorkloadPVCReferences(ctx, client, ns, standbyPVCs)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	got, err := client.AppsV1().Deployments(ns).Get(ctx, "partial", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "data", got.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName)
	assert.Equal(t, "other-pvc", got.Spec.Template.Spec.Volumes[1].PersistentVolumeClaim.ClaimName)
}

// newStatefulSetWithVCT creates a statefulset that uses volumeClaimTemplates (no static PVC volumes).
func newStatefulSetWithVCT(name, namespace string) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: appsv1.StatefulSetSpec{
			Selector:    &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
			ServiceName: name,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app", Image: "busybox"}},
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "data"},
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
		},
	}
}

// newStatefulSetWithVCTAndPVC creates a statefulset that uses both volumeClaimTemplates AND static PVC volumes.
func newStatefulSetWithVCTAndPVC(name, namespace, pvcName string) *appsv1.StatefulSet {
	sts := newStatefulSetWithVCT(name, namespace)
	sts.Spec.Template.Spec.Volumes = []corev1.Volume{
		{
			Name: "static-vol",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: pvcName,
				},
			},
		},
	}
	return sts
}

func TestHasVolumeClaimTemplates_WithVCTs(t *testing.T) {
	sts := newStatefulSetWithVCT("db", "test-ns")
	assert.True(t, hasVolumeClaimTemplates(sts))
}

func TestHasVolumeClaimTemplates_WithoutVCTs(t *testing.T) {
	sts := newStatefulSetWithPVC("db", "test-ns", "data")
	assert.False(t, hasVolumeClaimTemplates(sts))
}

func TestHasVolumeClaimTemplates_EmptyVCTs(t *testing.T) {
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "test-ns"},
		Spec: appsv1.StatefulSetSpec{
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{},
		},
	}
	assert.False(t, hasVolumeClaimTemplates(sts))
}

func TestUpdateWorkloadPVCReferences_VCTOnlyStatefulSet(t *testing.T) {
	ns := "dr-ns"
	objs := []runtime.Object{
		newStatefulSetWithVCT("db", ns),
	}
	client := fakeclientset.NewSimpleClientset(objs...)
	ctx := context.Background()

	standbyPVCs := []StandbyPVCInfo{
		{Name: "data-standby", SourcePVCName: "data"},
	}

	// VCT-only StatefulSet has no static PVC volumes to rewrite, so count should be 0.
	// Warning is logged but does not cause an error.
	count, err := updateWorkloadPVCReferences(ctx, client, ns, standbyPVCs)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestUpdateWorkloadPVCReferences_MixedVCTAndStaticPVC(t *testing.T) {
	ns := "dr-ns"
	objs := []runtime.Object{
		newStatefulSetWithVCTAndPVC("db", ns, "shared-data"),
	}
	client := fakeclientset.NewSimpleClientset(objs...)
	ctx := context.Background()

	standbyPVCs := []StandbyPVCInfo{
		{Name: "shared-data-standby", SourcePVCName: "shared-data"},
	}

	// The static PVC volume should be rewritten even though VCTs are present.
	count, err := updateWorkloadPVCReferences(ctx, client, ns, standbyPVCs)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Verify the static volume was rewritten.
	sts, err := client.AppsV1().StatefulSets(ns).Get(ctx, "db", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "shared-data-standby", sts.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName)
	// VCTs should remain unchanged (immutable).
	assert.Len(t, sts.Spec.VolumeClaimTemplates, 1)
	assert.Equal(t, "data", sts.Spec.VolumeClaimTemplates[0].Name)
}

func TestRevertWorkloadPVCReferences_VCTOnlyStatefulSet(t *testing.T) {
	ns := "dr-ns"
	objs := []runtime.Object{
		newStatefulSetWithVCT("db", ns),
	}
	client := fakeclientset.NewSimpleClientset(objs...)
	ctx := context.Background()

	standbyPVCs := []StandbyPVCInfo{
		{Name: "data-standby", SourcePVCName: "data"},
	}

	// VCT-only StatefulSet has no static PVC volumes to revert.
	count, err := revertWorkloadPVCReferences(ctx, client, ns, standbyPVCs)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestRevertWorkloadPVCReferences_MixedVCTAndStaticPVC(t *testing.T) {
	ns := "dr-ns"
	objs := []runtime.Object{
		newStatefulSetWithVCTAndPVC("db", ns, "shared-data-standby"),
	}
	client := fakeclientset.NewSimpleClientset(objs...)
	ctx := context.Background()

	standbyPVCs := []StandbyPVCInfo{
		{Name: "shared-data-standby", SourcePVCName: "shared-data"},
	}

	// The static PVC volume pointing to standby should be reverted.
	count, err := revertWorkloadPVCReferences(ctx, client, ns, standbyPVCs)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Verify the static volume was reverted.
	sts, err := client.AppsV1().StatefulSets(ns).Get(ctx, "db", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "shared-data", sts.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName)
	// VCTs should remain unchanged.
	assert.Len(t, sts.Spec.VolumeClaimTemplates, 1)
	assert.Equal(t, "data", sts.Spec.VolumeClaimTemplates[0].Name)
}

func TestValidateStandbyPVCsForCutover_MixedReadiness(t *testing.T) {
	ns := "dr-ns"
	objs := []runtime.Object{
		newStandbyPVC("data-standby", ns, "data", corev1.ClaimBound, true),
		newStandbyPVC("logs-standby", ns, "logs", corev1.ClaimPending, false),
		newStandbyPVC("cache-standby", ns, "cache", corev1.ClaimBound, false),
	}
	destClient := fakeclientset.NewSimpleClientset(objs...)
	sourceClient := fakeclientset.NewSimpleClientset()
	ctx := context.Background()

	pvcs, err := validateStandbyPVCsForCutover(ctx, sourceClient, destClient, "src-ns", ns)
	require.NoError(t, err)
	assert.Len(t, pvcs, 3)

	// Verify the mixed states are preserved.
	pvcMap := make(map[string]StandbyPVCInfo)
	for _, p := range pvcs {
		pvcMap[p.Name] = p
	}

	assert.Equal(t, corev1.ClaimBound, pvcMap["data-standby"].Phase)
	assert.NotNil(t, pvcMap["data-standby"].LastRestoreTime)

	assert.Equal(t, corev1.ClaimPending, pvcMap["logs-standby"].Phase)
	assert.Nil(t, pvcMap["logs-standby"].LastRestoreTime)

	assert.Equal(t, corev1.ClaimBound, pvcMap["cache-standby"].Phase)
	assert.Nil(t, pvcMap["cache-standby"].LastRestoreTime)
}

// --- Error path tests for updateWorkloadPVCReferences / revertWorkloadPVCReferences ---

func TestUpdateWorkloadPVCReferences_DeploymentUpdatePartialFailure(t *testing.T) {
	ns := "dr-ns"
	objs := []runtime.Object{
		newDeploymentWithPVC("web-ok", ns, "data"),
		newDeploymentWithPVC("web-fail", ns, "data"),
	}
	client := fakeclientset.NewSimpleClientset(objs...)

	// Inject error only when updating the "web-fail" deployment.
	client.PrependReactor("update", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		updateAction := action.(k8stesting.UpdateAction)
		dep := updateAction.GetObject().(*appsv1.Deployment)
		if dep.Name == "web-fail" {
			return true, nil, fmt.Errorf("simulated API conflict")
		}
		return false, nil, nil
	})

	ctx := context.Background()
	standbyPVCs := []StandbyPVCInfo{
		{Name: "data-standby", SourcePVCName: "data"},
	}

	count, err := updateWorkloadPVCReferences(ctx, client, ns, standbyPVCs)
	require.NoError(t, err)   // Function logs warning but doesn't return error.
	assert.Equal(t, 1, count) // Only the successful update is counted.
}

func TestUpdateWorkloadPVCReferences_StatefulSetListFails(t *testing.T) {
	ns := "dr-ns"
	objs := []runtime.Object{
		newDeploymentWithPVC("web", ns, "data"),
	}
	client := fakeclientset.NewSimpleClientset(objs...)

	// Inject error on listing statefulsets.
	client.PrependReactor("list", "statefulsets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("simulated RBAC denial")
	})

	ctx := context.Background()
	standbyPVCs := []StandbyPVCInfo{
		{Name: "data-standby", SourcePVCName: "data"},
	}

	count, err := updateWorkloadPVCReferences(ctx, client, ns, standbyPVCs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list statefulsets")
	// Deployment was already updated successfully before the error.
	assert.Equal(t, 1, count)
}

func TestUpdateWorkloadPVCReferences_AllDeploymentUpdatesFail(t *testing.T) {
	ns := "dr-ns"
	objs := []runtime.Object{
		newDeploymentWithPVC("web1", ns, "data"),
		newDeploymentWithPVC("web2", ns, "data"),
	}
	client := fakeclientset.NewSimpleClientset(objs...)

	// Fail all deployment updates.
	client.PrependReactor("update", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("simulated network timeout")
	})

	ctx := context.Background()
	standbyPVCs := []StandbyPVCInfo{
		{Name: "data-standby", SourcePVCName: "data"},
	}

	count, err := updateWorkloadPVCReferences(ctx, client, ns, standbyPVCs)
	require.NoError(t, err)   // Warnings logged, no error returned.
	assert.Equal(t, 0, count) // No updates succeeded.
}

func TestRevertWorkloadPVCReferences_DeploymentUpdatePartialFailure(t *testing.T) {
	ns := "dr-ns"
	objs := []runtime.Object{
		newDeploymentWithPVC("web-ok", ns, "data-standby"),
		newDeploymentWithPVC("web-fail", ns, "data-standby"),
	}
	client := fakeclientset.NewSimpleClientset(objs...)

	// Inject error only when reverting the "web-fail" deployment.
	client.PrependReactor("update", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		updateAction := action.(k8stesting.UpdateAction)
		dep := updateAction.GetObject().(*appsv1.Deployment)
		if dep.Name == "web-fail" {
			return true, nil, fmt.Errorf("simulated API conflict")
		}
		return false, nil, nil
	})

	ctx := context.Background()
	standbyPVCs := []StandbyPVCInfo{
		{Name: "data-standby", SourcePVCName: "data"},
	}

	count, err := revertWorkloadPVCReferences(ctx, client, ns, standbyPVCs)
	require.NoError(t, err)   // Function logs warning but doesn't return error.
	assert.Equal(t, 1, count) // Only the successful revert is counted.
}

func TestRevertWorkloadPVCReferences_StatefulSetListFails(t *testing.T) {
	ns := "dr-ns"
	objs := []runtime.Object{
		newDeploymentWithPVC("web", ns, "data-standby"),
	}
	client := fakeclientset.NewSimpleClientset(objs...)

	// Inject error on listing statefulsets.
	client.PrependReactor("list", "statefulsets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("simulated RBAC denial")
	})

	ctx := context.Background()
	standbyPVCs := []StandbyPVCInfo{
		{Name: "data-standby", SourcePVCName: "data"},
	}

	count, err := revertWorkloadPVCReferences(ctx, client, ns, standbyPVCs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list statefulsets")
	// Deployment was already reverted successfully before the error.
	assert.Equal(t, 1, count)
}

func TestUpdateWorkloadPVCReferences_StatefulSetUpdateFails(t *testing.T) {
	ns := "dr-ns"
	objs := []runtime.Object{
		newDeploymentWithPVC("web", ns, "data"),
		newStatefulSetWithPVC("db", ns, "data"),
	}
	client := fakeclientset.NewSimpleClientset(objs...)

	// Fail only statefulset updates.
	client.PrependReactor("update", "statefulsets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("simulated API error")
	})

	ctx := context.Background()
	standbyPVCs := []StandbyPVCInfo{
		{Name: "data-standby", SourcePVCName: "data"},
	}

	count, err := updateWorkloadPVCReferences(ctx, client, ns, standbyPVCs)
	require.NoError(t, err)   // StatefulSet update failure is logged, not returned.
	assert.Equal(t, 1, count) // Only deployment update succeeded.
}
