package controllers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	drsyncerio "github.com/supporttools/dr-syncer/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestNamespaceMappingReconciler_Struct(t *testing.T) {
	scheme := runtime.NewScheme()

	reconciler := &NamespaceMappingReconciler{
		Scheme: scheme,
	}

	assert.NotNil(t, reconciler)
	assert.NotNil(t, reconciler.Scheme)
}

func TestNamespaceMappingReconciler_SchemeWithTypes(t *testing.T) {
	scheme := runtime.NewScheme()
	err := drsyncerio.AddToScheme(scheme)
	assert.NoError(t, err)

	reconciler := &NamespaceMappingReconciler{
		Scheme: scheme,
	}

	assert.NotNil(t, reconciler)
	assert.NotNil(t, reconciler.Scheme)
}

func TestNamespaceMappingFinalizerName(t *testing.T) {
	assert.Equal(t, "dr-syncer.io/cleanup-namespacemapping", NamespaceMappingFinalizerName)
}

func TestContainsString_EmptySlice(t *testing.T) {
	var slice []string
	assert.False(t, containsString(slice, "test"))
}

func TestContainsString_NilSlice(t *testing.T) {
	assert.False(t, containsString(nil, "test"))
}

func TestContainsString_Found(t *testing.T) {
	slice := []string{"one", "two", "three"}
	assert.True(t, containsString(slice, "two"))
}

func TestContainsString_NotFound(t *testing.T) {
	slice := []string{"one", "two", "three"}
	assert.False(t, containsString(slice, "four"))
}

func TestContainsString_EmptyString(t *testing.T) {
	slice := []string{"one", "", "three"}
	assert.True(t, containsString(slice, ""))
}

func TestContainsString_EmptyStringNotInSlice(t *testing.T) {
	slice := []string{"one", "two", "three"}
	assert.False(t, containsString(slice, ""))
}

func TestContainsString_FirstElement(t *testing.T) {
	slice := []string{"one", "two", "three"}
	assert.True(t, containsString(slice, "one"))
}

func TestContainsString_LastElement(t *testing.T) {
	slice := []string{"one", "two", "three"}
	assert.True(t, containsString(slice, "three"))
}

func TestContainsString_SingleElement(t *testing.T) {
	slice := []string{"only"}
	assert.True(t, containsString(slice, "only"))
	assert.False(t, containsString(slice, "other"))
}

func TestContainsString_CaseSensitive(t *testing.T) {
	slice := []string{"One", "Two", "Three"}
	assert.True(t, containsString(slice, "One"))
	assert.False(t, containsString(slice, "one"))
	assert.False(t, containsString(slice, "ONE"))
}

func TestContainsString_WithFinalizer(t *testing.T) {
	finalizers := []string{
		"kubernetes.io/pvc-protection",
		NamespaceMappingFinalizerName,
		"other-finalizer",
	}
	assert.True(t, containsString(finalizers, NamespaceMappingFinalizerName))
	assert.False(t, containsString(finalizers, "nonexistent"))
}

func TestContainsString_Duplicates(t *testing.T) {
	slice := []string{"dup", "dup", "dup"}
	assert.True(t, containsString(slice, "dup"))
}

func TestRemoveString_EmptySlice(t *testing.T) {
	var slice []string
	result := removeString(slice, "test")
	assert.Empty(t, result)
}

func TestRemoveString_NilSlice(t *testing.T) {
	result := removeString(nil, "test")
	assert.Empty(t, result)
}

func TestRemoveString_RemoveExisting(t *testing.T) {
	slice := []string{"one", "two", "three"}
	result := removeString(slice, "two")

	assert.Len(t, result, 2)
	assert.Contains(t, result, "one")
	assert.Contains(t, result, "three")
	assert.NotContains(t, result, "two")
}

func TestRemoveString_RemoveNonExisting(t *testing.T) {
	slice := []string{"one", "two", "three"}
	result := removeString(slice, "four")

	assert.Len(t, result, 3)
	assert.Equal(t, slice, result)
}

func TestRemoveString_RemoveFirst(t *testing.T) {
	slice := []string{"one", "two", "three"}
	result := removeString(slice, "one")

	assert.Len(t, result, 2)
	assert.Equal(t, []string{"two", "three"}, result)
}

