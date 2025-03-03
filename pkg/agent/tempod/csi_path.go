package tempod

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// KubeletVolumesPath is the base path where kubelet mounts volumes
	KubeletVolumesPath = "/var/lib/kubelet/pods"

	// CSIVolumesSubPath is the subpath for CSI volumes
	CSIVolumesSubPath = "volumes/kubernetes.io~csi"

	// MountSubPath is the subpath for the actual mount point
	MountSubPath = "mount"
)

// CSIVolumeInfo contains information about a CSI volume
type CSIVolumeInfo struct {
	// PodUID is the UID of the pod using the volume
	PodUID string

	// VolumeName is the name of the volume in the pod
	VolumeName string

	// PVCName is the name of the PVC
	PVCName string

	// PVName is the name of the PV
	PVName string

	// NodeName is the name of the node where the volume is mounted
	NodeName string

	// MountPath is the full path to the mounted volume
	MountPath string
}

// FindCSIPath finds the CSI path for a PVC on a node
func FindCSIPath(ctx context.Context, client kubernetes.Interface, namespace, pvcName, nodeName string) (string, error) {
	log.WithFields(map[string]interface{}{
		"pvc":       pvcName,
		"namespace": namespace,
		"node":      nodeName,
	}).Info("Finding CSI path for PVC")

	// Get PVC
	pvc, err := client.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get PVC: %v", err)
	}

	// Get PV name
	pvName := pvc.Spec.VolumeName
	if pvName == "" {
		return "", fmt.Errorf("PVC is not bound to a PV")
	}

	// Get PV
	pv, err := client.CoreV1().PersistentVolumes().Get(ctx, pvName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get PV: %v", err)
	}

	// Check if PV is a CSI volume
	if pv.Spec.CSI == nil {
		return "", fmt.Errorf("PV is not a CSI volume")
	}

	// Find pods using the PVC on the specified node
	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
	})
	if err != nil {
		return "", fmt.Errorf("failed to list pods: %v", err)
	}

	// Check each pod to see if it's using the PVC
	for _, pod := range pods.Items {
		// Skip pods that are not running
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}

		// Check if pod is using the PVC
		for _, volume := range pod.Spec.Volumes {
			if volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName == pvcName {
				// Construct the CSI path
				csiPath := filepath.Join(
					KubeletVolumesPath,
					string(pod.UID),
					CSIVolumesSubPath,
					pvName,
					MountSubPath,
				)

				log.WithFields(map[string]interface{}{
					"pvc":       pvcName,
					"namespace": namespace,
					"pod":       pod.Name,
					"node":      nodeName,
					"csi_path":  csiPath,
				}).Info("Found CSI path for PVC")

				// Verify the path exists
				if _, err := os.Stat(csiPath); err != nil {
					log.WithFields(map[string]interface{}{
						"csi_path": csiPath,
						"error":    err,
					}).Warn("CSI path does not exist")
					continue
				}

				return csiPath, nil
			}
		}
	}

	// If no pod is using the PVC, we need to find the CSI path directly
	return FindCSIPathWithoutPod(ctx, client, namespace, pvcName, nodeName)
}

// FindCSIPathWithoutPod finds the CSI path for a PVC without a pod
func FindCSIPathWithoutPod(ctx context.Context, client kubernetes.Interface, namespace, pvcName, nodeName string) (string, error) {
	log.WithFields(map[string]interface{}{
		"pvc":       pvcName,
		"namespace": namespace,
		"node":      nodeName,
	}).Info("Finding CSI path for PVC without pod")

	// Get PVC
	pvc, err := client.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get PVC: %v", err)
	}

	// Get PV name
	pvName := pvc.Spec.VolumeName
	if pvName == "" {
		return "", fmt.Errorf("PVC is not bound to a PV")
	}

	// Get PV
	pv, err := client.CoreV1().PersistentVolumes().Get(ctx, pvName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get PV: %v", err)
	}

	// Check if PV is a CSI volume
	if pv.Spec.CSI == nil {
		return "", fmt.Errorf("PV is not a CSI volume")
	}

	// Search for the CSI path in the kubelet volumes directory
	return SearchCSIPath(pvName)
}

