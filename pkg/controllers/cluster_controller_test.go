package controllers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	drsyncerio "github.com/supporttools/dr-syncer/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestRemoteClusterReconciler_Struct(t *testing.T) {
	scheme := runtime.NewScheme()

	reconciler := &RemoteClusterReconciler{
		Scheme: scheme,
	}

	assert.NotNil(t, reconciler)
	assert.NotNil(t, reconciler.Scheme)
	assert.Nil(t, reconciler.pvcSyncManager)
}

func TestConditionsEqual_EmptySlices(t *testing.T) {
	var a, b []metav1.Condition

	result := conditionsEqual(a, b)
	assert.True(t, result, "Two empty slices should be equal")
}

func TestConditionsEqual_EmptyAndNil(t *testing.T) {
	var a []metav1.Condition
	b := []metav1.Condition{}

	result := conditionsEqual(a, b)
	assert.True(t, result, "Nil and empty slice should be equal")
}

func TestConditionsEqual_DifferentLengths(t *testing.T) {
	a := []metav1.Condition{
		{Type: "Ready", Status: metav1.ConditionTrue, Reason: "AllGood", Message: "Ready"},
	}
	b := []metav1.Condition{
		{Type: "Ready", Status: metav1.ConditionTrue, Reason: "AllGood", Message: "Ready"},
		{Type: "Available", Status: metav1.ConditionTrue, Reason: "AllGood", Message: "Available"},
	}

	result := conditionsEqual(a, b)
	assert.False(t, result, "Slices with different lengths should not be equal")
}

func TestConditionsEqual_SameConditions(t *testing.T) {
	now := metav1.Now()
	a := []metav1.Condition{
		{Type: "Ready", Status: metav1.ConditionTrue, Reason: "AllGood", Message: "Ready", LastTransitionTime: now},
		{Type: "Available", Status: metav1.ConditionTrue, Reason: "AllGood", Message: "Available", LastTransitionTime: now},
	}
	b := []metav1.Condition{
		{Type: "Ready", Status: metav1.ConditionTrue, Reason: "AllGood", Message: "Ready", LastTransitionTime: now},
		{Type: "Available", Status: metav1.ConditionTrue, Reason: "AllGood", Message: "Available", LastTransitionTime: now},
	}

	result := conditionsEqual(a, b)
	assert.True(t, result, "Same conditions should be equal")
}

func TestConditionsEqual_DifferentOrder(t *testing.T) {
	now := metav1.Now()
	a := []metav1.Condition{
		{Type: "Ready", Status: metav1.ConditionTrue, Reason: "AllGood", Message: "Ready", LastTransitionTime: now},
		{Type: "Available", Status: metav1.ConditionTrue, Reason: "AllGood", Message: "Available", LastTransitionTime: now},
	}
	b := []metav1.Condition{
		{Type: "Available", Status: metav1.ConditionTrue, Reason: "AllGood", Message: "Available", LastTransitionTime: now},
		{Type: "Ready", Status: metav1.ConditionTrue, Reason: "AllGood", Message: "Ready", LastTransitionTime: now},
	}

	result := conditionsEqual(a, b)
	assert.True(t, result, "Same conditions in different order should be equal")
}

func TestConditionsEqual_DifferentStatus(t *testing.T) {
	a := []metav1.Condition{
		{Type: "Ready", Status: metav1.ConditionTrue, Reason: "AllGood", Message: "Ready"},
	}
	b := []metav1.Condition{
		{Type: "Ready", Status: metav1.ConditionFalse, Reason: "AllGood", Message: "Ready"},
	}

	result := conditionsEqual(a, b)
	assert.False(t, result, "Conditions with different status should not be equal")
}

func TestConditionsEqual_DifferentReason(t *testing.T) {
	a := []metav1.Condition{
		{Type: "Ready", Status: metav1.ConditionTrue, Reason: "AllGood", Message: "Ready"},
	}
	b := []metav1.Condition{
		{Type: "Ready", Status: metav1.ConditionTrue, Reason: "SomethingElse", Message: "Ready"},
	}

	result := conditionsEqual(a, b)
	assert.False(t, result, "Conditions with different reason should not be equal")
}

