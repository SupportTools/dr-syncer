package modes

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestDefaultSchedule(t *testing.T) {
	assert.Equal(t, "*/5 * * * *", DefaultSchedule, "default schedule should be every 5 minutes")
}

func TestFormatDuration_Seconds(t *testing.T) {
	testCases := []struct {
		duration time.Duration
		expected string
	}{
		{1 * time.Second, "1s"},
		{30 * time.Second, "30s"},
		{59 * time.Second, "59s"},
		{0 * time.Second, "0s"},
	}

	for _, tc := range testCases {
		result := formatDuration(tc.duration)
		assert.Equal(t, tc.expected, result, "formatDuration(%v)", tc.duration)
	}
}

func TestFormatDuration_Minutes(t *testing.T) {
	testCases := []struct {
		duration time.Duration
		expected string
	}{
		{1 * time.Minute, "1m"},
		{5 * time.Minute, "5m"},
		{30 * time.Minute, "30m"},
		{59 * time.Minute, "59m"},
	}

	for _, tc := range testCases {
		result := formatDuration(tc.duration)
		assert.Equal(t, tc.expected, result, "formatDuration(%v)", tc.duration)
	}
}

func TestFormatDuration_Hours(t *testing.T) {
	testCases := []struct {
		duration time.Duration
		expected string
	}{
		{1 * time.Hour, "1h"},
		{2 * time.Hour, "2h"},
		{24 * time.Hour, "24h"},
	}

	for _, tc := range testCases {
		result := formatDuration(tc.duration)
		assert.Equal(t, tc.expected, result, "formatDuration(%v)", tc.duration)
	}
}

func TestFormatDuration_Mixed(t *testing.T) {
	testCases := []struct {
		duration time.Duration
		expected string
	}{
		{1*time.Hour + 30*time.Minute, "1h30m"},
		{1*time.Hour + 30*time.Minute + 45*time.Second, "1h30m45s"},
		{2*time.Minute + 30*time.Second, "2m30s"},
		{1*time.Hour + 1*time.Second, "1h1s"},
	}

	for _, tc := range testCases {
		result := formatDuration(tc.duration)
		assert.Equal(t, tc.expected, result, "formatDuration(%v)", tc.duration)
	}
}

func TestTimeEqual_BothNil(t *testing.T) {
	assert.True(t, timeEqual(nil, nil))
}

func TestTimeEqual_OneNil(t *testing.T) {
	now := metav1.Now()
	assert.False(t, timeEqual(&now, nil))
	assert.False(t, timeEqual(nil, &now))
}

func TestTimeEqual_SameTime(t *testing.T) {
	now := metav1.Now()
	now2 := now.DeepCopy()
	assert.True(t, timeEqual(&now, now2))
}

func TestTimeEqual_DifferentTime(t *testing.T) {
	now := metav1.Now()
	later := metav1.NewTime(now.Add(1 * time.Hour))
	assert.False(t, timeEqual(&now, &later))
}

func TestSyncStatsEqual_BothNil(t *testing.T) {
	assert.True(t, syncStatsEqual(nil, nil))
}

func TestSyncStatsEqual_OneNil(t *testing.T) {
	stats := &drv1alpha1.SyncStats{TotalResources: 5}
	assert.False(t, syncStatsEqual(stats, nil))
	assert.False(t, syncStatsEqual(nil, stats))
}

func TestSyncStatsEqual_Equal(t *testing.T) {
	stats1 := &drv1alpha1.SyncStats{
		TotalResources:   10,
		SuccessfulSyncs:  8,
		FailedSyncs:      2,
		LastSyncDuration: "1m30s",
	}
	stats2 := &drv1alpha1.SyncStats{
		TotalResources:   10,
		SuccessfulSyncs:  8,
		FailedSyncs:      2,
		LastSyncDuration: "1m30s",
	}
	assert.True(t, syncStatsEqual(stats1, stats2))
}

func TestSyncStatsEqual_Different(t *testing.T) {
	stats1 := &drv1alpha1.SyncStats{TotalResources: 10}
	stats2 := &drv1alpha1.SyncStats{TotalResources: 5}
	assert.False(t, syncStatsEqual(stats1, stats2))
}

func TestSyncErrorEqual_BothNil(t *testing.T) {
	assert.True(t, syncErrorEqual(nil, nil))
}

