package health

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
)

const (
	// DefaultHealthCheckInterval is the default interval between health checks
	DefaultHealthCheckInterval = 5 * time.Minute

	// DefaultSSHTimeout is the default timeout for SSH connection attempts
	DefaultSSHTimeout = 10 * time.Second

	// DefaultRetryAttempts is the default number of retry attempts
	DefaultRetryAttempts = 3

	// DefaultRetryInterval is the default interval between retry attempts
	DefaultRetryInterval = 30 * time.Second
)

// HealthChecker handles health checking for PVC sync agents
type HealthChecker struct {
	client     client.Client
	sshChecker *SSHHealthChecker
	podChecker *PodHealthChecker
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(client client.Client) *HealthChecker {
	return &HealthChecker{
		client:     client,
		sshChecker: NewSSHHealthChecker(client),
		podChecker: NewPodHealthChecker(client),
	}
}

// getHealthCheckConfig gets the health check configuration from the RemoteCluster
func getHealthCheckConfig(rc *drv1alpha1.RemoteCluster) (interval, sshTimeout time.Duration, retryAttempts int32, retryInterval time.Duration) {
	// Set defaults
	interval = DefaultHealthCheckInterval
	sshTimeout = DefaultSSHTimeout
	retryAttempts = DefaultRetryAttempts
	retryInterval = DefaultRetryInterval

	// Return defaults if no config
	if rc.Spec.PVCSync == nil || rc.Spec.PVCSync.HealthCheck == nil {
		return
	}

	// Parse interval
	if rc.Spec.PVCSync.HealthCheck.Interval != "" {
		if parsed, err := time.ParseDuration(rc.Spec.PVCSync.HealthCheck.Interval); err == nil {
			interval = parsed
		} else {
			log.Warnf("Invalid health check interval %q, using default: %v",
				rc.Spec.PVCSync.HealthCheck.Interval, err)
		}
	}

	// Parse SSH timeout
	if rc.Spec.PVCSync.HealthCheck.SSHTimeout != "" {
		if parsed, err := time.ParseDuration(rc.Spec.PVCSync.HealthCheck.SSHTimeout); err == nil {
			sshTimeout = parsed
		} else {
			log.Warnf("Invalid SSH timeout %q, using default: %v",
				rc.Spec.PVCSync.HealthCheck.SSHTimeout, err)
		}
	}

	// Get retry attempts
	if rc.Spec.PVCSync.HealthCheck.RetryAttempts > 0 {
		retryAttempts = rc.Spec.PVCSync.HealthCheck.RetryAttempts
	}

	// Parse retry interval
	if rc.Spec.PVCSync.HealthCheck.RetryInterval != "" {
		if parsed, err := time.ParseDuration(rc.Spec.PVCSync.HealthCheck.RetryInterval); err == nil {
			retryInterval = parsed
		} else {
			log.Warnf("Invalid retry interval %q, using default: %v",
				rc.Spec.PVCSync.HealthCheck.RetryInterval, err)
		}
	}

	return
}

// CheckAgentHealth checks the health of all agent pods for a remote cluster
func (h *HealthChecker) CheckAgentHealth(ctx context.Context, rc *drv1alpha1.RemoteCluster) error {
	// Initialize agent status if needed
	if rc.Status.PVCSync == nil {
		rc.Status.PVCSync = &drv1alpha1.PVCSyncStatus{
			Phase: "Initializing",
		}
	}

	if rc.Status.PVCSync.AgentStatus == nil {
		rc.Status.PVCSync.AgentStatus = &drv1alpha1.PVCSyncAgentStatus{
			NodeStatuses: make(map[string]drv1alpha1.PVCSyncNodeStatus),
		}
	}

	// Skip health check if PVC sync is not enabled
	if rc.Spec.PVCSync == nil || !rc.Spec.PVCSync.Enabled {
		return nil
	}

	// Get health check configuration
	interval, sshTimeout, retryAttempts, retryInterval := getHealthCheckConfig(rc)

	// Get agent pods
	pods, err := h.podChecker.GetAgentPods(ctx, rc)
	if err != nil {
		log.Errorf("Failed to get agent pods for cluster %s: %v", rc.Name, err)
		return err
	}

	// Track nodes with pods
	nodesWithPods := make(map[string]bool)

	// Check each pod
	for _, pod := range pods {
		nodeName := pod.Spec.NodeName
		if nodeName == "" {
			continue
		}

		nodesWithPods[nodeName] = true

		// Get pod status
		podStatus := h.podChecker.GetPodStatus(&pod)

		// Get pod IP
		podIP := pod.Status.PodIP
		if podIP == "" {
			log.Warnf("Pod %s/%s has no IP address", pod.Namespace, pod.Name)
			continue
		}

		// Initialize node status if needed
		nodeStatus, exists := rc.Status.PVCSync.AgentStatus.NodeStatuses[nodeName]
		if !exists {
			nodeStatus = drv1alpha1.PVCSyncNodeStatus{
				Ready:         false,
				LastHeartbeat: &metav1.Time{Time: time.Now()},
			}
		}

		// Update pod status
		nodeStatus.PodStatus = podStatus

		// Check SSH connectivity if pod is ready
		if podStatus.Ready {
			// Check if we need to perform SSH health check
			needsSSHCheck := true
			if nodeStatus.SSHStatus != nil && nodeStatus.SSHStatus.LastCheckTime != nil {
				timeSinceLastCheck := time.Since(nodeStatus.SSHStatus.LastCheckTime.Time)
				if timeSinceLastCheck < interval {
					needsSSHCheck = false
				}
			}

			if needsSSHCheck {
				log.Infof("Checking SSH connectivity to node %s for cluster %s", nodeName, rc.Name)

				// Perform SSH health check with retries
				var sshStatus *drv1alpha1.SSHConnectionStatus
				var lastErr error

				for attempt := int32(0); attempt <= retryAttempts; attempt++ {
					if attempt > 0 {
						log.Infof("Retrying SSH health check for node %s (attempt %d/%d)",
							nodeName, attempt, retryAttempts)
						time.Sleep(retryInterval)
					}

					// Set SSH timeout
					ctx, cancel := context.WithTimeout(ctx, sshTimeout)
					defer cancel()

					sshStatus, lastErr = h.sshChecker.CheckNodeSSHHealth(ctx, rc, nodeName, podIP)
					if lastErr == nil && sshStatus.Connected {
						break
					}
				}

				if lastErr != nil {
					log.Warnf("SSH health check failed for node %s after %d attempts: %v",
						nodeName, retryAttempts+1, lastErr)
				}

				nodeStatus.SSHStatus = sshStatus

				// If basic SSH check passed, also check SSH proxy connectivity
				if sshStatus != nil && sshStatus.Connected {
					log.Infof("Checking SSH proxy connectivity to node %s for cluster %s", nodeName, rc.Name)

					// Perform SSH proxy health check with retries
					var sshProxyStatus *drv1alpha1.SSHConnectionStatus
					var proxyLastErr error

					for attempt := int32(0); attempt <= retryAttempts; attempt++ {
						if attempt > 0 {
							log.Infof("Retrying SSH proxy health check for node %s (attempt %d/%d)",
								nodeName, attempt, retryAttempts)
							time.Sleep(retryInterval)
						}

						// Set SSH timeout
						proxyCtx, proxyCancel := context.WithTimeout(ctx, sshTimeout)
						defer proxyCancel()

						sshProxyStatus, proxyLastErr = h.sshChecker.CheckNodeSSHProxyHealth(proxyCtx, rc, nodeName, podIP)
						if proxyLastErr == nil && sshProxyStatus.Connected {
							break
						}
					}

					if proxyLastErr != nil {
						log.Warnf("SSH proxy health check failed for node %s after %d attempts: %v",
							nodeName, retryAttempts+1, proxyLastErr)

						// Update SSH status to reflect proxy failure
						nodeStatus.SSHStatus.Connected = false
						nodeStatus.SSHStatus.Error = fmt.Sprintf("SSH proxy check failed: %v", proxyLastErr)
					}
				}
			}
		}

		// Update node status
		nodeStatus.Ready = podStatus.Ready && (nodeStatus.SSHStatus != nil && nodeStatus.SSHStatus.Connected)
		nodeStatus.LastHeartbeat = &metav1.Time{Time: time.Now()}
		nodeStatus.Message = fmt.Sprintf("Pod: %s, SSH: %t", podStatus.Phase,
			nodeStatus.SSHStatus != nil && nodeStatus.SSHStatus.Connected)

		// Update node status in map
		rc.Status.PVCSync.AgentStatus.NodeStatuses[nodeName] = nodeStatus
	}

	// Remove nodes that no longer have pods
	for nodeName := range rc.Status.PVCSync.AgentStatus.NodeStatuses {
		if !nodesWithPods[nodeName] {
			delete(rc.Status.PVCSync.AgentStatus.NodeStatuses, nodeName)
		}
	}

	// Update aggregated status
	readyNodes := 0
	totalNodes := len(rc.Status.PVCSync.AgentStatus.NodeStatuses)

	for _, status := range rc.Status.PVCSync.AgentStatus.NodeStatuses {
		if status.Ready {
			readyNodes++
		}
	}

	rc.Status.PVCSync.AgentStatus.ReadyNodes = int32(readyNodes)
	rc.Status.PVCSync.AgentStatus.TotalNodes = int32(totalNodes)

	// Update overall status
	if readyNodes == 0 && totalNodes > 0 {
		rc.Status.PVCSync.Phase = "Degraded"
		rc.Status.PVCSync.Message = "No agent nodes are ready"
	} else if readyNodes < totalNodes {
		rc.Status.PVCSync.Phase = "Degraded"
		rc.Status.PVCSync.Message = fmt.Sprintf("%d/%d agent nodes are ready", readyNodes, totalNodes)
	} else if readyNodes > 0 {
		rc.Status.PVCSync.Phase = "Running"
		rc.Status.PVCSync.Message = "All agent nodes are ready"
	}

	return nil
}