func TestRemoveString_RemoveLast(t *testing.T) {
	slice := []string{"one", "two", "three"}
	result := removeString(slice, "three")

	assert.Len(t, result, 2)
	assert.Equal(t, []string{"one", "two"}, result)
}

func TestRemoveString_RemoveOnlyElement(t *testing.T) {
	slice := []string{"only"}
	result := removeString(slice, "only")

	assert.Empty(t, result)
}

func TestRemoveString_RemoveAllDuplicates(t *testing.T) {
	slice := []string{"dup", "other", "dup", "another", "dup"}
	result := removeString(slice, "dup")

	assert.Len(t, result, 2)
	assert.Equal(t, []string{"other", "another"}, result)
}

func TestRemoveString_PreservesOrder(t *testing.T) {
	slice := []string{"a", "b", "c", "d", "e"}
	result := removeString(slice, "c")

	assert.Equal(t, []string{"a", "b", "d", "e"}, result)
}

func TestRemoveString_EmptyString(t *testing.T) {
	slice := []string{"one", "", "three"}
	result := removeString(slice, "")

	assert.Len(t, result, 2)
	assert.Equal(t, []string{"one", "three"}, result)
}

func TestRemoveString_CaseSensitive(t *testing.T) {
	slice := []string{"One", "Two", "Three"}
	result := removeString(slice, "one") // lowercase

	assert.Len(t, result, 3)
	assert.Equal(t, slice, result) // No removal since case doesn't match
}

func TestRemoveString_WithFinalizer(t *testing.T) {
	finalizers := []string{
		"kubernetes.io/pvc-protection",
		NamespaceMappingFinalizerName,
		"other-finalizer",
	}
	result := removeString(finalizers, NamespaceMappingFinalizerName)

	assert.Len(t, result, 2)
	assert.Contains(t, result, "kubernetes.io/pvc-protection")
	assert.Contains(t, result, "other-finalizer")
	assert.NotContains(t, result, NamespaceMappingFinalizerName)
}

func TestRemoveString_DoesNotModifyOriginal(t *testing.T) {
	original := []string{"one", "two", "three"}
	originalCopy := make([]string, len(original))
	copy(originalCopy, original)

	_ = removeString(original, "two")

	// Original should be unchanged
	assert.Equal(t, originalCopy, original)
}

func TestRemoveString_ReturnsNewSlice(t *testing.T) {
	original := []string{"one", "two", "three"}
	result := removeString(original, "two")

	// Verify they are different slices (different backing arrays)
	// by modifying result and checking original is unchanged
	result[0] = "modified"
	assert.Equal(t, "one", original[0])
}

func TestNamespaceMappingSpec_ReplicationModes(t *testing.T) {
	// Test all replication modes are correctly defined
	assert.Equal(t, drsyncerio.ReplicationMode("Scheduled"), drsyncerio.ScheduledMode)
	assert.Equal(t, drsyncerio.ReplicationMode("Continuous"), drsyncerio.ContinuousMode)
	assert.Equal(t, drsyncerio.ReplicationMode("Manual"), drsyncerio.ManualMode)
}

func TestNamespaceMappingSpec_Paused(t *testing.T) {
	// Test paused true
	paused := true
	spec := drsyncerio.NamespaceMappingSpec{
		SourceNamespace:      "source-ns",
		DestinationNamespace: "dest-ns",
		Paused:               &paused,
	}

	assert.NotNil(t, spec.Paused)
	assert.True(t, *spec.Paused)

	// Test paused false
	notPaused := false
	spec2 := drsyncerio.NamespaceMappingSpec{
		SourceNamespace:      "source-ns",
		DestinationNamespace: "dest-ns",
		Paused:               &notPaused,
	}

	assert.NotNil(t, spec2.Paused)
	assert.False(t, *spec2.Paused)

	// Test paused nil (default)
	spec3 := drsyncerio.NamespaceMappingSpec{
		SourceNamespace:      "source-ns",
		DestinationNamespace: "dest-ns",
	}

	assert.Nil(t, spec3.Paused)
}