func TestConditionsEqual_DifferentMessage(t *testing.T) {
	a := []metav1.Condition{
		{Type: "Ready", Status: metav1.ConditionTrue, Reason: "AllGood", Message: "Ready"},
	}
	b := []metav1.Condition{
		{Type: "Ready", Status: metav1.ConditionTrue, Reason: "AllGood", Message: "Different message"},
	}

	result := conditionsEqual(a, b)
	assert.False(t, result, "Conditions with different message should not be equal")
}

func TestConditionsEqual_DifferentType(t *testing.T) {
	a := []metav1.Condition{
		{Type: "Ready", Status: metav1.ConditionTrue, Reason: "AllGood", Message: "Ready"},
	}
	b := []metav1.Condition{
		{Type: "Available", Status: metav1.ConditionTrue, Reason: "AllGood", Message: "Ready"},
	}

	result := conditionsEqual(a, b)
	assert.False(t, result, "Conditions with different type should not be equal")
}

func TestConditionsEqual_IgnoresLastTransitionTime(t *testing.T) {
	time1 := metav1.NewTime(time.Now())
	time2 := metav1.NewTime(time.Now().Add(1 * time.Hour))

	a := []metav1.Condition{
		{Type: "Ready", Status: metav1.ConditionTrue, Reason: "AllGood", Message: "Ready", LastTransitionTime: time1},
	}
	b := []metav1.Condition{
		{Type: "Ready", Status: metav1.ConditionTrue, Reason: "AllGood", Message: "Ready", LastTransitionTime: time2},
	}

	result := conditionsEqual(a, b)
	assert.True(t, result, "Conditions with different LastTransitionTime should still be equal")
}

func TestConditionsEqual_MultipleConditions(t *testing.T) {
	a := []metav1.Condition{
		{Type: "KubeconfigAvailable", Status: metav1.ConditionTrue, Reason: "Found", Message: "Secret exists"},
		{Type: "KubeconfigValid", Status: metav1.ConditionTrue, Reason: "Valid", Message: "Kubeconfig is valid"},
		{Type: "ClusterAvailable", Status: metav1.ConditionTrue, Reason: "Connected", Message: "Connection successful"},
	}
	b := []metav1.Condition{
		{Type: "KubeconfigAvailable", Status: metav1.ConditionTrue, Reason: "Found", Message: "Secret exists"},
		{Type: "KubeconfigValid", Status: metav1.ConditionTrue, Reason: "Valid", Message: "Kubeconfig is valid"},
		{Type: "ClusterAvailable", Status: metav1.ConditionTrue, Reason: "Connected", Message: "Connection successful"},
	}

	result := conditionsEqual(a, b)
	assert.True(t, result, "Multiple matching conditions should be equal")
}

func TestConditionsEqual_OneConditionDiffers(t *testing.T) {
	a := []metav1.Condition{
		{Type: "KubeconfigAvailable", Status: metav1.ConditionTrue, Reason: "Found", Message: "Secret exists"},
		{Type: "KubeconfigValid", Status: metav1.ConditionTrue, Reason: "Valid", Message: "Kubeconfig is valid"},
		{Type: "ClusterAvailable", Status: metav1.ConditionTrue, Reason: "Connected", Message: "Connection successful"},
	}
	b := []metav1.Condition{
		{Type: "KubeconfigAvailable", Status: metav1.ConditionTrue, Reason: "Found", Message: "Secret exists"},
		{Type: "KubeconfigValid", Status: metav1.ConditionFalse, Reason: "Invalid", Message: "Kubeconfig is invalid"},
		{Type: "ClusterAvailable", Status: metav1.ConditionTrue, Reason: "Connected", Message: "Connection successful"},
	}

	result := conditionsEqual(a, b)
	assert.False(t, result, "Should not be equal when one condition differs")
}