// SearchCSIPath searches for a CSI path by PV name
func SearchCSIPath(pvName string) (string, error) {
	log.WithFields(map[string]interface{}{
		"pv_name": pvName,
	}).Info("Searching for CSI path")

	// Check if the kubelet volumes path exists
	if _, err := os.Stat(KubeletVolumesPath); err != nil {
		return "", fmt.Errorf("kubelet volumes path does not exist: %v", err)
	}

	// List pod directories
	podDirs, err := os.ReadDir(KubeletVolumesPath)
	if err != nil {
		return "", fmt.Errorf("failed to read kubelet volumes path: %v", err)
	}

	// Search each pod directory for the CSI volume
	for _, podDir := range podDirs {
		if !podDir.IsDir() {
			continue
		}

		// Construct the CSI volumes path
		csiVolumesPath := filepath.Join(KubeletVolumesPath, podDir.Name(), CSIVolumesSubPath)

		// Check if the CSI volumes path exists
		if _, err := os.Stat(csiVolumesPath); err != nil {
			continue
		}

		// List CSI volume directories
		volumeDirs, err := os.ReadDir(csiVolumesPath)
		if err != nil {
			continue
		}

		// Search for the PV directory
		for _, volumeDir := range volumeDirs {
			if !volumeDir.IsDir() {
				continue
			}

			// Check if the directory name contains the PV name
			if strings.Contains(volumeDir.Name(), pvName) {
				// Construct the mount path
				mountPath := filepath.Join(csiVolumesPath, volumeDir.Name(), MountSubPath)

				// Check if the mount path exists
				if _, err := os.Stat(mountPath); err != nil {
					continue
				}

				log.WithFields(map[string]interface{}{
					"pv_name":    pvName,
					"mount_path": mountPath,
				}).Info("Found CSI path")

				return mountPath, nil
			}
		}
	}

	return "", fmt.Errorf("CSI path not found for PV %s", pvName)
}

// CreatePlaceholderPod creates a placeholder pod to mount a PVC
func CreatePlaceholderPod(ctx context.Context, client kubernetes.Interface, namespace, pvcName, nodeName string) (*corev1.Pod, error) {
	log.WithFields(map[string]interface{}{
		"pvc":       pvcName,
		"namespace": namespace,
		"node":      nodeName,
	}).Info("Creating placeholder pod for PVC")

	// Generate a unique name for the placeholder pod
	timestamp := time.Now().Format("20060102-150405")
	podName := fmt.Sprintf("pvc-placeholder-%s-%s", pvcName, timestamp)

	// Create the pod
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "dr-syncer-placeholder",
				"app.kubernetes.io/component": "pvc-placeholder",
				"app.kubernetes.io/part-of":   "dr-syncer",
				"pvc-name":                    pvcName,
			},
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
			Containers: []corev1.Container{
				{
					Name:  "placeholder",
					Image: "busybox:latest",
					Command: []string{
						"sh",
						"-c",
						"while true; do sleep 3600; done",
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "pvc-volume",
							MountPath: "/mnt/pvc",
						},
					},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    *resource.NewMilliQuantity(100, resource.DecimalSI),
							corev1.ResourceMemory: *resource.NewQuantity(64*1024*1024, resource.BinarySI),
						},
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    *resource.NewMilliQuantity(10, resource.DecimalSI),
							corev1.ResourceMemory: *resource.NewQuantity(32*1024*1024, resource.BinarySI),
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "pvc-volume",
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

	// Create the pod
	createdPod, err := client.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create placeholder pod: %v", err)
	}

	log.WithFields(map[string]interface{}{
		"pod":       createdPod.Name,
		"namespace": createdPod.Namespace,
		"pvc":       pvcName,
		"node":      nodeName,
	}).Info("Created placeholder pod")

	return createdPod, nil
}

// WaitForPlaceholderPod waits for a placeholder pod to be running
func WaitForPlaceholderPod(ctx context.Context, client kubernetes.Interface, namespace, podName string, timeout time.Duration) error {
	log.WithFields(map[string]interface{}{
		"pod":       podName,
		"namespace": namespace,
		"timeout":   timeout,
	}).Info("Waiting for placeholder pod to be running")

	// Create a context with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Poll until the pod is running or timeout
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("timeout waiting for placeholder pod %s/%s to be running", namespace, podName)
		case <-ticker.C:
			// Get the pod
			pod, err := client.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
			if err != nil {
				log.WithFields(map[string]interface{}{
					"pod":       podName,
					"namespace": namespace,
					"error":     err,
				}).Warn("Failed to get placeholder pod")
				continue
			}

			// Check if pod is running
			if pod.Status.Phase == corev1.PodRunning {
				log.WithFields(map[string]interface{}{
					"pod":       podName,
					"namespace": namespace,
				}).Info("Placeholder pod is running")
				return nil
			}

			log.WithFields(map[string]interface{}{
				"pod":       podName,
				"namespace": namespace,
				"phase":     pod.Status.Phase,
			}).Debug("Placeholder pod not yet running")
		}
	}
}

// DeletePlaceholderPod deletes a placeholder pod
func DeletePlaceholderPod(ctx context.Context, client kubernetes.Interface, namespace, podName string) error {
	log.WithFields(map[string]interface{}{
		"pod":       podName,
		"namespace": namespace,
	}).Info("Deleting placeholder pod")

	// Delete the pod
	err := client.CoreV1().Pods(namespace).Delete(ctx, podName, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete placeholder pod: %v", err)
	}

	log.WithFields(map[string]interface{}{
		"pod":       podName,
		"namespace": namespace,
	}).Info("Deleted placeholder pod")

	return nil
}