func TestNamespaceMappingSpec_ClusterMappingRef(t *testing.T) {
	ref := &drsyncerio.ClusterMappingReference{
		Name:      "my-cluster-mapping",
		Namespace: "default",
	}

	spec := drsyncerio.NamespaceMappingSpec{
		SourceNamespace:      "source-ns",
		DestinationNamespace: "dest-ns",
		ClusterMappingRef:    ref,
	}

	assert.NotNil(t, spec.ClusterMappingRef)
	assert.Equal(t, "my-cluster-mapping", spec.ClusterMappingRef.Name)
	assert.Equal(t, "default", spec.ClusterMappingRef.Namespace)
}

func TestNamespaceMappingSpec_DirectClusterSpec(t *testing.T) {
	spec := drsyncerio.NamespaceMappingSpec{
		SourceNamespace:      "source-ns",
		DestinationNamespace: "dest-ns",
		SourceCluster:        "prod-cluster",
		DestinationCluster:   "dr-cluster",
	}

	assert.Equal(t, "source-ns", spec.SourceNamespace)
	assert.Equal(t, "dest-ns", spec.DestinationNamespace)
	assert.Equal(t, "prod-cluster", spec.SourceCluster)
	assert.Equal(t, "dr-cluster", spec.DestinationCluster)
}

func TestNamespaceMappingStatus_Fields(t *testing.T) {
	now := metav1.Now()
	status := drsyncerio.NamespaceMappingStatus{
		LastSyncTime: &now,
		NextSyncTime: &now,
		Phase:        drsyncerio.SyncPhaseRunning,
	}

	assert.NotNil(t, status.LastSyncTime)
	assert.NotNil(t, status.NextSyncTime)
	assert.Equal(t, drsyncerio.SyncPhaseRunning, status.Phase)
}

func TestNamespaceMappingStatus_SyncStats(t *testing.T) {
	status := drsyncerio.NamespaceMappingStatus{
		Phase: drsyncerio.SyncPhaseCompleted,
		SyncStats: &drsyncerio.SyncStats{
			TotalResources:   10,
			SuccessfulSyncs:  8,
			FailedSyncs:      2,
			LastSyncDuration: "1m30s",
		},
	}

	assert.NotNil(t, status.SyncStats)
	assert.Equal(t, int32(10), status.SyncStats.TotalResources)
	assert.Equal(t, int32(8), status.SyncStats.SuccessfulSyncs)
	assert.Equal(t, int32(2), status.SyncStats.FailedSyncs)
	assert.Equal(t, "1m30s", status.SyncStats.LastSyncDuration)
}

func TestNamespaceMapping_Finalizers(t *testing.T) {
	nm := &drsyncerio.NamespaceMapping{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-mapping",
			Namespace:  "default",
			Finalizers: []string{},
		},
	}

	// Initially no finalizer
	assert.False(t, containsString(nm.Finalizers, NamespaceMappingFinalizerName))

	// Add finalizer
	nm.Finalizers = append(nm.Finalizers, NamespaceMappingFinalizerName)
	assert.True(t, containsString(nm.Finalizers, NamespaceMappingFinalizerName))

	// Remove finalizer
	nm.Finalizers = removeString(nm.Finalizers, NamespaceMappingFinalizerName)
	assert.False(t, containsString(nm.Finalizers, NamespaceMappingFinalizerName))
}

func TestNamespaceMapping_WithMultipleFinalizers(t *testing.T) {
	nm := &drsyncerio.NamespaceMapping{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-mapping",
			Namespace: "default",
			Finalizers: []string{
				"kubernetes.io/pvc-protection",
				NamespaceMappingFinalizerName,
				"another-controller/finalizer",
			},
		},
	}

	// Verify our finalizer is present
	assert.True(t, containsString(nm.Finalizers, NamespaceMappingFinalizerName))
	assert.Len(t, nm.Finalizers, 3)

	// Remove only our finalizer
	nm.Finalizers = removeString(nm.Finalizers, NamespaceMappingFinalizerName)

	// Other finalizers should remain
	assert.Len(t, nm.Finalizers, 2)
	assert.True(t, containsString(nm.Finalizers, "kubernetes.io/pvc-protection"))
	assert.True(t, containsString(nm.Finalizers, "another-controller/finalizer"))
	assert.False(t, containsString(nm.Finalizers, NamespaceMappingFinalizerName))
}