func TestSyncErrorEqual_OneNil(t *testing.T) {
	err := &drv1alpha1.SyncError{Message: "error"}
	assert.False(t, syncErrorEqual(err, nil))
	assert.False(t, syncErrorEqual(nil, err))
}

func TestSyncErrorEqual_Equal(t *testing.T) {
	now := metav1.Now()
	err1 := &drv1alpha1.SyncError{
		Message:  "sync failed",
		Resource: "Deployment/app",
		Time:     now,
	}
	err2 := &drv1alpha1.SyncError{
		Message:  "sync failed",
		Resource: "Deployment/app",
		Time:     now,
	}
	assert.True(t, syncErrorEqual(err1, err2))
}

func TestSyncErrorEqual_DifferentMessage(t *testing.T) {
	now := metav1.Now()
	err1 := &drv1alpha1.SyncError{Message: "error1", Time: now}
	err2 := &drv1alpha1.SyncError{Message: "error2", Time: now}
	assert.False(t, syncErrorEqual(err1, err2))
}

func TestRetryStatusEqual_BothNil(t *testing.T) {
	assert.True(t, retryStatusEqual(nil, nil))
}

func TestRetryStatusEqual_OneNil(t *testing.T) {
	status := &drv1alpha1.RetryStatus{RetriesRemaining: 3}
	assert.False(t, retryStatusEqual(status, nil))
	assert.False(t, retryStatusEqual(nil, status))
}

func TestRetryStatusEqual_Equal(t *testing.T) {
	now := metav1.Now()
	status1 := &drv1alpha1.RetryStatus{
		RetriesRemaining: 5,
		BackoffDuration:  "10s",
		NextRetryTime:    &now,
	}
	status2 := &drv1alpha1.RetryStatus{
		RetriesRemaining: 5,
		BackoffDuration:  "10s",
		NextRetryTime:    &now,
	}
	assert.True(t, retryStatusEqual(status1, status2))
}

func TestRetryStatusEqual_Different(t *testing.T) {
	status1 := &drv1alpha1.RetryStatus{RetriesRemaining: 5}
	status2 := &drv1alpha1.RetryStatus{RetriesRemaining: 3}
	assert.False(t, retryStatusEqual(status1, status2))
}

func TestConditionsEqual_BothEmpty(t *testing.T) {
	assert.True(t, conditionsEqual(nil, nil))
	assert.True(t, conditionsEqual([]metav1.Condition{}, []metav1.Condition{}))
}

func TestConditionsEqual_DifferentLength(t *testing.T) {
	cond1 := []metav1.Condition{{Type: "Ready"}}
	cond2 := []metav1.Condition{}
	assert.False(t, conditionsEqual(cond1, cond2))
}

func TestConditionsEqual_SameConditions(t *testing.T) {
	now := metav1.Now()
	cond1 := []metav1.Condition{
		{Type: "Synced", Status: metav1.ConditionTrue, Reason: "SyncCompleted", Message: "OK", LastTransitionTime: now},
	}
	cond2 := []metav1.Condition{
		{Type: "Synced", Status: metav1.ConditionTrue, Reason: "SyncCompleted", Message: "OK", LastTransitionTime: now},
	}
	assert.True(t, conditionsEqual(cond1, cond2))
}

func TestConditionsEqual_DifferentStatus(t *testing.T) {
	now := metav1.Now()
	cond1 := []metav1.Condition{{Type: "Synced", Status: metav1.ConditionTrue, LastTransitionTime: now}}
	cond2 := []metav1.Condition{{Type: "Synced", Status: metav1.ConditionFalse, LastTransitionTime: now}}
	assert.False(t, conditionsEqual(cond1, cond2))
}

func TestDeploymentScalesEqual_BothEmpty(t *testing.T) {
	assert.True(t, deploymentScalesEqual(nil, nil))
	assert.True(t, deploymentScalesEqual([]drv1alpha1.DeploymentScale{}, []drv1alpha1.DeploymentScale{}))
}

func TestDeploymentScalesEqual_DifferentLength(t *testing.T) {
	scales1 := []drv1alpha1.DeploymentScale{{Name: "app"}}
	scales2 := []drv1alpha1.DeploymentScale{}
	assert.False(t, deploymentScalesEqual(scales1, scales2))
}

