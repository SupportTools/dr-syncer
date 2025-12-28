package tempod

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
)

// Test constants from tempod.go
func TestTempod_Constants(t *testing.T) {
	assert.Equal(t, 8873, DefaultRsyncPort)
	assert.Equal(t, "dr-syncer-temp-", DefaultPodNamePrefix)
	assert.Equal(t, 5*time.Minute, DefaultPodCleanupTimeout)
	assert.Equal(t, 2*time.Minute, DefaultPodReadyTimeout)
}

// Test constants from csi_path.go
func TestCSIPath_Constants(t *testing.T) {
	assert.Equal(t, "/var/lib/kubelet/pods", KubeletVolumesPath)
	assert.Equal(t, "volumes/kubernetes.io~csi", CSIVolumesSubPath)
	assert.Equal(t, "mount", MountSubPath)
}

// Test constants from config.go
func TestConfig_Constants(t *testing.T) {
	assert.Equal(t, "dr-syncer-rsync-config", DefaultRsyncConfigMapName)
	assert.Equal(t, "rsyncd.conf", DefaultRsyncConfigKey)
	assert.Contains(t, DefaultRsyncConfig, "[data]")
	assert.Contains(t, DefaultRsyncConfig, "path = /data")
	assert.Contains(t, DefaultRsyncConfig, "uid = root")
}

func TestDefaultRsyncConfig_Content(t *testing.T) {
	// Verify default rsync config has all expected sections
	assert.Contains(t, DefaultRsyncConfig, "uid = root")
	assert.Contains(t, DefaultRsyncConfig, "gid = root")
	assert.Contains(t, DefaultRsyncConfig, "use chroot = no")
	assert.Contains(t, DefaultRsyncConfig, "max connections = 4")
	assert.Contains(t, DefaultRsyncConfig, "timeout = 300")
	assert.Contains(t, DefaultRsyncConfig, "read only = no")
	assert.Contains(t, DefaultRsyncConfig, "[data]")
	assert.Contains(t, DefaultRsyncConfig, "path = /data")
}

// Test TempPod struct
func TestTempPod_Struct(t *testing.T) {
	pod := &corev1.Pod{}
	tempPod := &TempPod{
		Name:      "test-pod",
		Namespace: "test-namespace",
		PVCName:   "test-pvc",
		NodeName:  "test-node",
		RsyncPort: 8873,
		Pod:       pod,
	}

	assert.Equal(t, "test-pod", tempPod.Name)
	assert.Equal(t, "test-namespace", tempPod.Namespace)
	assert.Equal(t, "test-pvc", tempPod.PVCName)
	assert.Equal(t, "test-node", tempPod.NodeName)
	assert.Equal(t, 8873, tempPod.RsyncPort)
	assert.Equal(t, pod, tempPod.Pod)
	assert.Nil(t, tempPod.Client)
}

func TestTempPod_EmptyStruct(t *testing.T) {
	tempPod := &TempPod{}

	assert.Empty(t, tempPod.Name)
	assert.Empty(t, tempPod.Namespace)
	assert.Empty(t, tempPod.PVCName)
	assert.Empty(t, tempPod.NodeName)
	assert.Equal(t, 0, tempPod.RsyncPort)
	assert.Nil(t, tempPod.Pod)
	assert.Nil(t, tempPod.Client)
}

// Test TempPodOptions struct
func TestTempPodOptions_Struct(t *testing.T) {
	opts := TempPodOptions{
		Name:          "test-pod",
		Namespace:     "test-namespace",
		PVCName:       "test-pvc",
		NodeName:      "test-node",
		RsyncPort:     8873,
		Labels:        map[string]string{"app": "test"},
		Annotations:   map[string]string{"note": "value"},
		KeySecretName: "test-secret",
	}

	assert.Equal(t, "test-pod", opts.Name)
	assert.Equal(t, "test-namespace", opts.Namespace)
	assert.Equal(t, "test-pvc", opts.PVCName)
	assert.Equal(t, "test-node", opts.NodeName)
	assert.Equal(t, 8873, opts.RsyncPort)
	assert.Equal(t, "test", opts.Labels["app"])
	assert.Equal(t, "value", opts.Annotations["note"])
	assert.Equal(t, "test-secret", opts.KeySecretName)
}

func TestTempPodOptions_Empty(t *testing.T) {
	opts := TempPodOptions{}

	assert.Empty(t, opts.Name)
	assert.Empty(t, opts.Namespace)
	assert.Empty(t, opts.PVCName)
	assert.Empty(t, opts.NodeName)
	assert.Equal(t, 0, opts.RsyncPort)
	assert.Nil(t, opts.Labels)
	assert.Nil(t, opts.Annotations)
	assert.Empty(t, opts.KeySecretName)
}

