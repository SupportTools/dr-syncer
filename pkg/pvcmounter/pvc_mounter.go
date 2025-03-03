package pvcmounter

import (
	"context"
	"fmt"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"

	"github.com/sirupsen/logrus"
)

var log = logrus.WithField("component", "pvcmounter")

// MountPodConfig holds configuration for mounting PVCs
type MountPodConfig struct {
	// PauseImage is the image to use for the mount pod (defaults to k8s.gcr.io/pause:3.6)
	PauseImage string
	// MountPath is the path where the PVC will be mounted in the pod (defaults to /data)
	MountPath string
	// PodNamePrefix is the prefix for generated pod names (defaults to pvc-mount)
	PodNamePrefix string
	// Pod labels to apply to the mount pod
	Labels map[string]string
	// Pod annotations to apply to the mount pod
	Annotations map[string]string
	// Resource requests for the mount pod
	Resources *corev1.ResourceRequirements
	// EnableNodeAffinity determines whether to add node affinity to the mount pod
	EnableNodeAffinity bool
	// Tolerations to apply to the mount pod
	Tolerations []corev1.Toleration
	// Timeout for waiting for the pod to be running (defaults to 2 minutes)
	PodRunningTimeout time.Duration
}

// PVCMounter handles mounting of PVCs via pause pods
type PVCMounter struct {
	client kubernetes.Interface
	config MountPodConfig
}

// NewPVCMounter creates a new PVCMounter
func NewPVCMounter(client kubernetes.Interface, config *MountPodConfig) *PVCMounter {
	// Get pause image from environment variable if set
	pauseImage := "k8s.gcr.io/pause:3.6"
	if envImage := getEnvOrDefault("DR_SYNCER_PAUSE_IMAGE", ""); envImage != "" {
		pauseImage = envImage
	}

	cfg := MountPodConfig{
		PauseImage:         pauseImage,
		MountPath:          "/data",
		PodNamePrefix:      "pvc-mount",
		EnableNodeAffinity: true,
		PodRunningTimeout:  2 * time.Minute,
		Labels: map[string]string{
			"app.kubernetes.io/managed-by": "dr-syncer",
			"app.kubernetes.io/name":       "pvc-mount-pod",
			"app.kubernetes.io/part-of":    "dr-syncer",
		},
		Annotations: map[string]string{
			"dr-syncer.io/purpose": "pvc-mount",
		},
		Resources: &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("32Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("50m"),
				corev1.ResourceMemory: resource.MustParse("64Mi"),
			},
		},
	}

	// Apply overrides if provided
	if config != nil {
		if config.PauseImage != "" {
			cfg.PauseImage = config.PauseImage
		}
		if config.MountPath != "" {
			cfg.MountPath = config.MountPath
		}
		if config.PodNamePrefix != "" {
			cfg.PodNamePrefix = config.PodNamePrefix
		}
		if len(config.Labels) > 0 {
			for k, v := range config.Labels {
				cfg.Labels[k] = v
			}
		}
		if len(config.Annotations) > 0 {
			for k, v := range config.Annotations {
				cfg.Annotations[k] = v
			}
		}
		if config.Resources != nil {
			cfg.Resources = config.Resources
		}
		if config.Tolerations != nil {
			cfg.Tolerations = config.Tolerations
		}
		if config.PodRunningTimeout > 0 {
			cfg.PodRunningTimeout = config.PodRunningTimeout
		}
		// Only override if explicitly set to false
		if config.EnableNodeAffinity == false {
			cfg.EnableNodeAffinity = false
		}
	}

	return &PVCMounter{
		client: client,
		config: cfg,
	}
}

// IsPVCMounted checks if a PVC is mounted by any pod
func (m *PVCMounter) IsPVCMounted(ctx context.Context, namespace, pvcName string) (bool, error) {
	// Get all pods in the namespace
	pods, err := m.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to list pods in namespace %s: %w", namespace, err)
	}

	// Check if any pod is mounting the PVC
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodPending {
			for _, volume := range pod.Spec.Volumes {
				if volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName == pvcName {
					return true, nil
				}
			}
		}
	}

	return false, nil
}

