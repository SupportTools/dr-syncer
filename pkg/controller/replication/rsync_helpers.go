package replication

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/supporttools/dr-syncer/pkg/logging"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Use the exported PVCClusterKey from context_keys.go

// PVCLockInfo contains information about a PVC lock
type PVCLockInfo struct {
	ControllerPodName string
	Timestamp         string
}

// contains checks if a string is in a slice
func contains(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// execCommandOnPod executes a command in a pod
func (p *PVCSyncer) execCommandOnPod(ctx context.Context, namespace, podName string, command []string) (string, string, error) {
	// Add debug logging to show which cluster we're executing commands on
	log.WithFields(logrus.Fields{
		"namespace":          namespace,
		"pod_name":           podName,
		"command":            strings.Join(command, " "),
		"source_cluster_url": p.SourceConfig.Host,
	}).Debug(logging.LogTagDetail + " Executing command on pod using source cluster")

	req := p.SourceK8sClient.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec")

	req.VersionedParams(&corev1.PodExecOptions{
		Command: command,
		Stdin:   false,
		Stdout:  true,
		Stderr:  true,
		TTY:     false,
	}, scheme.ParameterCodec)

	var stdout, stderr bytes.Buffer
	exec, err := remotecommand.NewSPDYExecutor(p.SourceConfig, "POST", req.URL())
	if err != nil {
		return "", "", err
	}

	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: &stdout,
		Stderr: &stderr,
		Tty:    false,
	})

	return stdout.String(), stderr.String(), err
}

// FindPVCNode finds a node where the PVC is mounted
func (p *PVCSyncer) FindPVCNode(ctx context.Context, c client.Client, namespace, pvcName string) (string, error) {
	// Check if the context has a cluster type specified
	var clientK8s kubernetes.Interface
	var clientRest *rest.Config
	var clusterType string

	// Check context for cluster identification
	if val := ctx.Value(PVCClusterKey); val != nil {
		if clusterStr, ok := val.(string); ok {
			clusterType = clusterStr
			if clusterStr == "destination" {
				clientK8s = p.DestinationK8sClient
				clientRest = p.DestinationConfig
				log.WithFields(logrus.Fields{
					"namespace": namespace,
					"cluster":   "destination",
					"host":      p.DestinationConfig.Host,
				}).Info(logging.LogTagDetail + " Using destination cluster for PVC node lookup based on context")
			} else {
				clientK8s = p.SourceK8sClient
				clientRest = p.SourceConfig
				log.WithFields(logrus.Fields{
					"namespace": namespace,
					"cluster":   "source",
					"host":      p.SourceConfig.Host,
				}).Info(logging.LogTagDetail + " Using source cluster for PVC node lookup based on context")
			}
		}
	} else {
		// Default to source if not specified
		clientK8s = p.SourceK8sClient
		clientRest = p.SourceConfig
		clusterType = "source (default)"
	}

	// Add extremely detailed debug logging to troubleshoot cluster connection issues
	log.WithFields(logrus.Fields{
		"namespace":               namespace,
		"pvc_name":                pvcName,
		"cluster_type":            clusterType,
		"source_cluster_url":      p.SourceConfig.Host,
		"destination_namespace":   p.DestinationNamespace,
		"destination_cluster_url": p.DestinationConfig.Host,
		"client_type":             fmt.Sprintf("%T", c),
		"source_client_type":      fmt.Sprintf("%T", p.SourceClient),
		"direct_client_type":      fmt.Sprintf("%T", p.SourceK8sClient),
		"function":                "FindPVCNode",
	}).Info(logging.LogTagDetail + " CLUSTER CONFIGURATION DETAILS FOR PVC NODE LOOKUP - checking which cluster we're using")

	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc_name":  pvcName,
		"function":  "FindPVCNode",
	}).Debug(logging.LogTagDetail + " Starting PVC node lookup")

	// Get all nodes where the PVC is mounted
	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc_name":  pvcName,
	}).Debug(logging.LogTagDetail + " Finding all nodes where PVC is mounted")

	// Use custom node finding function with the right client
	nodes, err := p.findPVCNodesWithClient(ctx, c, clientK8s, clientRest, namespace, pvcName)
	if err != nil {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
			"error":     err,
		}).Error(logging.LogTagError + " Failed to find PVC nodes")
		return "", err
	}
	log.WithFields(logrus.Fields{
		"namespace":   namespace,
		"pvc_name":    pvcName,
		"nodes_found": nodes,
	}).Debug(logging.LogTagDetail + " Found nodes for PVC")

	// If no nodes are found, return an error
	if len(nodes) == 0 {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
		}).Error(logging.LogTagError + " No nodes found with PVC mounted")
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
		}).Error(logging.LogTagError + " No nodes found with PVC mounted")
		return "", fmt.Errorf("no nodes found with PVC %s/%s mounted", namespace, pvcName)
	}

	// Return the first node
	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc_name":  pvcName,
		"node":      nodes[0],
	}).Info(logging.LogTagDetail + " Selected node with PVC mounted")

	return nodes[0], nil
}

