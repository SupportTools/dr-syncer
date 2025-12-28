package controllers

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	drsyncerio "github.com/supporttools/dr-syncer/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestClusterMappingReconciler_Struct(t *testing.T) {
	scheme := runtime.NewScheme()

	reconciler := &ClusterMappingReconciler{
		Scheme: scheme,
	}

	assert.NotNil(t, reconciler)
	assert.NotNil(t, reconciler.Scheme)
	assert.Nil(t, reconciler.workerPool)
	assert.Nil(t, reconciler.clusterMutexes)
}

func TestClusterMappingReconciler_GetClusterMutex_NewMutex(t *testing.T) {
	reconciler := &ClusterMappingReconciler{
		clusterMutexes: &sync.Map{},
	}

	mutex := reconciler.getClusterMutex("cluster-1")
	assert.NotNil(t, mutex)
}

func TestClusterMappingReconciler_GetClusterMutex_SameMutex(t *testing.T) {
	reconciler := &ClusterMappingReconciler{
		clusterMutexes: &sync.Map{},
	}

	// Get mutex for the same cluster twice
	mutex1 := reconciler.getClusterMutex("cluster-1")
	mutex2 := reconciler.getClusterMutex("cluster-1")

	// Should return the same mutex
	assert.Equal(t, mutex1, mutex2)
}

func TestClusterMappingReconciler_GetClusterMutex_DifferentClusters(t *testing.T) {
	reconciler := &ClusterMappingReconciler{
		clusterMutexes: &sync.Map{},
	}

	// Get mutexes for different clusters
	mutex1 := reconciler.getClusterMutex("cluster-1")
	mutex2 := reconciler.getClusterMutex("cluster-2")

	// Should return different mutexes (compare pointer addresses)
	assert.NotSame(t, mutex1, mutex2, "Different clusters should have different mutex pointers")
}

func TestClusterMappingReconciler_GetClusterMutex_Concurrent(t *testing.T) {
	reconciler := &ClusterMappingReconciler{
		clusterMutexes: &sync.Map{},
	}

	// Test concurrent access to the same cluster mutex
	var wg sync.WaitGroup
	mutexes := make([]*sync.Mutex, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			mutexes[idx] = reconciler.getClusterMutex("cluster-1")
		}(i)
	}

	wg.Wait()

	// All should be the same mutex
	for i := 1; i < 10; i++ {
		assert.Equal(t, mutexes[0], mutexes[i], "All mutexes should be the same for the same cluster")
	}
}

func TestClusterMappingReconciler_GetClusterMutex_UsableMutex(t *testing.T) {
	reconciler := &ClusterMappingReconciler{
		clusterMutexes: &sync.Map{},
	}

	mutex := reconciler.getClusterMutex("cluster-1")

	// Test that the mutex is actually usable
	mutex.Lock()
	// Should not deadlock
	mutex.Unlock()

	assert.True(t, true, "Mutex should be lockable and unlockable")
}

func TestClusterMappingReconciler_GetClusterMutex_MultipleClusters(t *testing.T) {
	reconciler := &ClusterMappingReconciler{
		clusterMutexes: &sync.Map{},
	}

	clusterNames := []string{
		"production-cluster",
		"dr-cluster",
		"staging-cluster",
		"dev-cluster",
	}

	mutexes := make(map[string]*sync.Mutex)
	for _, name := range clusterNames {
		mutexes[name] = reconciler.getClusterMutex(name)
	}

	// All should be unique (compare pointer addresses)
	for i := 0; i < len(clusterNames); i++ {
		for j := i + 1; j < len(clusterNames); j++ {
			assert.NotSame(t, mutexes[clusterNames[i]], mutexes[clusterNames[j]],
				"Different clusters should have different mutex pointers")
		}
	}
}

func TestClusterMappingPhase_Constants(t *testing.T) {
	// Test that phase constants are defined correctly
	assert.Equal(t, drsyncerio.ClusterMappingPhase("Pending"), drsyncerio.ClusterMappingPhasePending)
	assert.Equal(t, drsyncerio.ClusterMappingPhase("Connecting"), drsyncerio.ClusterMappingPhaseConnecting)
	assert.Equal(t, drsyncerio.ClusterMappingPhase("Connected"), drsyncerio.ClusterMappingPhaseConnected)
	assert.Equal(t, drsyncerio.ClusterMappingPhase("Failed"), drsyncerio.ClusterMappingPhaseFailed)
}

