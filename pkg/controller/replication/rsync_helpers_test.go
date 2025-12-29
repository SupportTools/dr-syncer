package replication

import (
	"encoding/json"
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

// Mount Path Cache Tests

func TestMountPathCache_Struct(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)

	cache := &MountPathCache{
		Path:        "/var/lib/kubelet/pods/abc123/volumes/kubernetes.io~csi/pvc-xyz/mount",
		NodeName:    "worker-node-01",
		AgentPodUID: "pod-uid-12345",
		Timestamp:   now,
	}

	assert.Equal(t, "/var/lib/kubelet/pods/abc123/volumes/kubernetes.io~csi/pvc-xyz/mount", cache.Path)
	assert.Equal(t, "worker-node-01", cache.NodeName)
	assert.Equal(t, "pod-uid-12345", cache.AgentPodUID)
	assert.Equal(t, now, cache.Timestamp)
}

func TestMountPathCache_EmptyFields(t *testing.T) {
	cache := &MountPathCache{}

	assert.Equal(t, "", cache.Path)
	assert.Equal(t, "", cache.NodeName)
	assert.Equal(t, "", cache.AgentPodUID)
	assert.Equal(t, "", cache.Timestamp)
}

func TestMountPathCache_JSONMarshal(t *testing.T) {
	cache := MountPathCache{
		Path:        "/data/mount",
		NodeName:    "node-1",
		AgentPodUID: "uid-123",
		Timestamp:   "2025-12-29T10:00:00Z",
	}

	// Test JSON marshaling
	data, err := json.Marshal(cache)
	assert.NoError(t, err)
	assert.Contains(t, string(data), `"path":"/data/mount"`)
	assert.Contains(t, string(data), `"nodeName":"node-1"`)
	assert.Contains(t, string(data), `"agentPodUID":"uid-123"`)
	assert.Contains(t, string(data), `"timestamp":"2025-12-29T10:00:00Z"`)
}

func TestMountPathCache_JSONUnmarshal(t *testing.T) {
	jsonData := `{"path":"/data/mount","nodeName":"node-1","agentPodUID":"uid-123","timestamp":"2025-12-29T10:00:00Z"}`

	var cache MountPathCache
	err := json.Unmarshal([]byte(jsonData), &cache)

	assert.NoError(t, err)
	assert.Equal(t, "/data/mount", cache.Path)
	assert.Equal(t, "node-1", cache.NodeName)
	assert.Equal(t, "uid-123", cache.AgentPodUID)
	assert.Equal(t, "2025-12-29T10:00:00Z", cache.Timestamp)
}

func TestMountPathCache_JSONUnmarshal_InvalidJSON(t *testing.T) {
	invalidJSON := `{"path": "missing closing brace"`

	var cache MountPathCache
	err := json.Unmarshal([]byte(invalidJSON), &cache)

	assert.Error(t, err)
}

func TestMountPathCache_Constants(t *testing.T) {
	// Test that the constants are defined correctly
	assert.Equal(t, "dr-syncer.io/mount-path-cache", MountPathCacheAnnotation)
	assert.Equal(t, 1*time.Hour, MountPathCacheTTL)
}

func TestMountPathCache_TimestampValidation(t *testing.T) {
	// Valid timestamp
	validTimestamp := "2025-12-29T10:00:00Z"
	_, err := time.Parse(time.RFC3339, validTimestamp)
	assert.NoError(t, err, "Valid RFC3339 timestamp should parse")

	// Invalid timestamp
	invalidTimestamp := "2025-12-29 10:00:00"
	_, err = time.Parse(time.RFC3339, invalidTimestamp)
	assert.Error(t, err, "Invalid RFC3339 timestamp should fail to parse")
}

func TestMountPathCache_TTLExpiration(t *testing.T) {
	// Create a timestamp that is 30 minutes old (not expired)
	recentTime := time.Now().Add(-30 * time.Minute)
	recentCache := MountPathCache{
		Path:        "/data/mount",
		NodeName:    "node-1",
		AgentPodUID: "uid-123",
		Timestamp:   recentTime.Format(time.RFC3339),
	}

	cacheTime, err := time.Parse(time.RFC3339, recentCache.Timestamp)
	assert.NoError(t, err)
	assert.False(t, time.Since(cacheTime) > MountPathCacheTTL, "30-minute-old cache should not be expired")

	// Create a timestamp that is 2 hours old (expired)
	expiredTime := time.Now().Add(-2 * time.Hour)
	expiredCache := MountPathCache{
		Path:        "/data/mount",
		NodeName:    "node-1",
		AgentPodUID: "uid-123",
		Timestamp:   expiredTime.Format(time.RFC3339),
	}

	expiredCacheTime, err := time.Parse(time.RFC3339, expiredCache.Timestamp)
	assert.NoError(t, err)
	assert.True(t, time.Since(expiredCacheTime) > MountPathCacheTTL, "2-hour-old cache should be expired")
}

func TestMountPathCache_RealisticPaths(t *testing.T) {
	// Test with realistic Kubernetes mount paths
	testPaths := []string{
		"/var/lib/kubelet/pods/abc123-def456/volumes/kubernetes.io~csi/pvc-xyz789/mount",
		"/var/lib/kubelet/pods/12345678-1234-1234-1234-123456789abc/volumes/kubernetes.io~nfs/my-nfs-pvc/mount",
		"/var/lib/kubelet/plugins/kubernetes.io/local-volume/mounts/pvc-local-123",
	}

	for _, path := range testPaths {
		cache := MountPathCache{
			Path:        path,
			NodeName:    "worker-1",
			AgentPodUID: "uid-test",
			Timestamp:   time.Now().Format(time.RFC3339),
		}
		assert.Equal(t, path, cache.Path, "Path should be stored correctly: %s", path)
	}
}

func TestMountPathCache_NodeNameVariations(t *testing.T) {
	// Test with various Kubernetes node name formats
	nodeNames := []string{
		"worker-node-01",
		"ip-10-0-1-100.us-west-2.compute.internal",
		"gke-cluster-default-pool-abc123-xyz",
		"aks-nodepool1-12345678-vmss000000",
		"k3s-agent-1",
	}

	for _, nodeName := range nodeNames {
		cache := MountPathCache{
			Path:        "/data/mount",
			NodeName:    nodeName,
			AgentPodUID: "uid-test",
			Timestamp:   time.Now().Format(time.RFC3339),
		}
		assert.Equal(t, nodeName, cache.NodeName, "Node name should be stored correctly: %s", nodeName)
	}
}
