package replication

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

// RsyncController manages the rsync deployment process
type RsyncController struct {
	// PVCSyncer is the parent PVC syncer
	syncer *PVCSyncer
}

// NewRsyncController creates a new rsync controller
func NewRsyncController(syncer *PVCSyncer) *RsyncController {
	return &RsyncController{
		syncer: syncer,
	}
}

// SyncReplication orchestrates the PVC replication process between source and destination clusters
func (r *RsyncController) SyncReplication(ctx context.Context, sourceNS, destNS, pvcName string, syncID string) error {
	log.WithFields(logrus.Fields{
		"source_namespace": sourceNS,
		"dest_namespace":   destNS,
		"pvc_name":         pvcName,
		"sync_id":          syncID,
	}).Info("[DR-SYNC] Starting enhanced PVC replication process")

	// Step 1: Check if source PVC is currently mounted
	log.Info("[DR-SYNC] Step 1: Checking if source PVC is mounted")
	hasAttachments, err := r.syncer.HasVolumeAttachments(ctx, sourceNS, pvcName)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("[DR-SYNC-ERROR] Failed to check if source PVC has attachments")
		return fmt.Errorf("failed to check if source PVC has attachments: %v", err)
	}

	if !hasAttachments {
		log.Info("[DR-SYNC] Source PVC is not mounted, skipping rsync")
		return nil
	}

	// Step 2: Find the nodes where the PVC is mounted
	log.Info("[DR-SYNC] Step 2: Finding nodes where source PVC is mounted")
	nodes, err := r.findPVCNodes(ctx, sourceNS, pvcName)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("[DR-SYNC-ERROR] Failed to find nodes where source PVC is mounted")
		return fmt.Errorf("failed to find nodes where source PVC is mounted: %v", err)
	}

	// Step 3: Select a node (randomly if multiple)
	log.WithFields(logrus.Fields{
		"node_count": len(nodes),
	}).Info("[DR-SYNC] Step 3: Selecting a node from available options")
	
	if len(nodes) == 0 {
		log.Error("[DR-SYNC-ERROR] No nodes found with mounted PVC")
		return fmt.Errorf("no nodes found with mounted PVC")
	}

	// Randomly select a node if multiple are available
	selectedNode := r.selectRandomNode(nodes)
	log.WithFields(logrus.Fields{
		"selected_node": selectedNode,
	}).Info("[DR-SYNC] Selected node for replication")

	// Step 4: Find the DR-Syncer-Agent running on that node
	log.Info("[DR-SYNC] Step 4: Finding DR-Syncer-Agent on selected node")
	agentPod, agentIP, err := r.syncer.FindAgentPod(ctx, selectedNode)
	if err != nil {
		log.WithFields(logrus.Fields{
			"node":  selectedNode,
			"error": err,
		}).Error("[DR-SYNC-ERROR] Failed to find DR-Syncer-Agent on node")
		return fmt.Errorf("failed to find DR-Syncer-Agent on node: %v", err)
	}

	// Step 5: Find the mount path for the PVC
	log.Info("[DR-SYNC] Step 5: Finding mount path for source PVC")
	mountPath, err := r.syncer.FindPVCMountPath(ctx, sourceNS, pvcName, agentPod)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("[DR-SYNC-ERROR] Failed to find mount path for source PVC")
		return fmt.Errorf("failed to find mount path for source PVC: %v", err)
	}

	// Step 6: Deploy an rsync deployment on the destination cluster and namespace
	log.Info("[DR-SYNC] Step 6: Deploying rsync deployment on destination cluster")
	deployment, err := r.deployRsyncDeployment(ctx, destNS, pvcName, syncID)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("[DR-SYNC-ERROR] Failed to deploy rsync deployment")
		return fmt.Errorf("failed to deploy rsync deployment: %v", err)
	}

	// Ensure we clean up the rsync deployment at the end
	defer func() {
		log.Info("[DR-SYNC] Cleaning up rsync deployment")
		cleanupErr := r.cleanupRsyncDeployment(ctx, deployment)
		if cleanupErr != nil {
			log.WithFields(logrus.Fields{
				"error": cleanupErr,
			}).Error("[DR-SYNC-ERROR] Failed to cleanup rsync deployment")
		}
	}()

	// Step 7: Wait for the rsync pod to be ready
	log.Info("[DR-SYNC] Step 7: Waiting for rsync pod to become ready")
	rsyncPod, err := r.waitForRsyncPodReady(ctx, deployment, 5*time.Minute)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("[DR-SYNC-ERROR] Rsync pod failed to become ready")
		return fmt.Errorf("rsync pod failed to become ready: %v", err)
	}

	// Step 8: Generate SSH keys in the rsync pod
	log.Info("[DR-SYNC] Step 8: Generating SSH keys in rsync pod")
	err = r.generateSSHKeysInPod(ctx, deployment.Namespace, rsyncPod)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("[DR-SYNC-ERROR] Failed to generate SSH keys in rsync pod")
		return fmt.Errorf("failed to generate SSH keys in rsync pod: %v", err)
	}

	// Step 9: Get the public SSH key from the rsync pod
	log.Info("[DR-SYNC] Step 9: Getting public SSH key from rsync pod")
	publicKey, err := r.getPublicKeyFromPod(ctx, deployment.Namespace, rsyncPod)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("[DR-SYNC-ERROR] Failed to get public SSH key from rsync pod")
		return fmt.Errorf("failed to get public SSH key from rsync pod: %v", err)
	}

	// Step 10: Push the public SSH key to the agent pod
	log.Info("[DR-SYNC] Step 10: Pushing public SSH key to agent pod")
	trackingInfo := fmt.Sprintf("dr-syncer-%s", syncID)
	err = r.syncer.PushPublicKeyToAgent(ctx, agentPod, publicKey, trackingInfo)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("[DR-SYNC-ERROR] Failed to push public SSH key to agent pod")
		return fmt.Errorf("failed to push public SSH key to agent pod: %v", err)
	}

	// Step 11: Test SSH connectivity from rsync pod to agent pod
	log.Info("[DR-SYNC] Step 11: Testing SSH connectivity")
	err = r.testSSHConnectivity(ctx, deployment.Namespace, rsyncPod, agentIP, 2222)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("[DR-SYNC-ERROR] SSH connectivity test failed")
		return fmt.Errorf("SSH connectivity test failed: %v", err)
	}

	// Step 12: Run the rsync command and monitor status
	log.Info("[DR-SYNC] Step 12: Running rsync command")
	err = r.performRsync(ctx, deployment.Namespace, rsyncPod, agentIP, mountPath)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("[DR-SYNC-ERROR] Rsync command failed")
		return fmt.Errorf("rsync command failed: %v", err)
	}

	// Step 13: Update source PVC annotations
	log.Info("[DR-SYNC] Step 13: Updating source PVC annotations")
	// Store current values to be restored after sync
	r.syncer.SourceNamespace = sourceNS
	r.syncer.DestinationNamespace = destNS
	err = r.syncer.UpdateSourcePVCAnnotations(ctx, sourceNS, pvcName)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("[DR-SYNC-ERROR] Failed to update source PVC annotations")
		return fmt.Errorf("failed to update source PVC annotations: %v", err)
	}

	log.WithFields(logrus.Fields{
		"source_namespace": sourceNS,
		"dest_namespace":   destNS,
		"pvc_name":         pvcName,
		"sync_id":          syncID,
	}).Info("[DR-SYNC] PVC replication process completed successfully")

	return nil
}

