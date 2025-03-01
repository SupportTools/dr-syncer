package tempod

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// PVCInfo contains information about a PVC
type PVCInfo struct {
	// Name is the name of the PVC
	Name string

	// Namespace is the namespace of the PVC
	Namespace string

	// PVName is the name of the PV
	PVName string

	// NodeName is the name of the node where the PVC is mounted
	NodeName string

	// Pods is a list of pods using the PVC
	Pods []string
}

// FindPVCNode finds the node where a PVC is mounted
func FindPVCNode(ctx context.Context, client kubernetes.Interface, namespace, pvcName string) (string, error) {
	// Get PVC
	pvc, err := client.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get PVC: %v", err)
	}

	// Get PV
	pvName := pvc.Spec.VolumeName
	if pvName == "" {
		return "", fmt.Errorf("PVC is not bound to a PV")
	}

	// Find pods using the PVC
	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to list pods: %v", err)
	}

	// Check each pod to see if it's using the PVC
	for _, pod := range pods.Items {
		// Skip pods that are not running
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}

		// Skip pods without a node
		if pod.Spec.NodeName == "" {
			continue
		}

		// Check if pod is using the PVC
		for _, volume := range pod.Spec.Volumes {
			if volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName == pvcName {
				log.WithFields(map[string]interface{}{
					"pvc":       pvcName,
					"namespace": namespace,
					"pod":       pod.Name,
					"node":      pod.Spec.NodeName,
				}).Info("Found pod using PVC")
				return pod.Spec.NodeName, nil
			}
		}
	}

	// If no pod is using the PVC, we need to find a node that can mount it
	// For now, we'll just return an error
	return "", fmt.Errorf("no running pod is using the PVC")
}

// GetPVCInfo gets information about a PVC
func GetPVCInfo(ctx context.Context, client kubernetes.Interface, namespace, pvcName string) (*PVCInfo, error) {
	// Get PVC
	pvc, err := client.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get PVC: %v", err)
	}

	// Get PV name
	pvName := pvc.Spec.VolumeName
	if pvName == "" {
		return nil, fmt.Errorf("PVC is not bound to a PV")
	}

	// Find pods using the PVC
	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %v", err)
	}

	// Create PVC info
	info := &PVCInfo{
		Name:      pvcName,
		Namespace: namespace,
		PVName:    pvName,
		Pods:      make([]string, 0),
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
				info.Pods = append(info.Pods, pod.Name)

				// Set node name if not already set
				if info.NodeName == "" && pod.Spec.NodeName != "" {
					info.NodeName = pod.Spec.NodeName
				}
			}
		}
	}

	// If no node name is set, we need to find a node that can mount it
	if info.NodeName == "" {
		return nil, fmt.Errorf("no running pod is using the PVC")
	}

	return info, nil
}

// ListPVCs lists all PVCs in a namespace
func ListPVCs(ctx context.Context, client kubernetes.Interface, namespace string) ([]string, error) {
	// List PVCs
	pvcs, err := client.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list PVCs: %v", err)
	}

	// Extract PVC names
	names := make([]string, 0, len(pvcs.Items))
	for _, pvc := range pvcs.Items {
		names = append(names, pvc.Name)
	}

	return names, nil
}