// FindPVCNodes finds all nodes where a PVC is mounted
func (p *PVCSyncer) FindPVCNodes(ctx context.Context, c client.Client, namespace, pvcName string) ([]string, error) {
	// Add debug logging to show cluster URL and function entry
	log.WithFields(logrus.Fields{
		"namespace":          namespace,
		"pvc_name":           pvcName,
		"source_cluster_url": p.SourceConfig.Host,
		"function":           "FindPVCNodes",
	}).Info(logging.LogTagDetail + " Starting PVC nodes lookup on source cluster")

	var nodes []string

	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc_name":  pvcName,
	}).Info(logging.LogTagDetail + " Finding nodes where PVC is mounted")

	// List pods in the namespace
	log.WithFields(logrus.Fields{
		"namespace":                       namespace,
		"pvc_name":                        pvcName,
		"client_type":                     fmt.Sprintf("%T", c),
		"source_client_type":              fmt.Sprintf("%T", p.SourceClient),
		"using_controller_runtime_client": true,
	}).Debug(logging.LogTagDetail + " Listing all pods in namespace")

	podList := &corev1.PodList{}
	if err := c.List(ctx, podList, client.InNamespace(namespace)); err != nil {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"error":     err,
		}).Error(logging.LogTagError + " Failed to list pods")
		return nil, fmt.Errorf("failed to list pods: %v", err)
	}

	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pod_count": len(podList.Items),
	}).Debug(logging.LogTagDetail + " Found pods in namespace")

	// Find pods using this PVC and collect their nodes
	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc_name":  pvcName,
		"pod_count": len(podList.Items),
	}).Debug(logging.LogTagDetail + " Iterating over pods to check for PVC usage")

	for _, pod := range podList.Items {
		log.WithFields(logrus.Fields{
			"pod_name":  pod.Name,
			"pod_phase": string(pod.Status.Phase),
		}).Debug(logging.LogTagDetail + " Checking pod for PVC usage")

		// Skip pods that are not running
		if pod.Status.Phase != corev1.PodRunning {
			log.WithFields(logrus.Fields{
				"pod_name":  pod.Name,
				"pod_phase": string(pod.Status.Phase),
			}).Debug(logging.LogTagDetail + " Skipping pod as it is not running")
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
				}).Info(logging.LogTagDetail + " Found pod using PVC")

				// Add the node if not already in the list
				if !contains(nodes, pod.Spec.NodeName) {
					log.WithFields(logrus.Fields{
						"namespace": namespace,
						"pvc_name":  pvcName,
						"node_name": pod.Spec.NodeName,
					}).Debug(logging.LogTagDetail + " Adding node to list")
					nodes = append(nodes, pod.Spec.NodeName)
				}
			}
		}
	}

	// If no running pod is found, try to find nodes with the PVC attached via volume attachments
	if len(nodes) == 0 {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
		}).Debug(logging.LogTagDetail + " No running pods found using PVC, checking volume attachments")

		// Get the PVC to find its volume name
		pvc := &corev1.PersistentVolumeClaim{}
		log.WithFields(logrus.Fields{
			"namespace":   namespace,
			"pvc_name":    pvcName,
			"client_type": fmt.Sprintf("%T", c),
		}).Debug(logging.LogTagDetail + " Getting PVC details using controller-runtime client")

		if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: pvcName}, pvc); err != nil {
			log.WithFields(logrus.Fields{
				"namespace": namespace,
				"pvc_name":  pvcName,
				"error":     err,
			}).Error(logging.LogTagError + " Failed to get PVC")

			// Try with direct API client as a fallback
			log.WithFields(logrus.Fields{
				"namespace": namespace,
				"pvc_name":  pvcName,
			}).Debug(logging.LogTagDetail + " Trying to get PVC with direct API client as fallback")

			var pvcErr error
			pvc, pvcErr = p.SourceK8sClient.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
			if pvcErr != nil {
				log.WithFields(logrus.Fields{
					"namespace": namespace,
					"pvc_name":  pvcName,
					"error":     pvcErr,
				}).Error(logging.LogTagError + " Failed to get PVC with direct API client too")
				return nil, fmt.Errorf("failed to get PVC with both clients: original: %v, direct: %v", err, pvcErr)
			}

			log.WithFields(logrus.Fields{
				"namespace": namespace,
				"pvc_name":  pvcName,
				"pv_name":   pvc.Spec.VolumeName,
			}).Info(logging.LogTagDetail + " Successfully got PVC with direct API client")
		} else {
			log.WithFields(logrus.Fields{
				"namespace": namespace,
				"pvc_name":  pvcName,
				"pv_name":   pvc.Spec.VolumeName,
			}).Info(logging.LogTagDetail + " Got PVC with controller-runtime client")
		}

		// If no PV is bound yet, return an empty list
		if pvc.Spec.VolumeName == "" {
			log.WithFields(logrus.Fields{
				"namespace": namespace,
				"pvc_name":  pvcName,
			}).Debug(logging.LogTagDetail + " PVC is not bound to a PV yet")
			return nil, nil
		}

		// Get volume attachments
		log.WithFields(logrus.Fields{
			"namespace":          namespace,
			"pvc_name":           pvcName,
			"pv_name":            pvc.Spec.VolumeName,
			"source_cluster_url": p.SourceConfig.Host,
		}).Debug(logging.LogTagDetail + " Listing volume attachments with direct client")

		volumeAttachments, err := p.SourceK8sClient.StorageV1().VolumeAttachments().List(ctx, metav1.ListOptions{})
		if err != nil {
			log.WithFields(logrus.Fields{
				"namespace": namespace,
				"pvc_name":  pvcName,
				"error":     err,
			}).Error(logging.LogTagError + " Failed to list volume attachments")
			return nil, fmt.Errorf("failed to list volume attachments: %v", err)
		}

		log.WithFields(logrus.Fields{
			"namespace":        namespace,
			"pvc_name":         pvcName,
			"attachment_count": len(volumeAttachments.Items),
		}).Debug(logging.LogTagDetail + " Found volume attachments")

		// Print PVC details for debugging
		log.WithFields(logrus.Fields{
			"namespace":     namespace,
			"pvc_name":      pvcName,
			"pv_name":       pvc.Spec.VolumeName,
			"pvc_phase":     string(pvc.Status.Phase),
			"access_modes":  fmt.Sprintf("%v", pvc.Spec.AccessModes),
			"storage_class": pvc.Spec.StorageClassName,
		}).Info(logging.LogTagDetail + " PVC details for node lookup")

		// Find volume attachments for this PVC's PV
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
			"pv_name":   pvc.Spec.VolumeName,
		}).Debug(logging.LogTagDetail + " Iterating over volume attachments to find matching PV")
		matchFound := false
		for _, va := range volumeAttachments.Items {
			// Print info about each volume attachment
			pvName := "nil"
			if va.Spec.Source.PersistentVolumeName != nil {
				pvName = *va.Spec.Source.PersistentVolumeName

				// Check if this attachment matches our PV
				if pvName == pvc.Spec.VolumeName {
					matchFound = true
					fmt.Printf("Found volume attachment: %s on node %s for PV %s\n", va.Name, va.Spec.NodeName, pvName)
					log.WithFields(logrus.Fields{
						"namespace":     namespace,
						"pvc_name":      pvcName,
						"pv_name":       pvc.Spec.VolumeName,
						"attachment":    va.Name,
						"attached_node": va.Spec.NodeName,
					}).Info(logging.LogTagDetail + " Found volume attachment for PVC")

					// Add the node if not already in the list
					if !contains(nodes, va.Spec.NodeName) {
						fmt.Printf("Adding node %s to list from volume attachment\n", va.Spec.NodeName)
						nodes = append(nodes, va.Spec.NodeName)
					}
				}
			}

			// Log about this attachment for debugging
			log.WithFields(logrus.Fields{
				"attachment_name": va.Name,
				"node_name":       va.Spec.NodeName,
				"pv_name":         pvName,
				"matches_our_pv":  pvName == pvc.Spec.VolumeName,
			}).Debug(logging.LogTagDetail + " Examining volume attachment")
		}

		if !matchFound {
			log.WithFields(logrus.Fields{
				"namespace":        namespace,
				"pvc_name":         pvcName,
				"pv_name":          pvc.Spec.VolumeName,
				"attachment_count": len(volumeAttachments.Items),
			}).Warn(logging.LogTagWarn + " No matching volume attachments found for this PV")
			fmt.Printf("No volume attachments found for PV %s among %d attachments\n",
				pvc.Spec.VolumeName, len(volumeAttachments.Items))
		}
	}

	fmt.Printf("Final list of nodes: %v\n", nodes)
	return nodes, nil
}