// findPVCNodes finds all nodes where a PVC is mounted
func (r *RsyncController) findPVCNodes(ctx context.Context, namespace, pvcName string) ([]string, error) {
	var nodes []string
	
	// Get the PVC
	pvc, err := r.syncer.SourceK8sClient.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get PVC: %v", err)
	}
	
	// Get volume attachments for this PVC
	volumeAttachments, err := r.syncer.SourceK8sClient.StorageV1().VolumeAttachments().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list volume attachments: %v", err)
	}
	
	// Find nodes where this PVC's PV is attached
	for _, va := range volumeAttachments.Items {
		if va.Spec.Source.PersistentVolumeName != nil && *va.Spec.Source.PersistentVolumeName == pvc.Spec.VolumeName {
			nodes = append(nodes, va.Spec.NodeName)
			log.WithFields(logrus.Fields{
				"node":    va.Spec.NodeName,
				"pv_name": *va.Spec.Source.PersistentVolumeName,
			}).Info("[DR-SYNC-DETAIL] Found node with attached PV")
		}
	}
	
	// If no volume attachments, look for pods using this PVC
	if len(nodes) == 0 {
		podList, err := r.syncer.SourceK8sClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to list pods: %v", err)
		}
		
		for _, pod := range podList.Items {
			for _, volume := range pod.Spec.Volumes {
				if volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName == pvcName {
					if pod.Spec.NodeName != "" && pod.Status.Phase == corev1.PodRunning {
						nodes = append(nodes, pod.Spec.NodeName)
						log.WithFields(logrus.Fields{
							"node":     pod.Spec.NodeName,
							"pod_name": pod.Name,
						}).Info("[DR-SYNC-DETAIL] Found node with pod using PVC")
					}
				}
			}
		}
	}
	
	// Deduplicate nodes (in case multiple pods or attachments on the same node)
	uniqueNodes := make(map[string]bool)
	var result []string
	for _, node := range nodes {
		if !uniqueNodes[node] {
			uniqueNodes[node] = true
			result = append(result, node)
		}
	}
	
	log.WithFields(logrus.Fields{
		"nodes_found": len(result),
		"nodes":       result,
	}).Info("[DR-SYNC-DETAIL] Found nodes with PVC mounted")
	
	return result, nil
}

