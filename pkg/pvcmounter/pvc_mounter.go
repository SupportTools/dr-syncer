package pvcmounter

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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
}

// PVCMounter handles mounting of PVCs via pause pods
type PVCMounter struct {
	client kubernetes.Interface
	config MountPodConfig
}

// NewPVCMounter creates a new PVCMounter
func NewPVCMounter(client kubernetes.Interface, config *MountPodConfig) *PVCMounter {
	cfg := MountPodConfig{
		PauseImage:    "k8s.gcr.io/pause:3.6",
		MountPath:     "/data",
		PodNamePrefix: "pvc-mount",
		Labels: map[string]string{
			"app.kubernetes.io/managed-by": "dr-syncer",
			"app.kubernetes.io/name":       "pvc-mount-pod",
			"app.kubernetes.io/part-of":    "dr-syncer",
		},
		Annotations: map[string]string{
			"dr-syncer.io/purpose": "pvc-mount",
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

// createMountPod creates a pod to mount the PVC
func (m *PVCMounter) createMountPod(ctx context.Context, pvc *corev1.PersistentVolumeClaim) error {
	podName := fmt.Sprintf("%s-%s", m.config.PodNamePrefix, pvc.Name)
	namespace := pvc.Namespace

	// Check if pod already exists
	_, err := m.client.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err == nil {
		// Pod already exists
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
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    *pvc.Spec.Resources.Requests.Cpu(),
							corev1.ResourceMemory: *pvc.Spec.Resources.Requests.Memory(),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    *pvc.Spec.Resources.Limits.Cpu(),
							corev1.ResourceMemory: *pvc.Spec.Resources.Limits.Memory(),
						},
					},
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

// waitForPodRunning waits for the pod to be in running state
func (m *PVCMounter) waitForPodRunning(ctx context.Context, namespace, podName string) error {
	// Create a timeout context
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	// Poll until the pod is running or timeout
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
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
				return fmt.Errorf("pod %s is in terminal state: %s", podName, pod.Status.Phase)
			default:
				log.Infof("Pod %s is in state %s, waiting...", podName, pod.Status.Phase)
			}
		}
	}
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