func TestDeploymentScalesEqual_SameScales(t *testing.T) {
	now := metav1.Now()
	scales1 := []drv1alpha1.DeploymentScale{
		{Name: "app", OriginalReplicas: 3, LastSyncedAt: &now},
	}
	scales2 := []drv1alpha1.DeploymentScale{
		{Name: "app", OriginalReplicas: 3, LastSyncedAt: &now},
	}
	assert.True(t, deploymentScalesEqual(scales1, scales2))
}

func TestDeploymentScalesEqual_DifferentReplicas(t *testing.T) {
	now := metav1.Now()
	scales1 := []drv1alpha1.DeploymentScale{{Name: "app", OriginalReplicas: 3, LastSyncedAt: &now}}
	scales2 := []drv1alpha1.DeploymentScale{{Name: "app", OriginalReplicas: 5, LastSyncedAt: &now}}
	assert.False(t, deploymentScalesEqual(scales1, scales2))
}

func TestResourceStatusEqual_BothEmpty(t *testing.T) {
	assert.True(t, resourceStatusEqual(nil, nil))
	assert.True(t, resourceStatusEqual([]drv1alpha1.ResourceStatus{}, []drv1alpha1.ResourceStatus{}))
}

func TestResourceStatusEqual_SameStatus(t *testing.T) {
	now := metav1.Now()
	status1 := []drv1alpha1.ResourceStatus{
		{Kind: "Deployment", Name: "app", Namespace: "default", Status: "Synced", LastSyncTime: &now},
	}
	status2 := []drv1alpha1.ResourceStatus{
		{Kind: "Deployment", Name: "app", Namespace: "default", Status: "Synced", LastSyncTime: &now},
	}
	assert.True(t, resourceStatusEqual(status1, status2))
}

func TestResourceStatusEqual_DifferentStatus(t *testing.T) {
	now := metav1.Now()
	status1 := []drv1alpha1.ResourceStatus{{Kind: "Deployment", Name: "app", Namespace: "default", Status: "Synced", LastSyncTime: &now}}
	status2 := []drv1alpha1.ResourceStatus{{Kind: "Deployment", Name: "app", Namespace: "default", Status: "Failed", LastSyncTime: &now}}
	assert.False(t, resourceStatusEqual(status1, status2))
}

func TestStatusEqual_BothNil(t *testing.T) {
	assert.True(t, statusEqual(nil, nil))
}

func TestStatusEqual_OneNil(t *testing.T) {
	status := &drv1alpha1.NamespaceMappingStatus{}
	assert.False(t, statusEqual(status, nil))
	assert.False(t, statusEqual(nil, status))
}

func TestStatusEqual_SamePhase(t *testing.T) {
	status1 := &drv1alpha1.NamespaceMappingStatus{Phase: drv1alpha1.SyncPhaseCompleted}
	status2 := &drv1alpha1.NamespaceMappingStatus{Phase: drv1alpha1.SyncPhaseCompleted}
	assert.True(t, statusEqual(status1, status2))
}

func TestStatusEqual_DifferentPhase(t *testing.T) {
	status1 := &drv1alpha1.NamespaceMappingStatus{Phase: drv1alpha1.SyncPhaseCompleted}
	status2 := &drv1alpha1.NamespaceMappingStatus{Phase: drv1alpha1.SyncPhaseFailed}
	assert.False(t, statusEqual(status1, status2))
}

func TestModeReconciler_GetResourceGVRs_ConfigMaps(t *testing.T) {
	r := &ModeReconciler{}
	gvrs := r.getResourceGVRs([]string{"configmaps"})

	assert.Len(t, gvrs, 1)
	assert.Equal(t, schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}, gvrs[0])
}

func TestModeReconciler_GetResourceGVRs_Secrets(t *testing.T) {
	r := &ModeReconciler{}
	gvrs := r.getResourceGVRs([]string{"secrets"})

	assert.Len(t, gvrs, 1)
	assert.Equal(t, schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"}, gvrs[0])
}

func TestModeReconciler_GetResourceGVRs_Deployments(t *testing.T) {
	r := &ModeReconciler{}
	gvrs := r.getResourceGVRs([]string{"deployments"})

	assert.Len(t, gvrs, 1)
	assert.Equal(t, schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, gvrs[0])
}

func TestModeReconciler_GetResourceGVRs_Services(t *testing.T) {
	r := &ModeReconciler{}
	gvrs := r.getResourceGVRs([]string{"services"})

	assert.Len(t, gvrs, 1)
	assert.Equal(t, schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}, gvrs[0])
}