// FindAgentPod finds the DR-Syncer-Agent running on the given node
func (p *PVCSyncer) FindAgentPod(ctx context.Context, nodeName string) (*corev1.Pod, string, error) {
	log.WithFields(logrus.Fields{
		"node":               nodeName,
		"source_cluster_url": p.SourceConfig.Host,
	}).Info(logging.LogTagDetail + " Finding DR-Syncer-Agent on node using source cluster")

	// List pods with agent selector
	podList, err := p.SourceK8sClient.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		LabelSelector: "app=dr-syncer-agent",
	})
	if err != nil {
		log.WithFields(logrus.Fields{
			"node":  nodeName,
			"error": err,
		}).Error(logging.LogTagError + " Failed to list agent pods")
		return nil, "", fmt.Errorf("failed to list agent pods: %v", err)
	}

	// Find the agent pod running on the given node
	var agentPod *corev1.Pod
	for i, pod := range podList.Items {
		if pod.Spec.NodeName == nodeName && pod.Status.Phase == corev1.PodRunning {
			agentPod = &podList.Items[i]
			log.WithFields(logrus.Fields{
				"node":      nodeName,
				"agent_pod": agentPod.Name,
				"namespace": agentPod.Namespace,
			}).Info(logging.LogTagDetail + " Found DR-Syncer-Agent on node")
			break
		}
	}

	if agentPod == nil {
		log.WithFields(logrus.Fields{
			"node": nodeName,
		}).Error(logging.LogTagError + " No DR-Syncer-Agent found on node")
		return nil, "", fmt.Errorf("no DR-Syncer-Agent found on node %s", nodeName)
	}

	// Get the node's IP address
	node, err := p.SourceK8sClient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		log.WithFields(logrus.Fields{
			"node":  nodeName,
			"error": err,
		}).Error(logging.LogTagError + " Failed to get node")
		return nil, "", fmt.Errorf("failed to get node %s: %v", nodeName, err)
	}

	// Get the node's external IP (if available) or internal IP
	var nodeIP string
	for _, addr := range node.Status.Addresses {
		if addr.Type == corev1.NodeExternalIP && addr.Address != "" {
			nodeIP = addr.Address
			break
		} else if addr.Type == corev1.NodeInternalIP && nodeIP == "" {
			nodeIP = addr.Address
		}
	}

	if nodeIP == "" {
		log.WithFields(logrus.Fields{
			"node": nodeName,
		}).Error(logging.LogTagError + " No IP address found for node")
		return nil, "", fmt.Errorf("no IP address found for node %s", nodeName)
	}

	log.WithFields(logrus.Fields{
		"node":    nodeName,
		"node_ip": nodeIP,
	}).Info(logging.LogTagDetail + " Retrieved node IP address")

	return agentPod, nodeIP, nil
}