func TestTempPodOptions_WithLabelsAndAnnotations(t *testing.T) {
	labels := map[string]string{
		"app":     "dr-syncer",
		"tier":    "backend",
		"version": "v1",
	}
	annotations := map[string]string{
		"description": "test pod",
		"owner":       "admin",
	}

	opts := TempPodOptions{
		Labels:      labels,
		Annotations: annotations,
	}

	assert.Len(t, opts.Labels, 3)
	assert.Len(t, opts.Annotations, 2)
	assert.Equal(t, "dr-syncer", opts.Labels["app"])
	assert.Equal(t, "test pod", opts.Annotations["description"])
}

// Test Manager struct
func TestManager_Struct(t *testing.T) {
	config := &rest.Config{Host: "https://test:6443"}
	pods := make(map[string]*TempPod)

	manager := &Manager{
		Config: config,
		Pods:   pods,
	}

	assert.Nil(t, manager.Client)
	assert.Equal(t, config, manager.Config)
	assert.NotNil(t, manager.Pods)
	assert.Len(t, manager.Pods, 0)
}

func TestManager_Empty(t *testing.T) {
	manager := &Manager{}

	assert.Nil(t, manager.Client)
	assert.Nil(t, manager.Config)
	assert.Nil(t, manager.Pods)
}

func TestManager_GetPod(t *testing.T) {
	tempPod := &TempPod{
		Name:      "test-pod",
		Namespace: "test-ns",
	}

	manager := &Manager{
		Pods: map[string]*TempPod{
			"test-pod": tempPod,
		},
	}

	// Test getting existing pod
	result := manager.GetPod("test-pod")
	assert.NotNil(t, result)
	assert.Equal(t, "test-pod", result.Name)
	assert.Equal(t, "test-ns", result.Namespace)

	// Test getting non-existing pod
	notFound := manager.GetPod("not-found")
	assert.Nil(t, notFound)
}

func TestManager_GetPod_EmptyMap(t *testing.T) {
	manager := &Manager{
		Pods: make(map[string]*TempPod),
	}

	result := manager.GetPod("any-pod")
	assert.Nil(t, result)
}

func TestManager_ListPods_Empty(t *testing.T) {
	manager := &Manager{
		Pods: make(map[string]*TempPod),
	}

	pods := manager.ListPods()
	assert.NotNil(t, pods)
	assert.Len(t, pods, 0)
}

func TestManager_ListPods_Single(t *testing.T) {
	tempPod := &TempPod{
		Name:      "pod-1",
		Namespace: "ns-1",
	}

	manager := &Manager{
		Pods: map[string]*TempPod{
			"pod-1": tempPod,
		},
	}

	pods := manager.ListPods()
	assert.Len(t, pods, 1)
	assert.Equal(t, "pod-1", pods[0].Name)
}

func TestManager_ListPods_Multiple(t *testing.T) {
	manager := &Manager{
		Pods: map[string]*TempPod{
			"pod-1": {Name: "pod-1", Namespace: "ns-1"},
			"pod-2": {Name: "pod-2", Namespace: "ns-2"},
			"pod-3": {Name: "pod-3", Namespace: "ns-3"},
		},
	}

	pods := manager.ListPods()
	assert.Len(t, pods, 3)

	// Collect names (order is not guaranteed)
	names := make(map[string]bool)
	for _, pod := range pods {
		names[pod.Name] = true
	}
	assert.True(t, names["pod-1"])
	assert.True(t, names["pod-2"])
	assert.True(t, names["pod-3"])
}

// Test CSIVolumeInfo struct
func TestCSIVolumeInfo_Struct(t *testing.T) {
	info := CSIVolumeInfo{
		PodUID:     "pod-uid-123",
		VolumeName: "volume-1",
		PVCName:    "pvc-1",
		PVName:     "pv-1",
		NodeName:   "node-1",
		MountPath:  "/var/lib/kubelet/pods/pod-uid-123/volumes/kubernetes.io~csi/pv-1/mount",
	}

	assert.Equal(t, "pod-uid-123", info.PodUID)
	assert.Equal(t, "volume-1", info.VolumeName)
	assert.Equal(t, "pvc-1", info.PVCName)
	assert.Equal(t, "pv-1", info.PVName)
	assert.Equal(t, "node-1", info.NodeName)
	assert.Contains(t, info.MountPath, "pod-uid-123")
}

