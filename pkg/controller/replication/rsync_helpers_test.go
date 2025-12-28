package replication

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestContains_Found(t *testing.T) {
	slice := []string{"apple", "banana", "cherry"}

	assert.True(t, contains(slice, "apple"), "Should find 'apple' in slice")
	assert.True(t, contains(slice, "banana"), "Should find 'banana' in slice")
	assert.True(t, contains(slice, "cherry"), "Should find 'cherry' in slice")
}

func TestContains_NotFound(t *testing.T) {
	slice := []string{"apple", "banana", "cherry"}

	assert.False(t, contains(slice, "orange"), "Should not find 'orange' in slice")
	assert.False(t, contains(slice, "grape"), "Should not find 'grape' in slice")
	assert.False(t, contains(slice, ""), "Should not find empty string in slice")
}

func TestContains_EmptySlice(t *testing.T) {
	var emptySlice []string

	assert.False(t, contains(emptySlice, "apple"), "Empty slice should not contain any value")
	assert.False(t, contains(emptySlice, ""), "Empty slice should not contain empty string")
}

func TestContains_SingleElement(t *testing.T) {
	slice := []string{"only"}

	assert.True(t, contains(slice, "only"), "Should find the only element")
	assert.False(t, contains(slice, "other"), "Should not find non-existent element")
}

func TestContains_CaseSensitive(t *testing.T) {
	slice := []string{"Apple", "Banana"}

	assert.True(t, contains(slice, "Apple"), "Should find exact case match")
	assert.False(t, contains(slice, "apple"), "Should not find different case")
	assert.False(t, contains(slice, "APPLE"), "Should not find uppercase")
}

func TestContains_SpecialCharacters(t *testing.T) {
	slice := []string{"node-1", "node.2", "node/3", "node_4"}

	assert.True(t, contains(slice, "node-1"), "Should handle hyphens")
	assert.True(t, contains(slice, "node.2"), "Should handle dots")
	assert.True(t, contains(slice, "node/3"), "Should handle slashes")
	assert.True(t, contains(slice, "node_4"), "Should handle underscores")
}

func TestContains_Duplicates(t *testing.T) {
	slice := []string{"node1", "node1", "node2"}

	assert.True(t, contains(slice, "node1"), "Should find value even with duplicates")
	assert.True(t, contains(slice, "node2"), "Should find unique value")
}

func TestPVCLockInfo_Struct(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)

	lockInfo := &PVCLockInfo{
		ControllerPodName: "dr-syncer-controller-abc123",
		Timestamp:         now,
	}

	assert.Equal(t, "dr-syncer-controller-abc123", lockInfo.ControllerPodName)
	assert.Equal(t, now, lockInfo.Timestamp)
}

func TestPVCLockInfo_EmptyFields(t *testing.T) {
	lockInfo := &PVCLockInfo{}

	assert.Equal(t, "", lockInfo.ControllerPodName)
	assert.Equal(t, "", lockInfo.Timestamp)
}

func TestPVCLockInfo_PartialFields(t *testing.T) {
	// Only controller pod name set
	lockInfo1 := &PVCLockInfo{
		ControllerPodName: "test-pod",
	}
	assert.Equal(t, "test-pod", lockInfo1.ControllerPodName)
	assert.Equal(t, "", lockInfo1.Timestamp)

	// Only timestamp set
	lockInfo2 := &PVCLockInfo{
		Timestamp: "2025-01-01T00:00:00Z",
	}
	assert.Equal(t, "", lockInfo2.ControllerPodName)
	assert.Equal(t, "2025-01-01T00:00:00Z", lockInfo2.Timestamp)
}

func TestContains_NodeNames(t *testing.T) {
	// Test with realistic Kubernetes node names
	nodes := []string{
		"ip-10-0-1-100.us-west-2.compute.internal",
		"worker-node-01",
		"gke-cluster-default-pool-abc123-xyz",
		"aks-nodepool1-12345678-vmss000000",
	}

	assert.True(t, contains(nodes, "ip-10-0-1-100.us-west-2.compute.internal"), "Should find AWS node name")
	assert.True(t, contains(nodes, "worker-node-01"), "Should find simple node name")
	assert.True(t, contains(nodes, "gke-cluster-default-pool-abc123-xyz"), "Should find GKE node name")
	assert.True(t, contains(nodes, "aks-nodepool1-12345678-vmss000000"), "Should find AKS node name")
	assert.False(t, contains(nodes, "unknown-node"), "Should not find unknown node")
}

func TestContains_PVCNames(t *testing.T) {
	// Test with realistic PVC names
	pvcs := []string{
		"data-postgres-0",
		"www-nginx-deployment-abc123",
		"redis-data",
	}

	assert.True(t, contains(pvcs, "data-postgres-0"), "Should find statefulset PVC")
	assert.True(t, contains(pvcs, "www-nginx-deployment-abc123"), "Should find deployment PVC")
	assert.True(t, contains(pvcs, "redis-data"), "Should find simple PVC")
	assert.False(t, contains(pvcs, "data-postgres-1"), "Should not find different index")
}

func TestPVCLockInfo_ValidTimestamp(t *testing.T) {
	timestamp := "2025-01-15T10:30:00Z"
	lockInfo := &PVCLockInfo{
		ControllerPodName: "controller-pod",
		Timestamp:         timestamp,
	}

	// Verify timestamp can be parsed
	_, err := time.Parse(time.RFC3339, lockInfo.Timestamp)
	assert.NoError(t, err, "Timestamp should be valid RFC3339 format")
}

func TestPVCClusterKey_Type(t *testing.T) {
	// Test that the PVCClusterKey constant has expected type and value
	var key PVCClusterContextKey = PVCClusterKey

	assert.Equal(t, PVCClusterContextKey("pvcCluster"), key)
}