// FindPVCMountPath finds the mount path for a PVC on the given agent pod's node
func (p *PVCSyncer) FindPVCMountPath(ctx context.Context, namespace, pvcName string, agentPod *corev1.Pod) (string, error) {
	log.WithFields(logrus.Fields{
		"namespace":          namespace,
		"pvc_name":           pvcName,
		"agent_pod":          agentPod.Name,
		"agent_node":         agentPod.Spec.NodeName,
		"source_cluster_url": p.SourceConfig.Host,
	}).Info(logging.LogTagDetail + " Finding mount path for PVC using source cluster")

	// Get the PVC to find its volume name
	pvc, err := p.SourceK8sClient.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
			"error":     err,
		}).Error(logging.LogTagError + " Failed to get PVC")
		return "", fmt.Errorf("failed to get PVC: %v", err)
	}

	// If no PV is bound yet, we can't find a mount path
	if pvc.Spec.VolumeName == "" {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
		}).Error(logging.LogTagError + " PVC is not bound to a PV")
		return "", fmt.Errorf("PVC %s/%s is not bound to a PV", namespace, pvcName)
	}

	// Get the PV
	pv, err := p.SourceK8sClient.CoreV1().PersistentVolumes().Get(ctx, pvc.Spec.VolumeName, metav1.GetOptions{})
	if err != nil {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
			"pv_name":   pvc.Spec.VolumeName,
			"error":     err,
		}).Error(logging.LogTagError + " Failed to get PV")
		return "", fmt.Errorf("failed to get PV: %v", err)
	}

	// If the PV is not bound, we can't find a mount path
	if pv.Status.Phase != corev1.VolumeBound {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
			"pv_name":   pvc.Spec.VolumeName,
		}).Error(logging.LogTagError + " PV is not bound")
		return "", fmt.Errorf("PV %s/%s is not bound", namespace, pvc.Spec.VolumeName)
	}

	// First try: Use df to find the mount path - most efficient approach
	log.WithFields(logrus.Fields{
		"pvc_name":   pvcName,
		"pv_name":    pvc.Spec.VolumeName,
		"agent_pod":  agentPod.Name,
		"approach":   "df-grep",
	}).Info(logging.LogTagDetail + " Trying df approach to find mount path")

	// Execute df command with timeout
	dfCmd := []string{
		"bash",
		"-c",
		fmt.Sprintf("df | grep -E '%s|%s' | awk '{print $6}' | head -n 1", 
			pvc.Spec.VolumeName, pvcName),
	}

	// Create a context with a short timeout
	dfCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	dfStdout, dfStderr, dfErr := p.execCommandOnPod(dfCtx, agentPod.Namespace, agentPod.Name, dfCmd)
	mountPath := strings.TrimSpace(dfStdout)
	
	if mountPath != "" {
		log.WithFields(logrus.Fields{
			"pvc_name":   pvcName,
			"pv_name":    pvc.Spec.VolumeName,
			"mount_path": mountPath,
			"approach":   "df-grep",
		}).Info(logging.LogTagDetail + " Found mount path using df approach")
		return mountPath, nil
	}

	// If df approach failed, try the 'mount' command - moderately efficient
	log.WithFields(logrus.Fields{
		"pvc_name":   pvcName,
		"pv_name":    pvc.Spec.VolumeName,
		"agent_pod":  agentPod.Name,
		"approach":   "mount-grep",
	}).Info(logging.LogTagDetail + " Trying mount approach to find mount path")

	mountCmd := []string{
		"bash",
		"-c",
		"mount | grep " + pvc.Spec.VolumeName + " | awk '{print $3}' | head -n 1",
	}

	// Create a context with a short timeout
	mountCtx, mountCancel := context.WithTimeout(ctx, 15*time.Second)
	defer mountCancel()

	mountStdout, mountStderr, mountErr := p.execCommandOnPod(mountCtx, agentPod.Namespace, agentPod.Name, mountCmd)
	mountPath = strings.TrimSpace(mountStdout)
	
	if mountPath != "" {
		log.WithFields(logrus.Fields{
			"pvc_name":   pvcName,
			"pv_name":    pvc.Spec.VolumeName,
			"mount_path": mountPath,
			"approach":   "mount-grep",
		}).Info(logging.LogTagDetail + " Found mount path using mount approach")
		return mountPath, nil
	}

	// Last resort: Use the find command with a strict timeout - least efficient but most thorough
	log.WithFields(logrus.Fields{
		"pvc_name":   pvcName,
		"pv_name":    pvc.Spec.VolumeName,
		"agent_pod":  agentPod.Name,
		"approach":   "find-command",
	}).Info(logging.LogTagDetail + " Trying find approach to find mount path (with timeout)")

	// Use a time-limited find command to avoid getting stuck
	findCmd := []string{
		"bash",
		"-c",
		fmt.Sprintf("timeout 30s find /var/lib/kubelet/pods -name %s -type d -path '*/volumes/*' | grep -v plugins | head -n 1", 
			pvc.Spec.VolumeName),
	}

	// Create a context with a reasonable timeout
	findCtx, findCancel := context.WithTimeout(ctx, 45*time.Second)
	defer findCancel()

	findStdout, findStderr, findErr := p.execCommandOnPod(findCtx, agentPod.Namespace, agentPod.Name, findCmd)
	mountPath = strings.TrimSpace(findStdout)

	if mountPath == "" {
		// If we still haven't found anything, log all our attempts and return an error
		log.WithFields(logrus.Fields{
			"namespace":   namespace,
			"pvc_name":    pvcName,
			"pv_name":     pvc.Spec.VolumeName,
			"agent_pod":   agentPod.Name,
			"df_error":    dfErr,
			"df_stderr":   dfStderr,
			"mount_error": mountErr,
			"mount_stderr": mountStderr,
			"find_error":  findErr, 
			"find_stderr": findStderr,
		}).Error(logging.LogTagError + " Mount path not found for PVC after trying all methods")
		return "", fmt.Errorf("mount path not found for PVC %s/%s after trying multiple methods", namespace, pvcName)
	}

	log.WithFields(logrus.Fields{
		"namespace":  namespace,
		"pvc_name":   pvcName,
		"pv_name":    pvc.Spec.VolumeName,
		"agent_pod":  agentPod.Name,
		"mount_path": mountPath,
	}).Info(logging.LogTagDetail + " Found mount path for PVC")

	return mountPath, nil
}