func TestSetRemoteClusterCondition_AddNewCondition(t *testing.T) {
	cluster := &drsyncerio.RemoteCluster{
		Status: drsyncerio.RemoteClusterStatus{
			Conditions: []metav1.Condition{},
		},
	}

	setRemoteClusterCondition(cluster, "Ready", metav1.ConditionTrue, "AllGood", "Cluster is ready")

	assert.Len(t, cluster.Status.Conditions, 1)
	assert.Equal(t, "Ready", cluster.Status.Conditions[0].Type)
	assert.Equal(t, metav1.ConditionTrue, cluster.Status.Conditions[0].Status)
	assert.Equal(t, "AllGood", cluster.Status.Conditions[0].Reason)
	assert.Equal(t, "Cluster is ready", cluster.Status.Conditions[0].Message)
	assert.False(t, cluster.Status.Conditions[0].LastTransitionTime.IsZero())
}

func TestSetRemoteClusterCondition_UpdateExistingCondition(t *testing.T) {
	oldTime := metav1.NewTime(time.Now().Add(-1 * time.Hour))
	cluster := &drsyncerio.RemoteCluster{
		Status: drsyncerio.RemoteClusterStatus{
			Conditions: []metav1.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionFalse,
					Reason:             "NotReady",
					Message:            "Cluster is not ready",
					LastTransitionTime: oldTime,
				},
			},
		},
	}

	setRemoteClusterCondition(cluster, "Ready", metav1.ConditionTrue, "AllGood", "Cluster is ready")

	assert.Len(t, cluster.Status.Conditions, 1)
	assert.Equal(t, "Ready", cluster.Status.Conditions[0].Type)
	assert.Equal(t, metav1.ConditionTrue, cluster.Status.Conditions[0].Status)
	assert.Equal(t, "AllGood", cluster.Status.Conditions[0].Reason)
	assert.Equal(t, "Cluster is ready", cluster.Status.Conditions[0].Message)
	// LastTransitionTime should be updated since status changed
	assert.True(t, cluster.Status.Conditions[0].LastTransitionTime.After(oldTime.Time))
}

func TestSetRemoteClusterCondition_NoOpWhenSame(t *testing.T) {
	oldTime := metav1.NewTime(time.Now().Add(-1 * time.Hour))
	cluster := &drsyncerio.RemoteCluster{
		Status: drsyncerio.RemoteClusterStatus{
			Conditions: []metav1.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionTrue,
					Reason:             "AllGood",
					Message:            "Cluster is ready",
					LastTransitionTime: oldTime,
				},
			},
		},
	}

	setRemoteClusterCondition(cluster, "Ready", metav1.ConditionTrue, "AllGood", "Cluster is ready")

	assert.Len(t, cluster.Status.Conditions, 1)
	// LastTransitionTime should NOT be updated since nothing changed
	assert.Equal(t, oldTime, cluster.Status.Conditions[0].LastTransitionTime)
}

func TestSetRemoteClusterCondition_UpdateOnlyStatus(t *testing.T) {
	oldTime := metav1.NewTime(time.Now().Add(-1 * time.Hour))
	cluster := &drsyncerio.RemoteCluster{
		Status: drsyncerio.RemoteClusterStatus{
			Conditions: []metav1.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionTrue,
					Reason:             "AllGood",
					Message:            "Cluster is ready",
					LastTransitionTime: oldTime,
				},
			},
		},
	}

	setRemoteClusterCondition(cluster, "Ready", metav1.ConditionFalse, "AllGood", "Cluster is ready")

	assert.Len(t, cluster.Status.Conditions, 1)
	assert.Equal(t, metav1.ConditionFalse, cluster.Status.Conditions[0].Status)
	// LastTransitionTime should be updated since status changed
	assert.True(t, cluster.Status.Conditions[0].LastTransitionTime.After(oldTime.Time))
}

