package health

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Test constants from checker.go
func TestChecker_Constants(t *testing.T) {
	assert.Equal(t, 5*time.Minute, DefaultHealthCheckInterval)
	assert.Equal(t, 10*time.Second, DefaultSSHTimeout)
	assert.Equal(t, 3, DefaultRetryAttempts)
	assert.Equal(t, 30*time.Second, DefaultRetryInterval)
}

// Test constants from ssh.go
func TestSSH_Constants(t *testing.T) {
	assert.Equal(t, "echo dr-syncer-ssh-test", TestCommand)
	assert.Equal(t, "test-connection", ProxyTestCommand)
}

// Test HealthChecker struct
func TestHealthChecker_Struct(t *testing.T) {
	hc := &HealthChecker{}

	assert.Nil(t, hc.client)
	assert.Nil(t, hc.sshChecker)
	assert.Nil(t, hc.podChecker)
}

// Test SSHHealthChecker struct
func TestSSHHealthChecker_Struct(t *testing.T) {
	ssh := &SSHHealthChecker{}

	assert.Nil(t, ssh.client)
}

func TestNewSSHHealthChecker(t *testing.T) {
	ssh := NewSSHHealthChecker(nil)

	assert.NotNil(t, ssh)
	assert.Nil(t, ssh.client)
}

// Test PodHealthChecker struct
func TestPodHealthChecker_Struct(t *testing.T) {
	ph := &PodHealthChecker{}

	assert.Nil(t, ph.client)
}

func TestNewPodHealthChecker(t *testing.T) {
	ph := NewPodHealthChecker(nil)

	assert.NotNil(t, ph)
	assert.Nil(t, ph.client)
}

// Test isPodReady function
func TestIsPodReady_Running_Ready(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	assert.True(t, isPodReady(pod))
}

func TestIsPodReady_Running_NotReady(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionFalse,
				},
			},
		},
	}

	assert.False(t, isPodReady(pod))
}

func TestIsPodReady_Pending(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
		},
	}

	assert.False(t, isPodReady(pod))
}

func TestIsPodReady_Failed(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodFailed,
		},
	}

	assert.False(t, isPodReady(pod))
}

func TestIsPodReady_Succeeded(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodSucceeded,
		},
	}

	assert.False(t, isPodReady(pod))
}

func TestIsPodReady_NoConditions(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase:      corev1.PodRunning,
			Conditions: []corev1.PodCondition{},
		},
	}

	assert.False(t, isPodReady(pod))
}

func TestIsPodReady_WrongConditionType(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodScheduled,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	assert.False(t, isPodReady(pod))
}

func TestIsPodReady_MultipleConditions(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodScheduled,
					Status: corev1.ConditionTrue,
				},
				{
					Type:   corev1.ContainersReady,
					Status: corev1.ConditionTrue,
				},
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	assert.True(t, isPodReady(pod))
}

// Test GetPodStatus method
func TestGetPodStatus_Running(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	ph := &PodHealthChecker{}
	status := ph.GetPodStatus(pod)

	assert.NotNil(t, status)
	assert.Equal(t, "Running", status.Phase)
	assert.True(t, status.Ready)
	assert.Equal(t, int32(0), status.RestartCount)
}

func TestGetPodStatus_Pending(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
		},
	}

	ph := &PodHealthChecker{}
	status := ph.GetPodStatus(pod)

	assert.Equal(t, "Pending", status.Phase)
	assert.False(t, status.Ready)
}

func TestGetPodStatus_WithRestartCount(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "container-1", RestartCount: 3},
				{Name: "container-2", RestartCount: 2},
			},
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	ph := &PodHealthChecker{}
	status := ph.GetPodStatus(pod)

	assert.Equal(t, int32(5), status.RestartCount)
}

