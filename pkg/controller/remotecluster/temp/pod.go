package temp

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/supporttools/dr-syncer/pkg/logging"
)

var log = logging.SetupLogging().WithField("component", "temp-pod-manager")

// PodManager handles temporary pod operations
type PodManager struct {
	client client.Client
	k8s    kubernetes.Interface
}

// NewPodManager creates a new temporary pod manager
func NewPodManager(client client.Client, k8s kubernetes.Interface) *PodManager {
	return &PodManager{
		client: client,
		k8s:    k8s,
	}
}

// PodOptions contains options for creating a temporary pod
type PodOptions struct {
	Namespace string
	PVCName   string
	NodeName  string
	Image     string
	Command   []string
}

// CreateTempPodForPVC creates a temporary pod that mounts the specified PVC
// The pod will be scheduled on the same node where the PVC is already mounted
// If the PVC is not mounted, the pod will be scheduled on any available node
func (p *PodManager) CreateTempPodForPVC(ctx context.Context, opts PodOptions) (*corev1.Pod, error) {
	// Generate a unique name for the pod
	podName := fmt.Sprintf("temp-pvc-%s-%s", opts.PVCName, time.Now().Format("20060102-150405"))

	// Set default image if not provided
	image := opts.Image
	if image == "" {
		image = "busybox:latest"
	}

	// Set default command if not provided
	command := opts.Command
	if len(command) == 0 {
		command = []string{"sleep", "3600"} // Sleep for 1 hour by default
	}

	// Create pod spec
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: opts.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "dr-syncer-temp",
				"app.kubernetes.io/managed-by": "dr-syncer-controller",
				"dr-syncer.io/temp-pod":        "true",
				"dr-syncer.io/pvc":             opts.PVCName,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "main",
					Image:   image,
					Command: command,
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "pvc-data",
							MountPath: "/data",
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "pvc-data",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: opts.PVCName,
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	// Set node affinity if a node name is provided
	if opts.NodeName != "" {
		pod.Spec.NodeName = opts.NodeName
	}

	// Create the pod
	createdPod, err := p.k8s.CoreV1().Pods(opts.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary pod: %w", err)
	}

	return createdPod, nil
}

// WaitForPodReady waits for the pod to be ready
func (p *PodManager) WaitForPodReady(ctx context.Context, namespace, name string, timeout time.Duration) error {
	if timeout == 0 {
		timeout = 2 * time.Minute // Default timeout
	}

	// Define backoff parameters
	backoff := wait.Backoff{
		Duration: 1 * time.Second,            // Initial duration
		Factor:   1.5,                        // Factor to increase duration each retry
		Jitter:   0.1,                        // Jitter factor
		Steps:    int(timeout.Seconds() / 5), // Maximum number of retries
		Cap:      30 * time.Second,           // Maximum duration between retries
	}

	// Use exponential backoff to wait for the pod to be ready
	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		// Get the pod
		pod := &corev1.Pod{}
		err := p.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, pod)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return false, nil // Pod not found yet, retry
			}
			return false, err // Other error, stop retrying
		}

		// Check if the pod is ready
		if pod.Status.Phase == corev1.PodRunning {
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
					return true, nil // Pod is ready
				}
			}
		}

		// Check for pod failure
		if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
			return false, fmt.Errorf("pod %s/%s is in phase %s", namespace, name, pod.Status.Phase)
		}

		return false, nil // Pod not ready yet, retry
	})

	if err != nil {
		return fmt.Errorf("timed out waiting for pod %s/%s to be ready: %w", namespace, name, err)
	}

	return nil
}

// DeletePod deletes the temporary pod
func (p *PodManager) DeletePod(ctx context.Context, namespace, name string) error {
	// Delete the pod
	err := p.k8s.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete temporary pod: %w", err)
	}

	return nil
}

// FindPVCNode finds the node where a PVC is mounted
func (p *PodManager) FindPVCNode(ctx context.Context, namespace, pvcName string) (string, error) {
	// Get the PVC
	pvc := &corev1.PersistentVolumeClaim{}
	if err := p.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: pvcName}, pvc); err != nil {
		return "", fmt.Errorf("failed to get PVC: %w", err)
	}

	// If PVC doesn't have a volume name, it's not bound
	if pvc.Spec.VolumeName == "" {
		return "", fmt.Errorf("PVC is not bound to a PV")
	}

	// Get the PV
	pv := &corev1.PersistentVolume{}
	if err := p.client.Get(ctx, types.NamespacedName{Name: pvc.Spec.VolumeName}, pv); err != nil {
		return "", fmt.Errorf("failed to get PV: %w", err)
	}

	// Check if the PV has a node affinity
	if pv.Spec.NodeAffinity != nil && pv.Spec.NodeAffinity.Required != nil {
		for _, term := range pv.Spec.NodeAffinity.Required.NodeSelectorTerms {
			for _, expr := range term.MatchExpressions {
				if expr.Key == "kubernetes.io/hostname" && len(expr.Values) > 0 {
					return expr.Values[0], nil
				}
			}
		}
	}

	// If no node affinity, find pods using this PVC
	var podList corev1.PodList
	if err := p.client.List(ctx, &podList, client.InNamespace(namespace)); err != nil {
		return "", fmt.Errorf("failed to list pods: %w", err)
	}

	for _, pod := range podList.Items {
		for _, volume := range pod.Spec.Volumes {
			if volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName == pvcName {
				if pod.Spec.NodeName != "" {
					return pod.Spec.NodeName, nil
				}
			}
		}
	}

	return "", fmt.Errorf("could not find node for PVC %s/%s", namespace, pvcName)
}

// CleanupTempPods deletes all temporary pods created by dr-syncer
func (p *PodManager) CleanupTempPods(ctx context.Context, namespace string) error {
	// List all pods with the dr-syncer.io/temp-pod label
	pods, err := p.k8s.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "dr-syncer.io/temp-pod=true",
	})
	if err != nil {
		return fmt.Errorf("failed to list temporary pods: %w", err)
	}

	// Delete each pod
	for _, pod := range pods.Items {
		if err := p.DeletePod(ctx, pod.Namespace, pod.Name); err != nil {
			return fmt.Errorf("failed to delete temporary pod %s/%s: %w", pod.Namespace, pod.Name, err)
		}
	}

	return nil
}