// PushPublicKeyToAgent pushes the public key to the agent pod
func (p *PVCSyncer) PushPublicKeyToAgent(ctx context.Context, agentPod *corev1.Pod, publicKey, trackingInfo string) error {
	log.WithFields(logrus.Fields{
		"agent_pod":          agentPod.Name,
		"agent_ns":           agentPod.Namespace,
		"tracking_info":      trackingInfo,
		"source_cluster_url": p.SourceConfig.Host,
	}).Info(logging.LogTagDetail + " Pushing public key to agent pod using source cluster")

	// Format the authorized_keys entry with the tracking info as a comment
	authKeyEntry := fmt.Sprintf("# %s\n%s\n", trackingInfo, publicKey)

	// Append the key to authorized_keys
	cmd := []string{
		"bash",
		"-c",
		fmt.Sprintf("mkdir -p /root/.ssh && echo '%s' >> /root/.ssh/authorized_keys && chmod 600 /root/.ssh/authorized_keys", authKeyEntry),
	}

	stdout, stderr, err := p.execCommandOnPod(ctx, agentPod.Namespace, agentPod.Name, cmd)
	if err != nil {
		log.WithFields(logrus.Fields{
			"agent_pod": agentPod.Name,
			"error":     err,
			"stderr":    stderr,
		}).Error(logging.LogTagError + " Failed to push public key to agent pod")
		return fmt.Errorf("failed to push public key to agent pod: %v: %s", err, stderr)
	}

	log.WithFields(logrus.Fields{
		"agent_pod": agentPod.Name,
		"stdout":    stdout,
	}).Debug(logging.LogTagDetail + " Public key pushed to agent pod")

	return nil
}

// UpdateSourcePVCAnnotations updates annotations on the source PVC to record the sync status
func (p *PVCSyncer) UpdateSourcePVCAnnotations(ctx context.Context, namespace, pvcName string) error {
	log.WithFields(logrus.Fields{
		"namespace":          namespace,
		"pvc_name":           pvcName,
		"source_cluster_url": p.SourceConfig.Host,
	}).Info(logging.LogTagDetail + " Updating source PVC annotations using source cluster")

	// Get the PVC
	pvc, err := p.SourceK8sClient.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
			"error":     err,
		}).Error("Failed to get source PVC for annotation update")
		return fmt.Errorf("failed to get source PVC for annotation update: %v", err)
	}

	// Set annotations
	if pvc.Annotations == nil {
		pvc.Annotations = make(map[string]string)
	}

	// Record sync time and details
	now := time.Now().UTC()
	pvc.Annotations["dr-syncer.io/last-sync-time"] = now.Format(time.RFC3339)
	pvc.Annotations["dr-syncer.io/last-sync-status"] = "Completed"
	pvc.Annotations["dr-syncer.io/destination-namespace"] = p.DestinationNamespace
	pvc.Annotations["dr-syncer.io/destination-pvc"] = pvcName

	// Update the PVC
	_, err = p.SourceK8sClient.CoreV1().PersistentVolumeClaims(namespace).Update(ctx, pvc, metav1.UpdateOptions{})
	if err != nil {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
			"error":     err,
		}).Error("Failed to update source PVC annotations")
		return fmt.Errorf("failed to update source PVC annotations: %v", err)
	}

	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc_name":  pvcName,
	}).Info("Successfully updated source PVC annotations")

	return nil
}