// EnsurePVCMounted ensures a PVC is mounted by creating a mount pod if needed
func (m *PVCMounter) EnsurePVCMounted(ctx context.Context, namespace, pvcName string) error {
	// Check if PVC exists
	pvc, err := m.client.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get PVC %s in namespace %s: %w", pvcName, namespace, err)
	}

	// Check if PVC is already mounted
	mounted, err := m.IsPVCMounted(ctx, namespace, pvcName)
	if err != nil {
		return fmt.Errorf("failed to check if PVC %s is mounted: %w", pvcName, err)
	}

	if mounted {
		log.Infof("PVC %s in namespace %s is already mounted", pvcName, namespace)
		return nil
	}

	// If not mounted, create a mount pod
	log.Infof("Creating mount pod for PVC %s in namespace %s", pvcName, namespace)
	return m.createMountPod(ctx, pvc)
}

// Helper function to get environment variable with default value
func getEnvOrDefault(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

// createMountPod creates a pod to mount the PVC
func (m *PVCMounter) createMountPod(ctx context.Context, pvc *corev1.PersistentVolumeClaim) error {
	podName := fmt.Sprintf("%s-%s", m.config.PodNamePrefix, pvc.Name)
	namespace := pvc.Namespace

	// Check if pod already exists
	_, err := m.client.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err == nil {
		// Pod already exists
		log.Infof("Mount pod %s already exists in namespace %s", podName, namespace)
		return nil
	} else if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to check for existing mount pod %s: %w", podName, err)
	}

	// Create the mount pod
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        podName,
			Namespace:   namespace,
			Labels:      m.config.Labels,
			Annotations: m.config.Annotations,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "v1",
					Kind:       "PersistentVolumeClaim",
					Name:       pvc.Name,
					UID:        pvc.UID,
				},
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "pause",
					Image: m.config.PauseImage,
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "data",
							MountPath: m.config.MountPath,
						},
					},
					Resources: *m.config.Resources,
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "data",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvc.Name,
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyAlways,
		},
	}

	// Add node affinity if enabled
	if m.config.EnableNodeAffinity {
		// Find the node where the PVC is bound
		nodeName, err := m.findPVCNode(ctx, pvc)
		if err != nil {
			log.Warnf("Failed to find node for PVC %s: %v, continuing without node affinity", pvc.Name, err)
		} else if nodeName != "" {
			log.Infof("Adding node affinity for PVC %s to node %s", pvc.Name, nodeName)
			pod.Spec.Affinity = &corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "kubernetes.io/hostname",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{nodeName},
									},
								},
							},
						},
					},
				},
			}
		}
	}

	// Add tolerations if configured
	if len(m.config.Tolerations) > 0 {
		pod.Spec.Tolerations = m.config.Tolerations
	}

	// Create the pod
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		_, err := m.client.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
		return err
	})

	if err != nil {
		return fmt.Errorf("failed to create mount pod %s: %w", podName, err)
	}

	log.Infof("Created mount pod %s for PVC %s", podName, pvc.Name)

	// Wait for the pod to be running
	return m.waitForPodRunning(ctx, namespace, podName)
}

// findPVCNode attempts to find the node where a PVC is bound
func (m *PVCMounter) findPVCNode(ctx context.Context, pvc *corev1.PersistentVolumeClaim) (string, error) {
	// If PVC is not bound to a PV, we can't determine the node
	if pvc.Spec.VolumeName == "" {
		log.Infof("PVC %s is not bound to a PV yet", pvc.Name)
		return "", nil
	}

	// Get the PV
	pv, err := m.client.CoreV1().PersistentVolumes().Get(ctx, pvc.Spec.VolumeName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get PV %s: %w", pvc.Spec.VolumeName, err)
	}

	// Check if the PV has a node affinity
	if pv.Spec.NodeAffinity != nil && 
	   pv.Spec.NodeAffinity.Required != nil && 
	   len(pv.Spec.NodeAffinity.Required.NodeSelectorTerms) > 0 {
		// Extract node name from node affinity if possible
		for _, term := range pv.Spec.NodeAffinity.Required.NodeSelectorTerms {
			for _, expr := range term.MatchExpressions {
				if expr.Key == "kubernetes.io/hostname" && expr.Operator == corev1.NodeSelectorOpIn && len(expr.Values) > 0 {
					return expr.Values[0], nil
				}
			}
		}
	}

	// If we can't determine from PV node affinity, check if any pods are using this PVC
	pods, err := m.client.CoreV1().Pods(pvc.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to list pods in namespace %s: %w", pvc.Namespace, err)
	}

	// Look for pods that are using this PVC and are running on a node
	for _, pod := range pods.Items {
		if pod.Spec.NodeName != "" && pod.Status.Phase == corev1.PodRunning {
			for _, volume := range pod.Spec.Volumes {
				if volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName == pvc.Name {
					return pod.Spec.NodeName, nil
				}
			}
		}
	}

	// If we still can't determine the node, return empty string
	log.Infof("Could not determine node for PVC %s", pvc.Name)
	return "", nil
}

