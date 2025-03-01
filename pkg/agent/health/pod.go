package health

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
)

// PodHealthChecker handles pod health checking
type PodHealthChecker struct {
	client client.Client
}

// NewPodHealthChecker creates a new pod health checker
func NewPodHealthChecker(client client.Client) *PodHealthChecker {
	return &PodHealthChecker{
		client: client,
	}
}

// GetPodStatus gets the status of a pod
func (h *PodHealthChecker) GetPodStatus(pod *corev1.Pod) *drv1alpha1.PodStatus {
	status := &drv1alpha1.PodStatus{
		Phase: string(pod.Status.Phase),
		Ready: isPodReady(pod),
	}

	// Get restart count
	if len(pod.Status.ContainerStatuses) > 0 {
		for _, containerStatus := range pod.Status.ContainerStatuses {
			status.RestartCount += containerStatus.RestartCount
		}
	}

	// Get last transition time
	if len(pod.Status.Conditions) > 0 {
		latestTime := metav1.Time{}
		for _, condition := range pod.Status.Conditions {
			if condition.LastTransitionTime.After(latestTime.Time) {
				latestTime = condition.LastTransitionTime
			}
		}
		status.LastTransitionTime = &latestTime
	}

	return status
}

// GetAgentPods gets all agent pods for a remote cluster
func (h *PodHealthChecker) GetAgentPods(ctx context.Context, rc *drv1alpha1.RemoteCluster) ([]corev1.Pod, error) {
	// List pods in the dr-syncer namespace
	var podList corev1.PodList
	err := h.client.List(ctx, &podList, client.InNamespace("dr-syncer"),
		client.MatchingLabels(map[string]string{
			"app.kubernetes.io/name":       "dr-syncer-agent",
			"app.kubernetes.io/managed-by": "dr-syncer-controller",
			"dr-syncer.io/remote-cluster":  rc.Name,
		}))

	if err != nil {
		return nil, fmt.Errorf("failed to list agent pods: %w", err)
	}

	return podList.Items, nil
}

// isPodReady checks if a pod is ready
func isPodReady(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}

	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}

	return false
}
