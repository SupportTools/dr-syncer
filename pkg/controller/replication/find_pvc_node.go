package replication

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FindPVCNode finds the node where a PVC is mounted
func (p *PVCSyncer) FindPVCNode(ctx context.Context, client client.Client, namespace, pvcName string) (string, error) {
	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc_name":  pvcName,
	}).Info("Finding node where PVC is mounted")

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

	// List pods in the namespace
	podList, err := k8sClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"error":     err,
		}).Error("Failed to list pods")
		return "", fmt.Errorf("failed to list pods: %v", err)
	}

	// Find pods using this PVC
	for _, pod := range podList.Items {
		// Skip pods that are not running
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}

		// Check if this pod uses the PVC
		for _, volume := range pod.Spec.Volumes {
			if volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName == pvcName {
				log.WithFields(logrus.Fields{
					"namespace": namespace,
					"pvc_name":  pvcName,
					"pod_name":  pod.Name,
					"node_name": pod.Spec.NodeName,
				}).Info("Found pod using PVC")
				return pod.Spec.NodeName, nil
			}
		}
	}

	// If no running pod is found, try to find a node with the PVC attached
	volumeAttachments, err := k8sClient.StorageV1().VolumeAttachments().List(ctx, metav1.ListOptions{})
	if err != nil {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
			"error":     err,
		}).Error("Failed to list volume attachments")
		return "", fmt.Errorf("failed to list volume attachments: %v", err)
	}

	// Get the PVC to find its volume name
	pvc, err := k8sClient.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
			"error":     err,
		}).Error("Failed to get PVC")
		return "", fmt.Errorf("failed to get PVC: %v", err)
	}

	// Check if any volume attachment references this PVC
	for _, va := range volumeAttachments.Items {
		if va.Spec.Source.PersistentVolumeName != nil && *va.Spec.Source.PersistentVolumeName == pvc.Spec.VolumeName {
			log.WithFields(logrus.Fields{
				"namespace":     namespace,
				"pvc_name":      pvcName,
				"pv_name":       pvc.Spec.VolumeName,
				"attachment":    va.Name,
				"attached_node": va.Spec.NodeName,
			}).Info("Found volume attachment for PVC")
			return va.Spec.NodeName, nil
		}
	}

	// If no node is found, try to find any available node in the cluster
	nodeList, err := k8sClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
			"error":     err,
		}).Error("Failed to list nodes")
		return "", fmt.Errorf("failed to list nodes: %v", err)
	}

	// Find a node that is ready
	var readyNode string
	for _, node := range nodeList.Items {
		// Check if the node is ready
		for _, condition := range node.Status.Conditions {
			if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
				readyNode = node.Name
				log.WithFields(logrus.Fields{
					"namespace": namespace,
					"pvc_name":  pvcName,
					"node_name": node.Name,
				}).Info("Found a ready node")
				break
			}
		}
		if readyNode != "" {
			break
		}
	}

	// If a ready node is found, create a temporary pod to mount the PVC
	if readyNode != "" {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
			"node_name": readyNode,
		}).Info("Creating temporary pod to mount PVC")

		tempPod, err := p.CreateTempPodForPVC(ctx, namespace, pvcName, readyNode)
		if err != nil {
			log.WithFields(logrus.Fields{
				"namespace": namespace,
				"pvc_name":  pvcName,
				"node_name": readyNode,
				"error":     err,
			}).Error("Failed to create temporary pod")
			// Continue with the ready node even if we couldn't create a temp pod
			return readyNode, nil
		}

		// Clean up the pod when we're done with it
		defer func() {
			if err := p.CleanupTempPod(ctx, namespace, tempPod.Name); err != nil {
				log.WithFields(logrus.Fields{
					"namespace": namespace,
					"pod_name":  tempPod.Name,
					"error":     err,
				}).Error("Failed to clean up temporary pod")
			}
		}()

		return readyNode, nil
	}

	// If still no node is found, return an error
	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc_name":  pvcName,
	}).Error("No node found for PVC and no ready nodes available")
	return "", fmt.Errorf("no node found for PVC %s/%s and no ready nodes available", namespace, pvcName)
}