// selectRandomNode randomly selects a node from the provided list
func (r *RsyncController) selectRandomNode(nodes []string) string {
	if len(nodes) == 1 {
		return nodes[0]
	}
	
	rand.Seed(time.Now().UnixNano())
	randomIndex := rand.Intn(len(nodes))
	return nodes[randomIndex]
}

// deployRsyncDeployment creates an rsync deployment in the destination cluster
func (r *RsyncController) deployRsyncDeployment(ctx context.Context, namespace, pvcName, syncID string) (*appsv1.Deployment, error) {
	// Generate a deployment name
	deploymentName := fmt.Sprintf("dr-syncer-rsync-%s-%s", pvcName, syncID)
	
	log.WithFields(logrus.Fields{
		"deployment": deploymentName,
		"namespace":  namespace,
		"pvc_name":   pvcName,
	}).Info("[DR-SYNC-DETAIL] Creating rsync deployment")
	
	// Define the deployment
	replicas := int32(1)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "dr-syncer-rsync",
				"app.kubernetes.io/managed-by": "dr-syncer-controller",
				"dr-syncer.io/sync-id":         syncID,
				"dr-syncer.io/pvc-name":        pvcName,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":    "dr-syncer-rsync",
					"dr-syncer.io/sync-id":      syncID,
					"dr-syncer.io/pvc-name":     pvcName,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/name":       "dr-syncer-rsync",
						"app.kubernetes.io/managed-by": "dr-syncer-controller",
						"dr-syncer.io/sync-id":         syncID,
						"dr-syncer.io/pvc-name":        pvcName,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "rsync",
							Image: "supporttools/dr-syncer-rsync:latest",
							Command: []string{
								"/bin/sh",
								"-c",
								"sleep infinity", // Start in a waiting state
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "data",
									MountPath: "/data",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "data",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: pvcName,
								},
							},
						},
					},
				},
			},
		},
	}
	
	// Check if a deployment with this name already exists and delete it
	existingDeployment, err := r.syncer.DestinationK8sClient.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err == nil {
		// Deployment exists, delete it
		log.WithFields(logrus.Fields{
			"deployment": existingDeployment.Name,
			"namespace":  existingDeployment.Namespace,
		}).Info("[DR-SYNC-DETAIL] Found existing deployment, deleting it")
		
		deletePolicy := metav1.DeletePropagationForeground
		deleteOptions := metav1.DeleteOptions{
			PropagationPolicy: &deletePolicy,
		}
		
		if err := r.syncer.DestinationK8sClient.AppsV1().Deployments(namespace).Delete(ctx, deploymentName, deleteOptions); err != nil {
			if !errors.IsNotFound(err) {
				return nil, fmt.Errorf("failed to delete existing deployment: %v", err)
			}
		}
		
		// Wait for deletion to complete
		if err := r.waitForDeploymentDeletion(ctx, namespace, deploymentName); err != nil {
			return nil, fmt.Errorf("timeout waiting for deployment deletion: %v", err)
		}
	} else if !errors.IsNotFound(err) {
		// Some error other than "not found"
		return nil, fmt.Errorf("failed to check for existing deployment: %v", err)
	}
	
	// Create the deployment
	createdDeployment, err := r.syncer.DestinationK8sClient.AppsV1().Deployments(namespace).Create(ctx, deployment, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create rsync deployment: %v", err)
	}
	
	log.WithFields(logrus.Fields{
		"deployment": createdDeployment.Name,
		"namespace":  createdDeployment.Namespace,
	}).Info("[DR-SYNC-DETAIL] Successfully created rsync deployment")
	
	return createdDeployment, nil
}