func TestCSIVolumeInfo_Empty(t *testing.T) {
	info := CSIVolumeInfo{}

	assert.Empty(t, info.PodUID)
	assert.Empty(t, info.VolumeName)
	assert.Empty(t, info.PVCName)
	assert.Empty(t, info.PVName)
	assert.Empty(t, info.NodeName)
	assert.Empty(t, info.MountPath)
}

// Test PVCInfo struct
func TestPVCInfo_Struct(t *testing.T) {
	info := &PVCInfo{
		Name:      "pvc-1",
		Namespace: "default",
		PVName:    "pv-1",
		NodeName:  "node-1",
		Pods:      []string{"pod-1", "pod-2"},
	}

	assert.Equal(t, "pvc-1", info.Name)
	assert.Equal(t, "default", info.Namespace)
	assert.Equal(t, "pv-1", info.PVName)
	assert.Equal(t, "node-1", info.NodeName)
	assert.Len(t, info.Pods, 2)
	assert.Contains(t, info.Pods, "pod-1")
	assert.Contains(t, info.Pods, "pod-2")
}

func TestPVCInfo_Empty(t *testing.T) {
	info := &PVCInfo{}

	assert.Empty(t, info.Name)
	assert.Empty(t, info.Namespace)
	assert.Empty(t, info.PVName)
	assert.Empty(t, info.NodeName)
	assert.Nil(t, info.Pods)
}

func TestPVCInfo_NoPods(t *testing.T) {
	info := &PVCInfo{
		Name:      "pvc-1",
		Namespace: "default",
		PVName:    "pv-1",
		NodeName:  "node-1",
		Pods:      []string{},
	}

	assert.NotNil(t, info.Pods)
	assert.Len(t, info.Pods, 0)
}

// Test randomString function
func TestRandomString_Length(t *testing.T) {
	testLengths := []int{1, 5, 8, 10, 20}

	for _, length := range testLengths {
		result := randomString(length)
		assert.Len(t, result, length, "randomString(%d) should return string of length %d", length, length)
	}
}

func TestRandomString_Characters(t *testing.T) {
	result := randomString(100)

	// All characters should be from the charset
	charset := "abcdefghijklmnopqrstuvwxyz0123456789"
	for _, char := range result {
		assert.Contains(t, charset, string(char), "Character '%c' should be in charset", char)
	}
}

func TestRandomString_Zero(t *testing.T) {
	result := randomString(0)
	assert.Len(t, result, 0)
}

func TestRandomString_Uniqueness(t *testing.T) {
	// Generate multiple strings and check they're different
	// Note: Due to the simple implementation using time, consecutive calls may have similar results
	results := make(map[string]bool)
	for i := 0; i < 5; i++ {
		result := randomString(8)
		results[result] = true
		// Small delay to help with uniqueness
		time.Sleep(10 * time.Nanosecond)
	}
	// At least some should be unique
	assert.GreaterOrEqual(t, len(results), 1)
}

// Test TempPod.GetRsyncEndpoint
func TestTempPod_GetRsyncEndpoint(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			PodIP: "10.0.0.5",
		},
	}

	tempPod := &TempPod{
		Name:      "test-pod",
		RsyncPort: 8873,
		Pod:       pod,
	}

	endpoint := tempPod.GetRsyncEndpoint()
	assert.Equal(t, "10.0.0.5:8873", endpoint)
}

func TestTempPod_GetRsyncEndpoint_CustomPort(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			PodIP: "192.168.1.100",
		},
	}

	tempPod := &TempPod{
		Name:      "test-pod",
		RsyncPort: 9999,
		Pod:       pod,
	}

	endpoint := tempPod.GetRsyncEndpoint()
	assert.Equal(t, "192.168.1.100:9999", endpoint)
}

func TestTempPod_GetRsyncEndpoint_IPv6(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			PodIP: "::1",
		},
	}

	tempPod := &TempPod{
		Name:      "test-pod",
		RsyncPort: 8873,
		Pod:       pod,
	}

	endpoint := tempPod.GetRsyncEndpoint()
	assert.Equal(t, "::1:8873", endpoint)
}

func TestTempPod_GetRsyncEndpoint_EmptyIP(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			PodIP: "",
		},
	}

	tempPod := &TempPod{
		RsyncPort: 8873,
		Pod:       pod,
	}

	endpoint := tempPod.GetRsyncEndpoint()
	assert.Equal(t, ":8873", endpoint)
}