// HasVolumeAttachments checks if the PVC has any volume attachments
func (p *PVCSyncer) HasVolumeAttachments(ctx context.Context, namespace, pvcName string) (bool, error) {
	log.WithFields(logrus.Fields{
		"namespace":          namespace,
		"pvc_name":           pvcName,
		"source_cluster_url": p.SourceConfig.Host,
	}).Info(logging.LogTagDetail + " Checking if PVC has volume attachments using source cluster")

	// Get the PVC
	pvc, err := p.SourceK8sClient.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
			"error":     err,
		}).Error(logging.LogTagError + " Failed to get PVC")
		return false, fmt.Errorf("failed to get PVC: %v", err)
	}

	// If no PV is bound yet, it can't have volume attachments
	if pvc.Spec.VolumeName == "" {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
		}).Info(logging.LogTagDetail + " PVC is not bound to a PV")
		return false, nil
	}

	// Check if there are any running pods using this PVC
	log.WithFields(logrus.Fields{
		"namespace":                       namespace,
		"pvc_name":                        pvcName,
		"client_type":                     fmt.Sprintf("%T", p.SourceClient),
		"source_k8s_client_type":          fmt.Sprintf("%T", p.SourceK8sClient),
		"using_controller_runtime_client": true,
	}).Debug(logging.LogTagDetail + " Client info for HasVolumeAttachments pod listing")

	// Try with controller-runtime client first
	podList := &corev1.PodList{}
	if err := p.SourceClient.List(ctx, podList, client.InNamespace(namespace)); err != nil {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"error":     err,
		}).Error(logging.LogTagError + " Failed to list pods with controller-runtime client")

		// IMPORTANT DEBUG: Try with direct client as fallback and be VERY explicit about which cluster
		log.WithFields(logrus.Fields{
			"namespace":                 namespace,
			"source_cluster_url":        p.SourceConfig.Host,
			"source_cluster_direct_url": p.SourceConfig.Host, // Extra logging to ensure cluster info is available
			"destination_namespace":     p.DestinationNamespace,
		}).Info(logging.LogTagDetail + " ATTEMPTING TO LIST PODS ON SOURCE CLUSTER using direct API client due to controller-runtime client failure")

		var apiPodList *corev1.PodList
		apiPodList, err = p.SourceK8sClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			log.WithFields(logrus.Fields{
				"namespace": namespace,
				"error":     err,
			}).Error(logging.LogTagError + " Failed to list pods with direct client too")
			return false, fmt.Errorf("failed to list pods with both clients: %v", err)
		}

		// Use the direct client results
		podList = apiPodList
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pod_count": len(podList.Items),
		}).Info(logging.LogTagDetail + " Successfully listed pods with direct client")
	} else {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pod_count": len(podList.Items),
		}).Debug(logging.LogTagDetail + " Successfully listed pods with controller-runtime client")
	}

	for _, pod := range podList.Items {
		if pod.Status.Phase == corev1.PodRunning {
			for _, volume := range pod.Spec.Volumes {
				if volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName == pvcName {
					log.WithFields(logrus.Fields{
						"namespace": namespace,
						"pvc_name":  pvcName,
						"pod_name":  pod.Name,
					}).Info(logging.LogTagDetail + " Found running pod using PVC")
					return true, nil
				}
			}
		}
	}

	// If no running pods are found, check volume attachments
	volumeAttachments, err := p.SourceK8sClient.StorageV1().VolumeAttachments().List(ctx, metav1.ListOptions{})
	if err != nil {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
			"error":     err,
		}).Error(logging.LogTagError + " Failed to list volume attachments")
		return false, fmt.Errorf("failed to list volume attachments: %v", err)
	}

	for _, va := range volumeAttachments.Items {
		if va.Spec.Source.PersistentVolumeName != nil && *va.Spec.Source.PersistentVolumeName == pvc.Spec.VolumeName {
			log.WithFields(logrus.Fields{
				"namespace":  namespace,
				"pvc_name":   pvcName,
				"pv_name":    pvc.Spec.VolumeName,
				"attachment": va.Name,
				"node":       va.Spec.NodeName,
			}).Info(logging.LogTagDetail + " Found volume attachment for PVC")
			return true, nil
		}
	}

	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc_name":  pvcName,
	}).Info(logging.LogTagDetail + " No volume attachments found for PVC")
	return false, nil
}

