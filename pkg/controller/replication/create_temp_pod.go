package replication

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// CreateTempPodForPVC creates a temporary pod to mount a PVC
func (p *PVCSyncer) CreateTempPodForPVC(ctx context.Context, namespace, pvcName, nodeName string) (*corev1.Pod, error) {
	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc_name":  pvcName,
		"node_name": nodeName,
	}).Info("Creating temporary pod to mount PVC")

	// Determine which Kubernetes client to use based on the namespace
	var k8sClient kubernetes.Interface
	if namespace == p.SourceNamespace {
		k8sClient = p.SourceK8sClient
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
		}).Debug("Using source Kubernetes client")
	} else {
		k8sClient = p.DestinationK8sClient
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
		}).Debug("Using destination Kubernetes client")
	}

	// Generate a unique name for the pod
	podName := fmt.Sprintf("temp-pvc-mount-%s-%s", pvcName, time.Now().Format("20060102-150405"))

	// Create the pod
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
			Labels: map[string]string{
				"app":                      "dr-syncer-temp-pod",
				"dr-syncer.io/temp-pod":    "true",
				"dr-syncer.io/mounted-pvc": pvcName,
			},
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
			Containers: []corev1.Container{
				{
					Name:  "pvc-mounter",
					Image: "busybox:latest",
					Command: []string{
						"sh",
						"-c",
						"echo 'Mounting PVC' && sleep 3600",
					},
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
							ClaimName: pvcName,
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	// Create the pod in the cluster
	createdPod, err := k8sClient.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
			"node_name": nodeName,
			"error":     err,
		}).Error("Failed to create temporary pod")
		return nil, fmt.Errorf("failed to create temporary pod: %v", err)
	}

	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc_name":  pvcName,
		"pod_name":  createdPod.Name,
		"node_name": nodeName,
	}).Info("Created temporary pod to mount PVC")

	// Wait for the pod to be ready
	err = p.WaitForPodReady(ctx, namespace, createdPod.Name, 2*time.Minute)
	if err != nil {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
			"pod_name":  createdPod.Name,
			"node_name": nodeName,
			"error":     err,
		}).Error("Failed to wait for temporary pod to be ready")

		// Try to clean up the pod
		_ = k8sClient.CoreV1().Pods(namespace).Delete(ctx, createdPod.Name, metav1.DeleteOptions{})

		return nil, fmt.Errorf("failed to wait for temporary pod to be ready: %v", err)
	}

	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc_name":  pvcName,
		"pod_name":  createdPod.Name,
		"node_name": nodeName,
	}).Info("Temporary pod is ready")

	return createdPod, nil
}

// WaitForPodReady waits for a pod to be ready with a timeout
func (p *PVCSyncer) WaitForPodReady(ctx context.Context, namespace, podName string, timeout time.Duration) error {
	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pod_name":  podName,
		"timeout":   timeout,
	}).Info("Waiting for pod to be ready")

	// Create a context with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Determine which Kubernetes client to use based on the namespace
	var k8sClient kubernetes.Interface
	if namespace == p.SourceNamespace {
		k8sClient = p.SourceK8sClient
	} else {
		k8sClient = p.DestinationK8sClient
	}

	// Poll until the pod is ready or timeout
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("timeout waiting for pod %s/%s to be ready", namespace, podName)
		case <-ticker.C:
			// Get the pod
			pod, err := k8sClient.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
			if err != nil {
				log.WithFields(logrus.Fields{
					"namespace": namespace,
					"pod_name":  podName,
					"error":     err,
				}).Warn("Failed to get pod while waiting for ready state")
				continue
			}

			// Check if pod is running
			if pod.Status.Phase == corev1.PodRunning {
				// Check if all containers are ready
				allContainersReady := true
				for _, containerStatus := range pod.Status.ContainerStatuses {
					if !containerStatus.Ready {
						allContainersReady = false
						break
					}
				}

				if allContainersReady {
					log.WithFields(logrus.Fields{
						"namespace": namespace,
						"pod_name":  podName,
					}).Info("Pod is now ready")
					return nil
				}
			}

			log.WithFields(logrus.Fields{
				"namespace": namespace,
				"pod_name":  podName,
				"phase":     pod.Status.Phase,
			}).Debug("Pod not yet ready, waiting...")
		}
	}
}

// CleanupTempPod deletes a temporary pod
func (p *PVCSyncer) CleanupTempPod(ctx context.Context, namespace, podName string) error {
	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pod_name":  podName,
	}).Info("Cleaning up temporary pod")

	// Determine which Kubernetes client to use based on the namespace
	var k8sClient kubernetes.Interface
	if namespace == p.SourceNamespace {
		k8sClient = p.SourceK8sClient
	} else {
		k8sClient = p.DestinationK8sClient
	}

	// Delete the pod
	err := k8sClient.CoreV1().Pods(namespace).Delete(ctx, podName, metav1.DeleteOptions{})
	if err != nil {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pod_name":  podName,
			"error":     err,
		}).Error("Failed to delete temporary pod")
		return fmt.Errorf("failed to delete temporary pod: %v", err)
	}

	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pod_name":  podName,
	}).Info("Successfully deleted temporary pod")

	return nil
}