// waitForRsyncPodReady waits for a pod from the deployment to be ready
func (r *RsyncController) waitForRsyncPodReady(ctx context.Context, deployment *appsv1.Deployment, timeout time.Duration) (string, error) {
	log.WithFields(logrus.Fields{
		"deployment": deployment.Name,
		"namespace":  deployment.Namespace,
		"timeout":    timeout,
	}).Info("[DR-SYNC-DETAIL] Waiting for rsync pod to be ready")
	
	// Create a context with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	
	var podName string
	
	// Poll until the pod is ready or timeout
	err := wait.PollUntilContextCancel(timeoutCtx, 5*time.Second, true, func(ctx context.Context) (bool, error) {
		// Get the latest deployment status
		dep, err := r.syncer.DestinationK8sClient.AppsV1().Deployments(deployment.Namespace).Get(ctx, deployment.Name, metav1.GetOptions{})
		if err != nil {
			log.WithFields(logrus.Fields{
				"deployment": deployment.Name,
				"namespace":  deployment.Namespace,
				"error":      err,
			}).Warn("[DR-SYNC-DETAIL] Failed to get deployment while waiting")
			return false, nil
		}
		
		// Check if deployment is available
		if dep.Status.AvailableReplicas == 0 {
			log.WithFields(logrus.Fields{
				"deployment":          deployment.Name,
				"namespace":           deployment.Namespace,
				"available_replicas":  dep.Status.AvailableReplicas,
				"updated_replicas":    dep.Status.UpdatedReplicas,
				"ready_replicas":      dep.Status.ReadyReplicas,
				"desired_replicas":    dep.Status.Replicas,
			}).Debug("[DR-SYNC-DETAIL] Deployment not ready yet")
			return false, nil
		}
		
		// Get pods managed by this deployment
		selector := metav1.FormatLabelSelector(dep.Spec.Selector)
		pods, err := r.syncer.DestinationK8sClient.CoreV1().Pods(deployment.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: selector,
		})
		if err != nil {
			log.WithFields(logrus.Fields{
				"deployment": deployment.Name,
				"namespace":  deployment.Namespace,
				"error":      err,
			}).Warn("[DR-SYNC-DETAIL] Failed to list pods for deployment")
			return false, nil
		}
		
		// Find a running pod
		for _, pod := range pods.Items {
			if pod.Status.Phase == corev1.PodRunning {
				podName = pod.Name
				log.WithFields(logrus.Fields{
					"deployment": deployment.Name,
					"namespace":  deployment.Namespace,
					"pod":        podName,
				}).Info("[DR-SYNC-DETAIL] Found running pod for deployment")
				return true, nil
			}
		}
		
		return false, nil
	})
	
	if err != nil {
		return "", fmt.Errorf("failed to wait for pod to be ready: %v", err)
	}
	
	if podName == "" {
		return "", fmt.Errorf("no running pod found for deployment %s/%s", deployment.Namespace, deployment.Name)
	}
	
	return podName, nil
}

// generateSSHKeysInPod generates SSH keys in the rsync pod
func (r *RsyncController) generateSSHKeysInPod(ctx context.Context, namespace, podName string) error {
	log.WithFields(logrus.Fields{
		"pod":       podName,
		"namespace": namespace,
	}).Info("[DR-SYNC-DETAIL] Generating SSH keys in rsync pod")
	
	cmd := []string{
		"sh",
		"-c",
		"mkdir -p /root/.ssh && ssh-keygen -t rsa -N '' -f /root/.ssh/id_rsa",
	}
	
	stdout, stderr, err := executeCommandInPod(ctx, r.syncer.DestinationK8sClient, namespace, podName, cmd)
	if err != nil {
		log.WithFields(logrus.Fields{
			"stderr": stderr,
			"error":  err,
		}).Error("[DR-SYNC-ERROR] Failed to generate SSH keys")
		return fmt.Errorf("failed to generate SSH keys: %v", err)
	}
	
	log.WithFields(logrus.Fields{
		"stdout": stdout,
	}).Debug("[DR-SYNC-DETAIL] Successfully generated SSH keys")
	
	return nil
}