func TestSetRemoteClusterCondition_UpdateOnlyReason(t *testing.T) {
	oldTime := metav1.NewTime(time.Now().Add(-1 * time.Hour))
	cluster := &drsyncerio.RemoteCluster{
		Status: drsyncerio.RemoteClusterStatus{
			Conditions: []metav1.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionTrue,
					Reason:             "AllGood",
					Message:            "Cluster is ready",
					LastTransitionTime: oldTime,
				},
			},
		},
	}

	setRemoteClusterCondition(cluster, "Ready", metav1.ConditionTrue, "StillGood", "Cluster is ready")

	assert.Len(t, cluster.Status.Conditions, 1)
	assert.Equal(t, "StillGood", cluster.Status.Conditions[0].Reason)
	// LastTransitionTime should be updated since reason changed
	assert.True(t, cluster.Status.Conditions[0].LastTransitionTime.After(oldTime.Time))
}

func TestSetRemoteClusterCondition_UpdateOnlyMessage(t *testing.T) {
	oldTime := metav1.NewTime(time.Now().Add(-1 * time.Hour))
	cluster := &drsyncerio.RemoteCluster{
		Status: drsyncerio.RemoteClusterStatus{
			Conditions: []metav1.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionTrue,
					Reason:             "AllGood",
					Message:            "Cluster is ready",
					LastTransitionTime: oldTime,
				},
			},
		},
	}

	setRemoteClusterCondition(cluster, "Ready", metav1.ConditionTrue, "AllGood", "Updated message")

	assert.Len(t, cluster.Status.Conditions, 1)
	assert.Equal(t, "Updated message", cluster.Status.Conditions[0].Message)
	// LastTransitionTime should be updated since message changed
	assert.True(t, cluster.Status.Conditions[0].LastTransitionTime.After(oldTime.Time))
}

func TestSetRemoteClusterCondition_MultipleConditions(t *testing.T) {
	oldTime := metav1.NewTime(time.Now().Add(-1 * time.Hour))
	cluster := &drsyncerio.RemoteCluster{
		Status: drsyncerio.RemoteClusterStatus{
			Conditions: []metav1.Condition{
				{
					Type:               "KubeconfigAvailable",
					Status:             metav1.ConditionTrue,
					Reason:             "Found",
					Message:            "Secret exists",
					LastTransitionTime: oldTime,
				},
				{
					Type:               "ClusterAvailable",
					Status:             metav1.ConditionFalse,
					Reason:             "NotConnected",
					Message:            "Connection failed",
					LastTransitionTime: oldTime,
				},
			},
		},
	}

	// Update just the second condition
	setRemoteClusterCondition(cluster, "ClusterAvailable", metav1.ConditionTrue, "Connected", "Connection successful")

	assert.Len(t, cluster.Status.Conditions, 2)

	// First condition should be unchanged
	assert.Equal(t, "KubeconfigAvailable", cluster.Status.Conditions[0].Type)
	assert.Equal(t, metav1.ConditionTrue, cluster.Status.Conditions[0].Status)
	assert.Equal(t, oldTime, cluster.Status.Conditions[0].LastTransitionTime)

	// Second condition should be updated
	assert.Equal(t, "ClusterAvailable", cluster.Status.Conditions[1].Type)
	assert.Equal(t, metav1.ConditionTrue, cluster.Status.Conditions[1].Status)
	assert.Equal(t, "Connected", cluster.Status.Conditions[1].Reason)
	assert.Equal(t, "Connection successful", cluster.Status.Conditions[1].Message)
}

func TestSetRemoteClusterCondition_AddSecondCondition(t *testing.T) {
	oldTime := metav1.NewTime(time.Now().Add(-1 * time.Hour))
	cluster := &drsyncerio.RemoteCluster{
		Status: drsyncerio.RemoteClusterStatus{
			Conditions: []metav1.Condition{
				{
					Type:               "KubeconfigAvailable",
					Status:             metav1.ConditionTrue,
					Reason:             "Found",
					Message:            "Secret exists",
					LastTransitionTime: oldTime,
				},
			},
		},
	}

	setRemoteClusterCondition(cluster, "ClusterAvailable", metav1.ConditionTrue, "Connected", "Connection successful")

	assert.Len(t, cluster.Status.Conditions, 2)
	assert.Equal(t, "KubeconfigAvailable", cluster.Status.Conditions[0].Type)
	assert.Equal(t, "ClusterAvailable", cluster.Status.Conditions[1].Type)
}