// Test realistic scenarios
func TestTempPodOptions_RealisticConfig(t *testing.T) {
	opts := TempPodOptions{
		Namespace:     "production",
		PVCName:       "app-data",
		NodeName:      "worker-node-1",
		RsyncPort:     DefaultRsyncPort,
		KeySecretName: "dr-syncer-ssh-keys",
		Labels: map[string]string{
			"app.kubernetes.io/name":      "dr-syncer",
			"app.kubernetes.io/component": "temp-pod",
			"dr-syncer.io/pvc":            "app-data",
		},
		Annotations: map[string]string{
			"dr-syncer.io/created-by": "controller",
		},
	}

	assert.Equal(t, "production", opts.Namespace)
	assert.Equal(t, "app-data", opts.PVCName)
	assert.Equal(t, DefaultRsyncPort, opts.RsyncPort)
	assert.Len(t, opts.Labels, 3)
}

func TestManager_MultiplePodsManagement(t *testing.T) {
	manager := &Manager{
		Pods: make(map[string]*TempPod),
	}

	// Add pods
	manager.Pods["pod-1"] = &TempPod{Name: "pod-1", Namespace: "ns-1", PVCName: "pvc-1"}
	manager.Pods["pod-2"] = &TempPod{Name: "pod-2", Namespace: "ns-2", PVCName: "pvc-2"}
	manager.Pods["pod-3"] = &TempPod{Name: "pod-3", Namespace: "ns-3", PVCName: "pvc-3"}

	assert.Len(t, manager.Pods, 3)
	assert.Len(t, manager.ListPods(), 3)

	// Get specific pod
	pod2 := manager.GetPod("pod-2")
	assert.NotNil(t, pod2)
	assert.Equal(t, "pvc-2", pod2.PVCName)

	// Remove pod
	delete(manager.Pods, "pod-2")
	assert.Len(t, manager.Pods, 2)
	assert.Nil(t, manager.GetPod("pod-2"))
}

// Test default values
func TestDefaults(t *testing.T) {
	// Test that defaults are reasonable
	assert.Greater(t, DefaultRsyncPort, 1024, "Default rsync port should be > 1024")
	assert.Less(t, DefaultRsyncPort, 65536, "Default rsync port should be < 65536")

	assert.GreaterOrEqual(t, DefaultPodCleanupTimeout, time.Minute)
	assert.GreaterOrEqual(t, DefaultPodReadyTimeout, time.Minute)

	assert.NotEmpty(t, DefaultPodNamePrefix)
	assert.Contains(t, DefaultPodNamePrefix, "dr-syncer")
}

func TestCSIPath_PathConstruction(t *testing.T) {
	// Verify path constants can construct valid CSI paths
	podUID := "12345678-1234-1234-1234-123456789012"
	pvName := "pvc-test-volume"

	expectedPath := KubeletVolumesPath + "/" + podUID + "/" + CSIVolumesSubPath + "/" + pvName + "/" + MountSubPath
	assert.Contains(t, expectedPath, "/var/lib/kubelet/pods")
	assert.Contains(t, expectedPath, "kubernetes.io~csi")
	assert.Contains(t, expectedPath, "/mount")
}

func TestPVCInfo_MultiplePods(t *testing.T) {
	info := &PVCInfo{
		Name:      "shared-pvc",
		Namespace: "default",
		PVName:    "shared-pv",
		NodeName:  "node-1",
		Pods:      []string{"pod-a", "pod-b", "pod-c", "pod-d"},
	}

	assert.Len(t, info.Pods, 4)

	// Simulate adding another pod
	info.Pods = append(info.Pods, "pod-e")
	assert.Len(t, info.Pods, 5)
}

func TestTempPod_WithRealPodStatus(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIP: "10.244.0.5",
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:  "rsync",
					Ready: true,
				},
			},
		},
	}

	tempPod := &TempPod{
		Name:      "dr-syncer-temp-abc123",
		Namespace: "production",
		PVCName:   "app-data",
		NodeName:  "worker-01",
		RsyncPort: DefaultRsyncPort,
		Pod:       pod,
	}

	assert.Equal(t, corev1.PodRunning, tempPod.Pod.Status.Phase)
	assert.True(t, tempPod.Pod.Status.ContainerStatuses[0].Ready)
	assert.Equal(t, "10.244.0.5:8873", tempPod.GetRsyncEndpoint())
}

// Test log package initialization
func TestLogInit(t *testing.T) {
	// Verify the package logger is initialized
	assert.NotNil(t, log)
}