// AcquirePVCLock tries to acquire a lock on the source PVC
func (p *PVCSyncer) AcquirePVCLock(ctx context.Context, namespace, pvcName string) (bool, *PVCLockInfo, error) {
	log.WithFields(logrus.Fields{
		"namespace":          namespace,
		"pvc_name":           pvcName,
		"source_cluster_url": p.SourceConfig.Host,
	}).Info(logging.LogTagDetail + " Attempting to acquire lock on PVC using source cluster")

	// Get the source PVC
	pvc, err := p.SourceK8sClient.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
			"error":     err,
		}).Error(logging.LogTagError + " Failed to get source PVC")
		return false, nil, fmt.Errorf("failed to get source PVC: %v", err)
	}

	// Check if the PVC is already locked
	if pvc.Annotations != nil && pvc.Annotations["dr-syncer.io/lock-owner"] != "" {
		// Get our pod name
		podName := os.Getenv("POD_NAME")
		if podName == "" {
			podName = "unknown"
		}

		// If we already own the lock, return success
		if pvc.Annotations["dr-syncer.io/lock-owner"] == podName {
			log.WithFields(logrus.Fields{
				"namespace": namespace,
				"pvc_name":  pvcName,
				"pod_name":  podName,
			}).Info(logging.LogTagDetail + " We already own the lock on PVC")

			return true, &PVCLockInfo{
				ControllerPodName: podName,
				Timestamp:         pvc.Annotations["dr-syncer.io/lock-timestamp"],
			}, nil
		}

		// Check if the lock is stale (older than 1 hour)
		if pvc.Annotations["dr-syncer.io/lock-timestamp"] != "" {
			lockTime, err := time.Parse(time.RFC3339, pvc.Annotations["dr-syncer.io/lock-timestamp"])
			if err == nil {
				if time.Since(lockTime) > 1*time.Hour {
					log.WithFields(logrus.Fields{
						"namespace":    namespace,
						"pvc_name":     pvcName,
						"lock_owner":   pvc.Annotations["dr-syncer.io/lock-owner"],
						"lock_time":    lockTime,
						"current_time": time.Now(),
					}).Info(logging.LogTagDetail + " Lock is stale, taking over")
				} else {
					// Lock is not stale, return the lock info
					return false, &PVCLockInfo{
						ControllerPodName: pvc.Annotations["dr-syncer.io/lock-owner"],
						Timestamp:         pvc.Annotations["dr-syncer.io/lock-timestamp"],
					}, nil
				}
			}
		} else {
			// No timestamp, but has owner - return the lock info
			return false, &PVCLockInfo{
				ControllerPodName: pvc.Annotations["dr-syncer.io/lock-owner"],
				Timestamp:         "",
			}, nil
		}
	}

	// Initialize annotations if needed
	if pvc.Annotations == nil {
		pvc.Annotations = make(map[string]string)
	}

	// Get our pod name
	podName := os.Getenv("POD_NAME")
	if podName == "" {
		podName = "unknown"
	}

	// Set lock annotations
	pvc.Annotations["dr-syncer.io/lock-owner"] = podName
	pvc.Annotations["dr-syncer.io/lock-timestamp"] = time.Now().UTC().Format(time.RFC3339)

	// Update the PVC
	_, err = p.SourceK8sClient.CoreV1().PersistentVolumeClaims(namespace).Update(ctx, pvc, metav1.UpdateOptions{})
	if err != nil {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
			"error":     err,
		}).Error(logging.LogTagError + " Failed to update source PVC to acquire lock")
		return false, nil, fmt.Errorf("failed to update source PVC to acquire lock: %v", err)
	}

	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc_name":  pvcName,
		"pod_name":  podName,
	}).Info(logging.LogTagDetail + " Lock acquired on PVC")

	return true, &PVCLockInfo{
		ControllerPodName: podName,
		Timestamp:         pvc.Annotations["dr-syncer.io/lock-timestamp"],
	}, nil
}