func TestNamespaceMappingSpec_ResourceTypes(t *testing.T) {
	spec := drsyncerio.NamespaceMappingSpec{
		SourceNamespace:      "source-ns",
		DestinationNamespace: "dest-ns",
		ResourceTypes:        []string{"ConfigMap", "Secret", "Deployment"},
	}

	assert.Len(t, spec.ResourceTypes, 3)
	assert.Equal(t, "ConfigMap", spec.ResourceTypes[0])
	assert.Equal(t, "Secret", spec.ResourceTypes[1])
	assert.Equal(t, "Deployment", spec.ResourceTypes[2])
}

func TestNamespaceMappingSpec_Schedule(t *testing.T) {
	spec := drsyncerio.NamespaceMappingSpec{
		SourceNamespace:      "source-ns",
		DestinationNamespace: "dest-ns",
		Schedule:             "*/5 * * * *",
		ReplicationMode:      drsyncerio.ScheduledMode,
	}

	assert.Equal(t, "*/5 * * * *", spec.Schedule)
	assert.Equal(t, drsyncerio.ScheduledMode, spec.ReplicationMode)
}

func TestNamespaceMappingSpec_AllReplicationModes(t *testing.T) {
	modes := []drsyncerio.ReplicationMode{
		drsyncerio.ScheduledMode,
		drsyncerio.ContinuousMode,
		drsyncerio.ManualMode,
	}

	for _, mode := range modes {
		spec := drsyncerio.NamespaceMappingSpec{
			SourceNamespace:      "source-ns",
			DestinationNamespace: "dest-ns",
			ReplicationMode:      mode,
		}
		assert.Equal(t, mode, spec.ReplicationMode)
	}
}

func TestContainsString_RealisticFinalizers(t *testing.T) {
	testCases := []struct {
		finalizers []string
		search     string
		expected   bool
	}{
		{
			finalizers: []string{},
			search:     NamespaceMappingFinalizerName,
			expected:   false,
		},
		{
			finalizers: []string{NamespaceMappingFinalizerName},
			search:     NamespaceMappingFinalizerName,
			expected:   true,
		},
		{
			finalizers: []string{"foregroundDeletion"},
			search:     NamespaceMappingFinalizerName,
			expected:   false,
		},
		{
			finalizers: []string{
				"kubernetes.io/pv-protection",
				"snapshot.storage.kubernetes.io/volumesnapshot-as-source-protection",
				NamespaceMappingFinalizerName,
			},
			search:   NamespaceMappingFinalizerName,
			expected: true,
		},
	}

	for _, tc := range testCases {
		result := containsString(tc.finalizers, tc.search)
		assert.Equal(t, tc.expected, result, "containsString(%v, %s)", tc.finalizers, tc.search)
	}
}

func TestRemoveString_RealisticFinalizers(t *testing.T) {
	testCases := []struct {
		name       string
		finalizers []string
		remove     string
		expected   []string
	}{
		{
			name:       "remove from empty",
			finalizers: []string{},
			remove:     NamespaceMappingFinalizerName,
			expected:   []string{},
		},
		{
			name:       "remove only finalizer",
			finalizers: []string{NamespaceMappingFinalizerName},
			remove:     NamespaceMappingFinalizerName,
			expected:   []string{},
		},
		{
			name:       "remove non-existent",
			finalizers: []string{"foregroundDeletion"},
			remove:     NamespaceMappingFinalizerName,
			expected:   []string{"foregroundDeletion"},
		},
		{
			name: "remove from multiple",
			finalizers: []string{
				"kubernetes.io/pv-protection",
				NamespaceMappingFinalizerName,
				"snapshot.storage.kubernetes.io/volumesnapshot-as-source-protection",
			},
			remove: NamespaceMappingFinalizerName,
			expected: []string{
				"kubernetes.io/pv-protection",
				"snapshot.storage.kubernetes.io/volumesnapshot-as-source-protection",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := removeString(tc.finalizers, tc.remove)
			assert.Equal(t, tc.expected, result)
		})
	}
}