// waitForPodRunning waits for the pod to be in running state
func (m *PVCMounter) waitForPodRunning(ctx context.Context, namespace, podName string) error {
	// Create a timeout context
	ctx, cancel := context.WithTimeout(ctx, m.config.PodRunningTimeout)
	defer cancel()

	// Poll until the pod is running or timeout
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	log.Infof("Waiting for pod %s to be running (timeout: %s)...", podName, m.config.PodRunningTimeout)

	for {
		select {
		case <-ctx.Done():
			// Get pod events to help diagnose timeout issues
			events, err := m.getPodEvents(context.Background(), namespace, podName)
			if err != nil {
				log.Warnf("Failed to get events for pod %s: %v", podName, err)
			} else if len(events) > 0 {
				log.Infof("Events for pod %s:", podName)
				for _, event := range events {
					log.Infof("  %s: %s", event.Reason, event.Message)
				}
			}
			return fmt.Errorf("timeout waiting for pod %s to be running", podName)
		case <-ticker.C:
			pod, err := m.client.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					log.Infof("Pod %s not found, continuing to wait...", podName)
					continue
				}
				return fmt.Errorf("failed to get pod %s: %w", podName, err)
			}

			switch pod.Status.Phase {
			case corev1.PodRunning:
				log.Infof("Pod %s is now running", podName)
				return nil
			case corev1.PodFailed, corev1.PodSucceeded:
				// Get pod events to help diagnose failure
				events, err := m.getPodEvents(context.Background(), namespace, podName)
				if err != nil {
					log.Warnf("Failed to get events for pod %s: %v", podName, err)
				} else if len(events) > 0 {
					log.Infof("Events for pod %s:", podName)
					for _, event := range events {
						log.Infof("  %s: %s", event.Reason, event.Message)
					}
				}
				return fmt.Errorf("pod %s is in terminal state: %s", podName, pod.Status.Phase)
			default:
				log.Infof("Pod %s is in state %s, waiting...", podName, pod.Status.Phase)
				// If pod is pending for a while, log the pod status conditions
				if pod.Status.Phase == corev1.PodPending {
					if len(pod.Status.Conditions) > 0 {
						for _, cond := range pod.Status.Conditions {
							if cond.Status == corev1.ConditionFalse {
								log.Infof("Pod %s condition: %s = %s, reason: %s, message: %s", 
									podName, cond.Type, cond.Status, cond.Reason, cond.Message)
							}
						}
					}
				}
			}
		}
	}
}

// getPodEvents gets events for a specific pod
func (m *PVCMounter) getPodEvents(ctx context.Context, namespace, podName string) ([]corev1.Event, error) {
	fieldSelector := fmt.Sprintf("involvedObject.name=%s,involvedObject.namespace=%s,involvedObject.kind=Pod", podName, namespace)
	events, err := m.client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fieldSelector,
	})
	if err != nil {
		return nil, err
	}
	return events.Items, nil
}

// CleanupMountPod removes the mount pod for a PVC
func (m *PVCMounter) CleanupMountPod(ctx context.Context, namespace, pvcName string) error {
	podName := fmt.Sprintf("%s-%s", m.config.PodNamePrefix, pvcName)

	// Delete the pod
	err := m.client.CoreV1().Pods(namespace).Delete(ctx, podName, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Pod doesn't exist, nothing to do
			return nil
		}
		return fmt.Errorf("failed to delete mount pod %s: %w", podName, err)
	}

	log.Infof("Deleted mount pod %s for PVC %s", podName, pvcName)
	return nil
}