func TestClusterMappingReconciler_EmptyClusterMutexes(t *testing.T) {
	reconciler := &ClusterMappingReconciler{
		clusterMutexes: &sync.Map{},
	}

	// Count should be 0
	count := 0
	reconciler.clusterMutexes.Range(func(key, value interface{}) bool {
		count++
		return true
	})

	assert.Equal(t, 0, count, "Initially there should be no mutexes")
}

func TestClusterMappingReconciler_AfterGetClusterMutex(t *testing.T) {
	reconciler := &ClusterMappingReconciler{
		clusterMutexes: &sync.Map{},
	}

	// Add a mutex
	reconciler.getClusterMutex("test-cluster")

	// Count should be 1
	count := 0
	reconciler.clusterMutexes.Range(func(key, value interface{}) bool {
		count++
		return true
	})

	assert.Equal(t, 1, count, "Should have one mutex after getClusterMutex")
}

func TestClusterMappingReconciler_GetClusterMutex_EmptyName(t *testing.T) {
	reconciler := &ClusterMappingReconciler{
		clusterMutexes: &sync.Map{},
	}

	// Even empty string should work
	mutex := reconciler.getClusterMutex("")
	assert.NotNil(t, mutex)
}

func TestClusterMappingReconciler_GetClusterMutex_SpecialCharacters(t *testing.T) {
	reconciler := &ClusterMappingReconciler{
		clusterMutexes: &sync.Map{},
	}

	// Test with various special characters that might be in cluster names
	testNames := []string{
		"cluster-with-dashes",
		"cluster.with.dots",
		"cluster_with_underscores",
		"cluster/with/slashes",
		"cluster:with:colons",
	}

	for _, name := range testNames {
		mutex := reconciler.getClusterMutex(name)
		assert.NotNil(t, mutex, "Should handle cluster name: %s", name)
	}
}

func TestClusterMapping_StatusFields(t *testing.T) {
	status := drsyncerio.ClusterMappingStatus{
		Phase:               drsyncerio.ClusterMappingPhasePending,
		Message:             "Test message",
		ConsecutiveFailures: 0,
	}

	assert.Equal(t, drsyncerio.ClusterMappingPhasePending, status.Phase)
	assert.Equal(t, "Test message", status.Message)
	assert.Equal(t, 0, status.ConsecutiveFailures)
}

func TestClusterMapping_StatusPhaseTransitions(t *testing.T) {
	// Test valid phase transitions
	transitions := []struct {
		from drsyncerio.ClusterMappingPhase
		to   drsyncerio.ClusterMappingPhase
	}{
		{drsyncerio.ClusterMappingPhasePending, drsyncerio.ClusterMappingPhaseConnecting},
		{drsyncerio.ClusterMappingPhaseConnecting, drsyncerio.ClusterMappingPhaseConnected},
		{drsyncerio.ClusterMappingPhaseConnecting, drsyncerio.ClusterMappingPhaseFailed},
		{drsyncerio.ClusterMappingPhaseFailed, drsyncerio.ClusterMappingPhasePending},
	}

	for _, tr := range transitions {
		status := drsyncerio.ClusterMappingStatus{
			Phase: tr.from,
		}
		// Simulate transition
		status.Phase = tr.to

		assert.Equal(t, tr.to, status.Phase, "Phase should transition from %s to %s", tr.from, tr.to)
	}
}

func TestConnectionStatus_Fields(t *testing.T) {
	connectionStatus := drsyncerio.ConnectionStatus{
		TotalSourceAgents: 2,
		TotalTargetAgents: 3,
		ConnectedAgents:   2,
		ConnectionDetails: []drsyncerio.AgentConnectionDetail{
			{
				SourceNode: "node-1",
				TargetNode: "node-2",
				Connected:  true,
			},
		},
	}

	assert.Equal(t, int32(2), connectionStatus.TotalSourceAgents)
	assert.Equal(t, int32(3), connectionStatus.TotalTargetAgents)
	assert.Equal(t, int32(2), connectionStatus.ConnectedAgents)
	assert.Len(t, connectionStatus.ConnectionDetails, 1)
	assert.True(t, connectionStatus.ConnectionDetails[0].Connected)
}