func TestGetPodStatus_WithTransitionTime(t *testing.T) {
	now := metav1.Now()
	earlier := metav1.NewTime(now.Add(-1 * time.Hour))

	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{
					Type:               corev1.PodScheduled,
					Status:             corev1.ConditionTrue,
					LastTransitionTime: earlier,
				},
				{
					Type:               corev1.PodReady,
					Status:             corev1.ConditionTrue,
					LastTransitionTime: now,
				},
			},
		},
	}

	ph := &PodHealthChecker{}
	status := ph.GetPodStatus(pod)

	assert.NotNil(t, status.LastTransitionTime)
	assert.Equal(t, now.Time, status.LastTransitionTime.Time)
}

func TestGetPodStatus_NoContainers(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase:             corev1.PodPending,
			ContainerStatuses: []corev1.ContainerStatus{},
		},
	}

	ph := &PodHealthChecker{}
	status := ph.GetPodStatus(pod)

	assert.Equal(t, int32(0), status.RestartCount)
}

func TestGetPodStatus_Failed(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodFailed,
		},
	}

	ph := &PodHealthChecker{}
	status := ph.GetPodStatus(pod)

	assert.Equal(t, "Failed", status.Phase)
	assert.False(t, status.Ready)
}

// Test getHealthCheckConfig function
func TestGetHealthCheckConfig_Defaults(t *testing.T) {
	rc := &drv1alpha1.RemoteCluster{
		Spec: drv1alpha1.RemoteClusterSpec{},
	}

	interval, sshTimeout, retryAttempts, retryInterval := getHealthCheckConfig(rc)

	assert.Equal(t, DefaultHealthCheckInterval, interval)
	assert.Equal(t, DefaultSSHTimeout, sshTimeout)
	assert.Equal(t, int32(DefaultRetryAttempts), retryAttempts)
	assert.Equal(t, DefaultRetryInterval, retryInterval)
}

func TestGetHealthCheckConfig_NilPVCSync(t *testing.T) {
	rc := &drv1alpha1.RemoteCluster{
		Spec: drv1alpha1.RemoteClusterSpec{
			PVCSync: nil,
		},
	}

	interval, sshTimeout, retryAttempts, retryInterval := getHealthCheckConfig(rc)

	assert.Equal(t, DefaultHealthCheckInterval, interval)
	assert.Equal(t, DefaultSSHTimeout, sshTimeout)
	assert.Equal(t, int32(DefaultRetryAttempts), retryAttempts)
	assert.Equal(t, DefaultRetryInterval, retryInterval)
}

func TestGetHealthCheckConfig_NilHealthCheck(t *testing.T) {
	rc := &drv1alpha1.RemoteCluster{
		Spec: drv1alpha1.RemoteClusterSpec{
			PVCSync: &drv1alpha1.PVCSyncSpec{
				HealthCheck: nil,
			},
		},
	}

	interval, sshTimeout, retryAttempts, retryInterval := getHealthCheckConfig(rc)

	assert.Equal(t, DefaultHealthCheckInterval, interval)
	assert.Equal(t, DefaultSSHTimeout, sshTimeout)
	assert.Equal(t, int32(DefaultRetryAttempts), retryAttempts)
	assert.Equal(t, DefaultRetryInterval, retryInterval)
}

func TestGetHealthCheckConfig_CustomInterval(t *testing.T) {
	rc := &drv1alpha1.RemoteCluster{
		Spec: drv1alpha1.RemoteClusterSpec{
			PVCSync: &drv1alpha1.PVCSyncSpec{
				HealthCheck: &drv1alpha1.HealthCheckConfig{
					Interval: "10m",
				},
			},
		},
	}

	interval, _, _, _ := getHealthCheckConfig(rc)

	assert.Equal(t, 10*time.Minute, interval)
}

func TestGetHealthCheckConfig_CustomSSHTimeout(t *testing.T) {
	rc := &drv1alpha1.RemoteCluster{
		Spec: drv1alpha1.RemoteClusterSpec{
			PVCSync: &drv1alpha1.PVCSyncSpec{
				HealthCheck: &drv1alpha1.HealthCheckConfig{
					SSHTimeout: "30s",
				},
			},
		},
	}

	_, sshTimeout, _, _ := getHealthCheckConfig(rc)

	assert.Equal(t, 30*time.Second, sshTimeout)
}