// getPublicKeyFromPod gets the public key from the rsync pod
func (r *RsyncController) getPublicKeyFromPod(ctx context.Context, namespace, podName string) (string, error) {
	log.WithFields(logrus.Fields{
		"pod":       podName,
		"namespace": namespace,
	}).Info("[DR-SYNC-DETAIL] Getting public key from rsync pod")
	
	cmd := []string{
		"cat",
		"/root/.ssh/id_rsa.pub",
	}
	
	stdout, stderr, err := executeCommandInPod(ctx, r.syncer.DestinationK8sClient, namespace, podName, cmd)
	if err != nil {
		log.WithFields(logrus.Fields{
			"stderr": stderr,
			"error":  err,
		}).Error("[DR-SYNC-ERROR] Failed to get public key")
		return "", fmt.Errorf("failed to get public key: %v", err)
	}
	
	publicKey := strings.TrimSpace(stdout)
	if publicKey == "" {
		return "", fmt.Errorf("empty public key returned")
	}
	
	log.WithFields(logrus.Fields{
		"public_key_length": len(publicKey),
	}).Debug("[DR-SYNC-DETAIL] Successfully retrieved public key")
	
	return publicKey, nil
}

// testSSHConnectivity tests SSH connectivity from the rsync pod to the agent pod
func (r *RsyncController) testSSHConnectivity(ctx context.Context, namespace, podName, agentIP string, port int) error {
	log.WithFields(logrus.Fields{
		"pod":       podName,
		"namespace": namespace,
		"agent_ip":  agentIP,
		"port":      port,
	}).Info("[DR-SYNC-DETAIL] Testing SSH connectivity")
	
	// Construct SSH command
	sshCommand := fmt.Sprintf("ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i /root/.ssh/id_rsa -p %d root@%s 'echo SSH connectivity test'", port, agentIP)
	
	cmd := []string{"sh", "-c", sshCommand}
	
	stdout, stderr, err := executeCommandInPod(ctx, r.syncer.DestinationK8sClient, namespace, podName, cmd)
	if err != nil {
		log.WithFields(logrus.Fields{
			"stderr": stderr,
			"error":  err,
		}).Error("[DR-SYNC-ERROR] SSH connectivity test failed")
		return fmt.Errorf("SSH connectivity test failed: %v", err)
	}
	
	log.WithFields(logrus.Fields{
		"stdout": stdout,
	}).Info("[DR-SYNC-DETAIL] SSH connectivity test successful")
	
	return nil
}

// performRsync runs the rsync command in the rsync pod
func (r *RsyncController) performRsync(ctx context.Context, namespace, podName, agentIP, mountPath string) error {
	log.WithFields(logrus.Fields{
		"pod":        podName,
		"namespace":  namespace,
		"agent_ip":   agentIP,
		"mount_path": mountPath,
	}).Info("[DR-SYNC-DETAIL] Running rsync command")
	
	// Default rsync options
	rsyncOptions := []string{
		"--archive",
		"--verbose",
		"--delete",
		"--human-readable",
	}
	
	// Combine rsync options
	rsyncOptsStr := strings.Join(rsyncOptions, " ")
	
	// Build the rsync command with tee to log the output
	rsyncCmd := fmt.Sprintf("rsync %s -e 'ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i /root/.ssh/id_rsa -p 2222' root@%s:%s/ /data/ | tee /var/log/rsync.log",
		rsyncOptsStr, agentIP, mountPath)
	
	log.WithFields(logrus.Fields{
		"rsync_cmd": rsyncCmd,
	}).Info("[DR-SYNC-DETAIL] Executing rsync command")
	
	cmd := []string{"sh", "-c", rsyncCmd}
	
	stdout, stderr, err := executeCommandInPod(ctx, r.syncer.DestinationK8sClient, namespace, podName, cmd)
	if err != nil {
		log.WithFields(logrus.Fields{
			"stderr": stderr,
			"error":  err,
		}).Error("[DR-SYNC-ERROR] Rsync command failed")
		return fmt.Errorf("rsync command failed: %v", err)
	}
	
	// Check if rsync was successful
	if strings.Contains(stderr, "rsync error") {
		log.WithFields(logrus.Fields{
			"stderr": stderr,
		}).Error("[DR-SYNC-ERROR] Rsync error detected in output")
		return fmt.Errorf("rsync error: %s", stderr)
	}
	
	log.WithFields(logrus.Fields{
		"pod":        podName,
		"namespace":  namespace,
		"agent_ip":   agentIP,
		"mount_path": mountPath,
	}).Info("[DR-SYNC-DETAIL] Rsync command executed successfully")
	
	// Output first 100 characters of stdout to help with debugging
	if len(stdout) > 100 {
		log.WithFields(logrus.Fields{
			"stdout_preview": stdout[:100] + "...",
		}).Info("[DR-SYNC-DETAIL] Rsync output preview")
		
		// Log the full output with multiple log entries for better visibility in logs
		lines := strings.Split(stdout, "\n")
		for i, line := range lines {
			if len(line) > 0 {
				log.WithFields(logrus.Fields{
					"line_num": i + 1,
					"content":  line,
				}).Debug("[DR-SYNC-OUTPUT] Rsync output line")
			}
		}
	} else if len(stdout) > 0 {
		log.WithFields(logrus.Fields{
			"stdout": stdout,
		}).Info("[DR-SYNC-DETAIL] Rsync output")
	}
	
	return nil
}