// findPVCNodesWithClient finds all nodes where a PVC is mounted using the specified Kubernetes client
func (p *PVCSyncer) findPVCNodesWithClient(ctx context.Context, c client.Client, k8sClient kubernetes.Interface, restConfig *rest.Config, namespace, pvcName string) ([]string, error) {
	// Add debug logging to show cluster URL and function entry
	log.WithFields(logrus.Fields{
		"namespace":   namespace,
		"pvc_name":    pvcName,
		"cluster_url": restConfig.Host,
		"function":    "findPVCNodesWithClient",
		"client_type": fmt.Sprintf("%T", k8sClient),
	}).Info(logging.LogTagDetail + " Starting PVC nodes lookup with specified client")

	var nodes []string

	// List pods in the namespace
	log.WithFields(logrus.Fields{
		"namespace":    namespace,
		"pvc_name":     pvcName,
		"using_client": fmt.Sprintf("%T", k8sClient),
	}).Debug(logging.LogTagDetail + " Listing all pods in namespace")

	podList, err := k8sClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"error":     err,
		}).Error(logging.LogTagError + " Failed to list pods")
		return nil, fmt.Errorf("failed to list pods: %v", err)
	}

	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pod_count": len(podList.Items),
	}).Debug(logging.LogTagDetail + " Found pods in namespace")

	// Find pods using this PVC and collect their nodes
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
				}).Info(logging.LogTagDetail + " Found pod using PVC")

				// Add the node if not already in the list
				if !contains(nodes, pod.Spec.NodeName) {
					nodes = append(nodes, pod.Spec.NodeName)
				}
			}
		}
	}

	// If no running pod is found, try to find nodes with the PVC attached via volume attachments
	if len(nodes) == 0 {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
		}).Debug(logging.LogTagDetail + " No running pods found using PVC, checking volume attachments")

		// Get the PVC to find its volume name
		pvc, err := k8sClient.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
		if err != nil {
			log.WithFields(logrus.Fields{
				"namespace": namespace,
				"pvc_name":  pvcName,
				"error":     err,
			}).Error(logging.LogTagError + " Failed to get PVC")
			return nil, fmt.Errorf("failed to get PVC: %v", err)
		}

		// If no PV is bound yet, return an empty list
		if pvc.Spec.VolumeName == "" {
			log.WithFields(logrus.Fields{
				"namespace": namespace,
				"pvc_name":  pvcName,
			}).Debug(logging.LogTagDetail + " PVC is not bound to a PV yet")
			return nil, nil
		}

		// Get volume attachments
		volumeAttachments, err := k8sClient.StorageV1().VolumeAttachments().List(ctx, metav1.ListOptions{})
		if err != nil {
			log.WithFields(logrus.Fields{
				"namespace": namespace,
				"pvc_name":  pvcName,
				"error":     err,
			}).Error(logging.LogTagError + " Failed to list volume attachments")
			return nil, fmt.Errorf("failed to list volume attachments: %v", err)
		}

		// Find volume attachments for this PVC's PV
		matchFound := false
		for _, va := range volumeAttachments.Items {
			pvName := "nil"
			if va.Spec.Source.PersistentVolumeName != nil {
				pvName = *va.Spec.Source.PersistentVolumeName

				// Check if this attachment matches our PV
				if pvName == pvc.Spec.VolumeName {
					matchFound = true
					log.WithFields(logrus.Fields{
						"namespace":     namespace,
						"pvc_name":      pvcName,
						"pv_name":       pvc.Spec.VolumeName,
						"attachment":    va.Name,
						"attached_node": va.Spec.NodeName,
					}).Info(logging.LogTagDetail + " Found volume attachment for PVC")

					// Add the node if not already in the list
					if !contains(nodes, va.Spec.NodeName) {
						nodes = append(nodes, va.Spec.NodeName)
					}
				}
			}
		}

		if !matchFound {
			log.WithFields(logrus.Fields{
				"namespace":        namespace,
				"pvc_name":         pvcName,
				"pv_name":          pvc.Spec.VolumeName,
				"attachment_count": len(volumeAttachments.Items),
			}).Warn(logging.LogTagWarn + " No matching volume attachments found for this PV")
		}
	}

	log.WithFields(logrus.Fields{
		"namespace":   namespace,
		"pvc_name":    pvcName,
		"nodes_count": len(nodes),
		"nodes":       nodes,
	}).Info(logging.LogTagDetail + " Completed finding nodes for PVC")

	return nodes, nil
}

// ReleasePVCLock releases a lock on the source PVC
func (p *PVCSyncer) ReleasePVCLock(ctx context.Context, namespace, pvcName string) error {
	log.WithFields(logrus.Fields{
		"namespace":          namespace,
		"pvc_name":           pvcName,
		"source_cluster_url": p.SourceConfig.Host,
	}).Info(logging.LogTagDetail + " Releasing lock on PVC using source cluster")

	// Get the source PVC
	pvc, err := p.SourceK8sClient.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
			"error":     err,
		}).Error(logging.LogTagError + " Failed to get source PVC")
		return fmt.Errorf("failed to get source PVC: %v", err)
	}

	// Check if we have the lock
	if pvc.Annotations == nil || pvc.Annotations["dr-syncer.io/lock-owner"] == "" {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
		}).Info(logging.LogTagDetail + " PVC is not locked")
		return nil
	}

	// Get our pod name
	podName := os.Getenv("POD_NAME")
	if podName == "" {
		podName = "unknown"
	}

	// Only release the lock if we own it
	if pvc.Annotations["dr-syncer.io/lock-owner"] != podName {
		log.WithFields(logrus.Fields{
			"namespace":  namespace,
			"pvc_name":   pvcName,
			"lock_owner": pvc.Annotations["dr-syncer.io/lock-owner"],
			"our_pod":    podName,
		}).Warn(logging.LogTagWarn + " PVC is locked by another controller, not releasing")
		return fmt.Errorf("PVC is locked by another controller: %s", pvc.Annotations["dr-syncer.io/lock-owner"])
	}

	// Remove lock annotations
	delete(pvc.Annotations, "dr-syncer.io/lock-owner")
	delete(pvc.Annotations, "dr-syncer.io/lock-timestamp")

	// Update the PVC
	_, err = p.SourceK8sClient.CoreV1().PersistentVolumeClaims(namespace).Update(ctx, pvc, metav1.UpdateOptions{})
	if err != nil {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
			"error":     err,
		}).Error(logging.LogTagError + " Failed to update source PVC to release lock")
		return fmt.Errorf("failed to update source PVC to release lock: %v", err)
	}

	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc_name":  pvcName,
	}).Info(logging.LogTagDetail + " Lock released on PVC")

	return nil
}