func TestGetHealthCheckConfig_CustomRetryAttempts(t *testing.T) {
	rc := &drv1alpha1.RemoteCluster{
		Spec: drv1alpha1.RemoteClusterSpec{
			PVCSync: &drv1alpha1.PVCSyncSpec{
				HealthCheck: &drv1alpha1.HealthCheckConfig{
					RetryAttempts: 5,
				},
			},
		},
	}

	_, _, retryAttempts, _ := getHealthCheckConfig(rc)

	assert.Equal(t, int32(5), retryAttempts)
}

func TestGetHealthCheckConfig_CustomRetryInterval(t *testing.T) {
	rc := &drv1alpha1.RemoteCluster{
		Spec: drv1alpha1.RemoteClusterSpec{
			PVCSync: &drv1alpha1.PVCSyncSpec{
				HealthCheck: &drv1alpha1.HealthCheckConfig{
					RetryInterval: "1m",
				},
			},
		},
	}

	_, _, _, retryInterval := getHealthCheckConfig(rc)

	assert.Equal(t, 1*time.Minute, retryInterval)
}

func TestGetHealthCheckConfig_AllCustom(t *testing.T) {
	rc := &drv1alpha1.RemoteCluster{
		Spec: drv1alpha1.RemoteClusterSpec{
			PVCSync: &drv1alpha1.PVCSyncSpec{
				HealthCheck: &drv1alpha1.HealthCheckConfig{
					Interval:      "15m",
					SSHTimeout:    "20s",
					RetryAttempts: 10,
					RetryInterval: "2m",
				},
			},
		},
	}

	interval, sshTimeout, retryAttempts, retryInterval := getHealthCheckConfig(rc)

	assert.Equal(t, 15*time.Minute, interval)
	assert.Equal(t, 20*time.Second, sshTimeout)
	assert.Equal(t, int32(10), retryAttempts)
	assert.Equal(t, 2*time.Minute, retryInterval)
}

func TestGetHealthCheckConfig_InvalidInterval(t *testing.T) {
	rc := &drv1alpha1.RemoteCluster{
		Spec: drv1alpha1.RemoteClusterSpec{
			PVCSync: &drv1alpha1.PVCSyncSpec{
				HealthCheck: &drv1alpha1.HealthCheckConfig{
					Interval: "invalid",
				},
			},
		},
	}

	interval, _, _, _ := getHealthCheckConfig(rc)

	// Should use default when invalid
	assert.Equal(t, DefaultHealthCheckInterval, interval)
}

func TestGetHealthCheckConfig_InvalidSSHTimeout(t *testing.T) {
	rc := &drv1alpha1.RemoteCluster{
		Spec: drv1alpha1.RemoteClusterSpec{
			PVCSync: &drv1alpha1.PVCSyncSpec{
				HealthCheck: &drv1alpha1.HealthCheckConfig{
					SSHTimeout: "not-a-duration",
				},
			},
		},
	}

	_, sshTimeout, _, _ := getHealthCheckConfig(rc)

	// Should use default when invalid
	assert.Equal(t, DefaultSSHTimeout, sshTimeout)
}

func TestGetHealthCheckConfig_InvalidRetryInterval(t *testing.T) {
	rc := &drv1alpha1.RemoteCluster{
		Spec: drv1alpha1.RemoteClusterSpec{
			PVCSync: &drv1alpha1.PVCSyncSpec{
				HealthCheck: &drv1alpha1.HealthCheckConfig{
					RetryInterval: "bad",
				},
			},
		},
	}

	_, _, _, retryInterval := getHealthCheckConfig(rc)

	// Should use default when invalid
	assert.Equal(t, DefaultRetryInterval, retryInterval)
}