// cleanupRsyncDeployment deletes the rsync deployment
func (r *RsyncController) cleanupRsyncDeployment(ctx context.Context, deployment *appsv1.Deployment) error {
	log.WithFields(logrus.Fields{
		"deployment": deployment.Name,
		"namespace":  deployment.Namespace,
	}).Info("[DR-SYNC-DETAIL] Cleaning up rsync deployment")
	
	// Set foreground deletion to ensure pods are deleted first
	deletePolicy := metav1.DeletePropagationForeground
	deleteOptions := metav1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	}
	
	err := r.syncer.DestinationK8sClient.AppsV1().Deployments(deployment.Namespace).Delete(ctx, deployment.Name, deleteOptions)
	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete rsync deployment: %v", err)
		}
		// Deployment already deleted, which is fine
		log.WithFields(logrus.Fields{
			"deployment": deployment.Name,
			"namespace":  deployment.Namespace,
		}).Info("[DR-SYNC-DETAIL] Deployment not found, already deleted")
		return nil
	}
	
	// Wait for the deployment to be deleted
	err = r.waitForDeploymentDeletion(ctx, deployment.Namespace, deployment.Name)
	if err != nil {
		log.WithFields(logrus.Fields{
			"deployment": deployment.Name,
			"namespace":  deployment.Namespace,
			"error":      err,
		}).Warn("[DR-SYNC-DETAIL] Timed out waiting for deployment deletion, continuing anyway")
		// We'll continue anyway since we've initiated deletion
	}
	
	log.WithFields(logrus.Fields{
		"deployment": deployment.Name,
		"namespace":  deployment.Namespace,
	}).Info("[DR-SYNC-DETAIL] Successfully deleted rsync deployment")
	
	return nil
}

// waitForDeploymentDeletion waits for a deployment to be deleted
func (r *RsyncController) waitForDeploymentDeletion(ctx context.Context, namespace, name string) error {
	log.WithFields(logrus.Fields{
		"deployment": name,
		"namespace":  namespace,
	}).Info("[DR-SYNC-DETAIL] Waiting for deployment deletion")
	
	// Create a timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	
	// Poll until the deployment is gone or timeout
	return wait.PollUntilContextCancel(timeoutCtx, 5*time.Second, true, func(ctx context.Context) (bool, error) {
		_, err := r.syncer.DestinationK8sClient.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			// Deployment is gone, which is what we want
			log.WithFields(logrus.Fields{
				"deployment": name,
				"namespace":  namespace,
			}).Info("[DR-SYNC-DETAIL] Deployment successfully deleted")
			return true, nil
		}
		if err != nil {
			// Some other error, log it but keep waiting
			log.WithFields(logrus.Fields{
				"deployment": name,
				"namespace":  namespace,
				"error":      err,
			}).Warn("[DR-SYNC-DETAIL] Error checking deployment existence, will retry")
		}
		// Deployment still exists, keep waiting
		log.WithFields(logrus.Fields{
			"deployment": name,
			"namespace":  namespace,
		}).Debug("[DR-SYNC-DETAIL] Deployment still exists, waiting for deletion")
		return false, nil
	})
}