func TestSetRemoteClusterCondition_NilConditions(t *testing.T) {
	cluster := &drsyncerio.RemoteCluster{
		Status: drsyncerio.RemoteClusterStatus{
			Conditions: nil,
		},
	}

	setRemoteClusterCondition(cluster, "Ready", metav1.ConditionTrue, "AllGood", "Cluster is ready")

	assert.Len(t, cluster.Status.Conditions, 1)
	assert.Equal(t, "Ready", cluster.Status.Conditions[0].Type)
}

func TestSetRemoteClusterCondition_AllConditionTypes(t *testing.T) {
	cluster := &drsyncerio.RemoteCluster{
		Status: drsyncerio.RemoteClusterStatus{
			Conditions: []metav1.Condition{},
		},
	}

	// Add all condition types used in the controller
	conditionTypes := []struct {
		condType string
		status   metav1.ConditionStatus
		reason   string
		message  string
	}{
		{"ScheduleValid", metav1.ConditionTrue, "ScheduleValidated", "Default schedule is valid"},
		{"KubeconfigAvailable", metav1.ConditionTrue, "KubeconfigFound", "Kubeconfig secret is available"},
		{"KubeconfigValid", metav1.ConditionTrue, "KubeconfigValidated", "Kubeconfig is valid"},
		{"ClusterAvailable", metav1.ConditionTrue, "ConnectionSuccessful", "Successfully connected to cluster"},
		{"PVCSyncReady", metav1.ConditionTrue, "ReconciliationSuccessful", "PVC sync agent deployed successfully"},
	}

	for _, ct := range conditionTypes {
		setRemoteClusterCondition(cluster, ct.condType, ct.status, ct.reason, ct.message)
	}

	assert.Len(t, cluster.Status.Conditions, 5)

	// Verify all conditions are present
	conditionMap := make(map[string]metav1.Condition)
	for _, c := range cluster.Status.Conditions {
		conditionMap[c.Type] = c
	}

	for _, ct := range conditionTypes {
		cond, exists := conditionMap[ct.condType]
		assert.True(t, exists, "Condition %s should exist", ct.condType)
		assert.Equal(t, ct.status, cond.Status)
		assert.Equal(t, ct.reason, cond.Reason)
		assert.Equal(t, ct.message, cond.Message)
	}
}

func TestSetRemoteClusterCondition_ConditionFalseWithError(t *testing.T) {
	cluster := &drsyncerio.RemoteCluster{
		Status: drsyncerio.RemoteClusterStatus{
			Conditions: []metav1.Condition{},
		},
	}

	setRemoteClusterCondition(cluster, "ClusterAvailable", metav1.ConditionFalse, "ConnectionFailed", "dial tcp 192.168.1.1:6443: connect: connection refused")

	assert.Len(t, cluster.Status.Conditions, 1)
	assert.Equal(t, metav1.ConditionFalse, cluster.Status.Conditions[0].Status)
	assert.Equal(t, "ConnectionFailed", cluster.Status.Conditions[0].Reason)
	assert.Contains(t, cluster.Status.Conditions[0].Message, "connection refused")
}

func TestSetRemoteClusterCondition_ConditionUnknown(t *testing.T) {
	cluster := &drsyncerio.RemoteCluster{
		Status: drsyncerio.RemoteClusterStatus{
			Conditions: []metav1.Condition{},
		},
	}

	setRemoteClusterCondition(cluster, "PVCSyncReady", metav1.ConditionUnknown, "Unknown", "Status is unknown")

	assert.Len(t, cluster.Status.Conditions, 1)
	assert.Equal(t, metav1.ConditionUnknown, cluster.Status.Conditions[0].Status)
}

func TestRemoteClusterReconciler_SchemeAssignment(t *testing.T) {
	scheme := runtime.NewScheme()

	// Simulate adding types to scheme
	err := drsyncerio.AddToScheme(scheme)
	assert.NoError(t, err)

	reconciler := &RemoteClusterReconciler{
		Scheme: scheme,
	}

	assert.NotNil(t, reconciler.Scheme)
}