func TestGetHealthCheckConfig_ZeroRetryAttempts(t *testing.T) {
	rc := &drv1alpha1.RemoteCluster{
		Spec: drv1alpha1.RemoteClusterSpec{
			PVCSync: &drv1alpha1.PVCSyncSpec{
				HealthCheck: &drv1alpha1.HealthCheckConfig{
					RetryAttempts: 0,
				},
			},
		},
	}

	_, _, retryAttempts, _ := getHealthCheckConfig(rc)

	// Should use default when zero
	assert.Equal(t, int32(DefaultRetryAttempts), retryAttempts)
}

func TestGetHealthCheckConfig_NegativeRetryAttempts(t *testing.T) {
	rc := &drv1alpha1.RemoteCluster{
		Spec: drv1alpha1.RemoteClusterSpec{
			PVCSync: &drv1alpha1.PVCSyncSpec{
				HealthCheck: &drv1alpha1.HealthCheckConfig{
					RetryAttempts: -1,
				},
			},
		},
	}

	_, _, retryAttempts, _ := getHealthCheckConfig(rc)

	// Should use default when negative
	assert.Equal(t, int32(DefaultRetryAttempts), retryAttempts)
}

// Test realistic scenarios
func TestIsPodReady_RealisticAgentPod(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dr-syncer-agent-abc123",
			Namespace: "dr-syncer",
			Labels: map[string]string{
				"app.kubernetes.io/name":       "dr-syncer-agent",
				"app.kubernetes.io/managed-by": "dr-syncer-controller",
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIP: "10.244.0.5",
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodScheduled,
					Status: corev1.ConditionTrue,
				},
				{
					Type:   corev1.PodInitialized,
					Status: corev1.ConditionTrue,
				},
				{
					Type:   corev1.ContainersReady,
					Status: corev1.ConditionTrue,
				},
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "agent",
					Ready:        true,
					RestartCount: 0,
				},
				{
					Name:         "ssh-server",
					Ready:        true,
					RestartCount: 0,
				},
			},
		},
	}

	assert.True(t, isPodReady(pod))

	ph := &PodHealthChecker{}
	status := ph.GetPodStatus(pod)
	assert.True(t, status.Ready)
	assert.Equal(t, int32(0), status.RestartCount)
}

func TestGetPodStatus_CrashLoopBackOff(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionFalse,
				},
			},
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "agent",
					Ready:        false,
					RestartCount: 10,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason:  "CrashLoopBackOff",
							Message: "Back-off 5m0s restarting failed container",
						},
					},
				},
			},
		},
	}

	ph := &PodHealthChecker{}
	status := ph.GetPodStatus(pod)

	assert.False(t, status.Ready)
	assert.Equal(t, int32(10), status.RestartCount)
}

// Test default values are reasonable
func TestDefaults_Reasonable(t *testing.T) {
	// Health check interval should be at least 1 minute
	assert.GreaterOrEqual(t, DefaultHealthCheckInterval, time.Minute)

	// SSH timeout should be reasonable (5-60 seconds)
	assert.GreaterOrEqual(t, DefaultSSHTimeout, 5*time.Second)
	assert.LessOrEqual(t, DefaultSSHTimeout, time.Minute)

	// Retry attempts should be at least 1
	assert.GreaterOrEqual(t, DefaultRetryAttempts, 1)

	// Retry interval should be at least 5 seconds
	assert.GreaterOrEqual(t, DefaultRetryInterval, 5*time.Second)
}

func TestTestCommand_Format(t *testing.T) {
	// Test command should be an echo command
	assert.Contains(t, TestCommand, "echo")
	assert.Contains(t, TestCommand, "dr-syncer")
}

func TestProxyTestCommand_Format(t *testing.T) {
	// Proxy test command should be meaningful
	assert.NotEmpty(t, ProxyTestCommand)
}

// Test NewHealthChecker
func TestNewHealthChecker(t *testing.T) {
	hc := NewHealthChecker(nil)

	assert.NotNil(t, hc)
	assert.NotNil(t, hc.sshChecker)
	assert.NotNil(t, hc.podChecker)
}

// Test log package initialization
func TestLogInit(t *testing.T) {
	assert.NotNil(t, log)
}