func TestModeReconciler_GetResourceGVRs_Ingresses(t *testing.T) {
	r := &ModeReconciler{}
	gvrs := r.getResourceGVRs([]string{"ingresses"})

	assert.Len(t, gvrs, 1)
	assert.Equal(t, schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}, gvrs[0])
}

func TestModeReconciler_GetResourceGVRs_PVCs(t *testing.T) {
	r := &ModeReconciler{}
	gvrs := r.getResourceGVRs([]string{"persistentvolumeclaims"})

	assert.Len(t, gvrs, 1)
	assert.Equal(t, schema.GroupVersionResource{Group: "", Version: "v1", Resource: "persistentvolumeclaims"}, gvrs[0])
}

func TestModeReconciler_GetResourceGVRs_Wildcard(t *testing.T) {
	r := &ModeReconciler{}
	gvrs := r.getResourceGVRs([]string{"*"})

	// Should return all default resource types
	assert.Len(t, gvrs, 6)
}

func TestModeReconciler_GetResourceGVRs_Multiple(t *testing.T) {
	r := &ModeReconciler{}
	gvrs := r.getResourceGVRs([]string{"configmaps", "secrets", "deployments"})

	assert.Len(t, gvrs, 3)
}

func TestModeReconciler_GetResourceGVRs_CaseInsensitive(t *testing.T) {
	r := &ModeReconciler{}

	gvrsLower := r.getResourceGVRs([]string{"configmaps"})
	gvrsUpper := r.getResourceGVRs([]string{"CONFIGMAPS"})
	gvrsMixed := r.getResourceGVRs([]string{"ConfigMaps"})

	assert.Equal(t, gvrsLower, gvrsUpper)
	assert.Equal(t, gvrsLower, gvrsMixed)
}

func TestModeReconciler_GetResourceGVRs_SingularForms(t *testing.T) {
	r := &ModeReconciler{}

	// Test singular forms are recognized
	gvrPlural := r.getResourceGVRs([]string{"deployments"})
	gvrSingular := r.getResourceGVRs([]string{"deployment"})

	assert.Len(t, gvrPlural, 1)
	assert.Len(t, gvrSingular, 1)
	assert.Equal(t, gvrPlural[0].Resource, gvrSingular[0].Resource)
}

func TestModeReconciler_GetResourceGVRs_PVCVariants(t *testing.T) {
	r := &ModeReconciler{}

	gvr1 := r.getResourceGVRs([]string{"persistentvolumeclaims"})
	gvr2 := r.getResourceGVRs([]string{"persistentvolumeclaim"})
	gvr3 := r.getResourceGVRs([]string{"pvc"})

	assert.Len(t, gvr1, 1)
	assert.Len(t, gvr2, 1)
	assert.Len(t, gvr3, 1)
	assert.Equal(t, gvr1[0].Resource, gvr2[0].Resource)
	assert.Equal(t, gvr1[0].Resource, gvr3[0].Resource)
}

func TestModeReconciler_GetResourceGVRs_Empty(t *testing.T) {
	r := &ModeReconciler{}
	gvrs := r.getResourceGVRs([]string{})

	assert.Empty(t, gvrs)
}

func TestModeReconciler_GetResourceGVRs_Unknown(t *testing.T) {
	r := &ModeReconciler{}
	gvrs := r.getResourceGVRs([]string{"unknownresource"})

	// Unknown resources should be skipped
	assert.Empty(t, gvrs)
}

func TestNewModeReconciler_DefaultClusterNames(t *testing.T) {
	r := NewModeReconciler(nil, nil, nil, nil, nil, nil, nil, "", "")

	assert.Equal(t, "source", r.sourceClusterName)
	assert.Equal(t, "destination", r.destClusterName)
}

func TestNewModeReconciler_CustomClusterNames(t *testing.T) {
	r := NewModeReconciler(nil, nil, nil, nil, nil, nil, nil, "prod-cluster", "dr-cluster")

	assert.Equal(t, "prod-cluster", r.sourceClusterName)
	assert.Equal(t, "dr-cluster", r.destClusterName)
}

func TestNewModeReconciler_WatchManagerCreated(t *testing.T) {
	r := NewModeReconciler(nil, nil, nil, nil, nil, nil, nil, "", "")

	assert.NotNil(t, r.watchManager)
}