func TestConditionsEqual_SingleConditionTrue(t *testing.T) {
	a := []metav1.Condition{
		{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Test", Message: "Test"},
	}
	b := []metav1.Condition{
		{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Test", Message: "Test"},
	}

	assert.True(t, conditionsEqual(a, b))
}

func TestConditionsEqual_SingleConditionFalse(t *testing.T) {
	a := []metav1.Condition{
		{Type: "Ready", Status: metav1.ConditionFalse, Reason: "Test", Message: "Test"},
	}
	b := []metav1.Condition{
		{Type: "Ready", Status: metav1.ConditionFalse, Reason: "Test", Message: "Test"},
	}

	assert.True(t, conditionsEqual(a, b))
}

func TestConditionsEqual_RealisticClusterConditions(t *testing.T) {
	now := metav1.Now()
	healthy := []metav1.Condition{
		{Type: "KubeconfigAvailable", Status: metav1.ConditionTrue, Reason: "KubeconfigFound", Message: "Kubeconfig secret is available for cluster prod", LastTransitionTime: now},
		{Type: "KubeconfigValid", Status: metav1.ConditionTrue, Reason: "KubeconfigValidated", Message: "Kubeconfig is valid for cluster prod", LastTransitionTime: now},
		{Type: "ClusterAvailable", Status: metav1.ConditionTrue, Reason: "ConnectionSuccessful", Message: "Successfully connected to cluster prod", LastTransitionTime: now},
		{Type: "PVCSyncReady", Status: metav1.ConditionTrue, Reason: "ReconciliationSuccessful", Message: "PVC sync agent deployed successfully", LastTransitionTime: now},
	}

	healthy2 := []metav1.Condition{
		{Type: "KubeconfigAvailable", Status: metav1.ConditionTrue, Reason: "KubeconfigFound", Message: "Kubeconfig secret is available for cluster prod", LastTransitionTime: now},
		{Type: "KubeconfigValid", Status: metav1.ConditionTrue, Reason: "KubeconfigValidated", Message: "Kubeconfig is valid for cluster prod", LastTransitionTime: now},
		{Type: "ClusterAvailable", Status: metav1.ConditionTrue, Reason: "ConnectionSuccessful", Message: "Successfully connected to cluster prod", LastTransitionTime: now},
		{Type: "PVCSyncReady", Status: metav1.ConditionTrue, Reason: "ReconciliationSuccessful", Message: "PVC sync agent deployed successfully", LastTransitionTime: now},
	}

	assert.True(t, conditionsEqual(healthy, healthy2), "Identical healthy cluster conditions should be equal")
}

func TestConditionsEqual_UnhealthyCluster(t *testing.T) {
	now := metav1.Now()
	unhealthy := []metav1.Condition{
		{Type: "KubeconfigAvailable", Status: metav1.ConditionTrue, Reason: "KubeconfigFound", Message: "Kubeconfig secret is available", LastTransitionTime: now},
		{Type: "KubeconfigValid", Status: metav1.ConditionTrue, Reason: "KubeconfigValidated", Message: "Kubeconfig is valid", LastTransitionTime: now},
		{Type: "ClusterAvailable", Status: metav1.ConditionFalse, Reason: "ConnectionFailed", Message: "Failed to connect: timeout", LastTransitionTime: now},
	}

	healthy := []metav1.Condition{
		{Type: "KubeconfigAvailable", Status: metav1.ConditionTrue, Reason: "KubeconfigFound", Message: "Kubeconfig secret is available", LastTransitionTime: now},
		{Type: "KubeconfigValid", Status: metav1.ConditionTrue, Reason: "KubeconfigValidated", Message: "Kubeconfig is valid", LastTransitionTime: now},
		{Type: "ClusterAvailable", Status: metav1.ConditionTrue, Reason: "ConnectionSuccessful", Message: "Successfully connected", LastTransitionTime: now},
	}

	assert.False(t, conditionsEqual(unhealthy, healthy), "Unhealthy and healthy conditions should not be equal")
}