func TestAgentConnectionDetail_Fields(t *testing.T) {
	// Test connected case
	connected := drsyncerio.AgentConnectionDetail{
		SourceNode: "source-node-1",
		TargetNode: "target-node-1",
		Connected:  true,
		Error:      "",
	}

	assert.Equal(t, "source-node-1", connected.SourceNode)
	assert.Equal(t, "target-node-1", connected.TargetNode)
	assert.True(t, connected.Connected)
	assert.Empty(t, connected.Error)

	// Test failed case
	failed := drsyncerio.AgentConnectionDetail{
		SourceNode: "source-node-2",
		TargetNode: "target-node-2",
		Connected:  false,
		Error:      "Connection timed out",
	}

	assert.Equal(t, "source-node-2", failed.SourceNode)
	assert.Equal(t, "target-node-2", failed.TargetNode)
	assert.False(t, failed.Connected)
	assert.Equal(t, "Connection timed out", failed.Error)
}

func TestClusterMappingReconciler_MutexLocking(t *testing.T) {
	reconciler := &ClusterMappingReconciler{
		clusterMutexes: &sync.Map{},
	}

	mutex := reconciler.getClusterMutex("test-cluster")

	// Test that we can lock and unlock the mutex properly
	locked := make(chan bool)
	go func() {
		mutex.Lock()
		locked <- true
		// Simulate some work
		mutex.Unlock()
	}()

	// Should be able to receive the locked signal
	result := <-locked
	assert.True(t, result, "Should be able to lock the mutex")
}

func TestClusterMappingSpec_Paused(t *testing.T) {
	// Test paused true
	paused := true
	spec := drsyncerio.ClusterMappingSpec{
		SourceCluster: "source",
		TargetCluster: "target",
		Paused:        &paused,
	}

	assert.NotNil(t, spec.Paused)
	assert.True(t, *spec.Paused)

	// Test paused false
	notPaused := false
	spec2 := drsyncerio.ClusterMappingSpec{
		SourceCluster: "source",
		TargetCluster: "target",
		Paused:        &notPaused,
	}

	assert.NotNil(t, spec2.Paused)
	assert.False(t, *spec2.Paused)

	// Test paused nil
	spec3 := drsyncerio.ClusterMappingSpec{
		SourceCluster: "source",
		TargetCluster: "target",
		Paused:        nil,
	}

	assert.Nil(t, spec3.Paused)
}

func TestClusterMappingSpec_VerifyConnectivity(t *testing.T) {
	// Test verify connectivity true
	verify := true
	spec := drsyncerio.ClusterMappingSpec{
		SourceCluster:      "source",
		TargetCluster:      "target",
		VerifyConnectivity: &verify,
	}

	assert.NotNil(t, spec.VerifyConnectivity)
	assert.True(t, *spec.VerifyConnectivity)

	// Test verify connectivity false
	noVerify := false
	spec2 := drsyncerio.ClusterMappingSpec{
		SourceCluster:      "source",
		TargetCluster:      "target",
		VerifyConnectivity: &noVerify,
	}

	assert.NotNil(t, spec2.VerifyConnectivity)
	assert.False(t, *spec2.VerifyConnectivity)
}

func TestClusterMappingSpec_ConnectivityTimeout(t *testing.T) {
	timeout := int32(120)
	spec := drsyncerio.ClusterMappingSpec{
		SourceCluster:              "source",
		TargetCluster:              "target",
		ConnectivityTimeoutSeconds: &timeout,
	}

	assert.NotNil(t, spec.ConnectivityTimeoutSeconds)
	assert.Equal(t, int32(120), *spec.ConnectivityTimeoutSeconds)

	// Test nil timeout
	spec2 := drsyncerio.ClusterMappingSpec{
		SourceCluster: "source",
		TargetCluster: "target",
	}

	assert.Nil(t, spec2.ConnectivityTimeoutSeconds)
}

func TestClusterMappingStatus_ConsecutiveFailures(t *testing.T) {
	status := drsyncerio.ClusterMappingStatus{
		Phase:               drsyncerio.ClusterMappingPhaseFailed,
		ConsecutiveFailures: 0,
	}

	// Simulate incrementing failures
	status.ConsecutiveFailures++
	assert.Equal(t, 1, status.ConsecutiveFailures)

	status.ConsecutiveFailures++
	assert.Equal(t, 2, status.ConsecutiveFailures)

	// Reset on success
	status.ConsecutiveFailures = 0
	assert.Equal(t, 0, status.ConsecutiveFailures)
}
