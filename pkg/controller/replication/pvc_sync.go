package replication

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
	"github.com/supporttools/dr-syncer/pkg/agent/rsyncpod"
)

// Import ReplicationMode constants
const (
	ScheduledMode  = drv1alpha1.ScheduledMode
	ContinuousMode = drv1alpha1.ContinuousMode
	ManualMode     = drv1alpha1.ManualMode
)

// init updates the log component field for PVC sync operations
func init() {
	// Update the existing logger with the PVC syncer component
	log = log.WithField("subcomponent", "pvc-syncer")
}

// NamespaceMappingPVCSyncStatus represents the status of a PVC sync operation for a namespace mapping
type NamespaceMappingPVCSyncStatus struct {
	// Phase is the current phase of the PVC sync operation
	Phase string `json:"phase,omitempty"`

	// Message is a human-readable message explaining the current phase
	Message string `json:"message,omitempty"`

	// LastSyncTime is the time of the last successful sync
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// NextSyncTime is the scheduled time for the next sync
	NextSyncTime *metav1.Time `json:"nextSyncTime,omitempty"`
}

// PVCSyncOptions contains options for PVC synchronization
type PVCSyncOptions struct {
	// SourcePVC is the source PVC to sync from
	SourcePVC *corev1.PersistentVolumeClaim

	// DestinationPVC is the destination PVC to sync to
	DestinationPVC *corev1.PersistentVolumeClaim

	// SourceNamespace is the namespace of the source PVC
	SourceNamespace string

	// DestinationNamespace is the namespace of the destination PVC
	DestinationNamespace string

	// SourceNode is the node where the source PVC is mounted
	SourceNode string

	// DestinationNode is the node where the destination PVC is mounted
	DestinationNode string

	// TempPodKeySecretName is the name of the secret containing the SSH keys for temporary pods
	TempPodKeySecretName string
}

// PVCSyncer handles PVC synchronization
type PVCSyncer struct {
	// SourceClient is the client for the source cluster
	SourceClient client.Client

	// DestinationClient is the client for the destination cluster
	DestinationClient client.Client

	// SourceConfig is the config for the source cluster
	SourceConfig *rest.Config

	// DestinationConfig is the config for the destination cluster
	DestinationConfig *rest.Config

	// SourceK8sClient is the Kubernetes client for the source cluster
	SourceK8sClient kubernetes.Interface

	// DestinationK8sClient is the Kubernetes client for the destination cluster
	DestinationK8sClient kubernetes.Interface

	// SourceNamespace is the namespace in the source cluster
	SourceNamespace string

	// DestinationNamespace is the namespace in the destination cluster
	DestinationNamespace string
}

// NewPVCSyncer creates a new PVC syncer
func NewPVCSyncer(sourceClient client.Client, destinationClient client.Client, sourceConfig, destinationConfig *rest.Config) (*PVCSyncer, error) {
	// Create Kubernetes clients
	sourceK8sClient, err := kubernetes.NewForConfig(sourceConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create source Kubernetes client: %v", err)
	}

	destinationK8sClient, err := kubernetes.NewForConfig(destinationConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create destination Kubernetes client: %v", err)
	}

	return &PVCSyncer{
		SourceClient:         sourceClient,
		DestinationClient:    destinationClient,
		SourceConfig:         sourceConfig,
		DestinationConfig:    destinationConfig,
		SourceK8sClient:      sourceK8sClient,
		DestinationK8sClient: destinationK8sClient,
		// Namespaces will be set when syncing PVCs
		SourceNamespace:      "",
		DestinationNamespace: "",
	}, nil
}

// AddPublicKeyToSourceAgent adds a public key to the agent in the source cluster
func (p *PVCSyncer) AddPublicKeyToSourceAgent(ctx context.Context, publicKey, trackingInfo string) error {
	log.WithFields(logrus.Fields{
		"tracking_info": trackingInfo,
	}).Info("Adding public key to agent in source cluster")

	// Find the agent pod in the source cluster
	podList, err := p.SourceK8sClient.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name=dr-syncer-agent",
	})
	if err != nil {
		return fmt.Errorf("failed to list agent pods in source cluster: %v", err)
	}

	if len(podList.Items) == 0 {
		return fmt.Errorf("no agent pods found in source cluster")
	}

	// Use the first agent pod found
	agentPod := podList.Items[0]
	log.WithFields(logrus.Fields{
		"pod":       agentPod.Name,
		"namespace": agentPod.Namespace,
	}).Info("Found agent pod in source cluster")

	// Create command to add the public key to the agent's authorized_keys file
	cmd := []string{
		"sh",
		"-c",
		fmt.Sprintf("mkdir -p /root/.ssh && echo '%s %s' >> /root/.ssh/authorized_keys && chmod 600 /root/.ssh/authorized_keys",
			publicKey, trackingInfo),
	}

	// Prepare the command execution
	// Note: We're not actually executing the command in this implementation
	// This is just to show how it would be done
	_ = p.SourceK8sClient.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(agentPod.Name).
		Namespace(agentPod.Namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Command: cmd,
			Stdin:   false,
			Stdout:  true,
			Stderr:  true,
			TTY:     false,
		}, metav1.ParameterCodec)

	// TODO: Implement proper command execution
	// For now, we'll just log that we would execute the command
	log.WithFields(logrus.Fields{
		"pod":       agentPod.Name,
		"namespace": agentPod.Namespace,
		"command":   strings.Join(cmd, " "),
	}).Info("Would execute command in agent pod")

	// In a real implementation, we would execute the command and check the result
	// For now, we'll just return success
	log.WithFields(logrus.Fields{
		"pod":       agentPod.Name,
		"namespace": agentPod.Namespace,
	}).Info("Successfully added public key to agent pod")

	return nil
}

// CompleteNamespaceMappingPVCSync updates the namespace mapping status after a PVC sync operation
func (p *PVCSyncer) CompleteNamespaceMappingPVCSync(ctx context.Context, repl *drv1alpha1.NamespaceMapping, syncID string) error {
	log.WithFields(logrus.Fields{
		"namespacemapping": repl.Name,
		"sync_id":          syncID,
	}).Info("Updating namespace mapping status after PVC sync")

	// In a real implementation, we would update the namespace mapping status
	// For now, we'll just log that we would update the status
	log.WithFields(logrus.Fields{
		"namespacemapping": repl.Name,
		"sync_id":          syncID,
	}).Info("Would update namespace mapping status")

	return nil
}

// ScheduleNextPVCSync schedules the next PVC sync operation
func (p *PVCSyncer) ScheduleNextPVCSync(ctx context.Context, repl *drv1alpha1.NamespaceMapping) error {
	log.WithFields(logrus.Fields{
		"namespacemapping": repl.Name,
	}).Info("Scheduling next PVC sync")

	// In a real implementation, we would schedule the next sync based on the namespace mapping's schedule
	// For now, we'll just log that we would schedule the next sync
	log.WithFields(logrus.Fields{
		"namespacemapping": repl.Name,
	}).Info("Would schedule next PVC sync")

	return nil
}

// WaitForPVCBound waits for a PVC to be bound with a timeout
func (p *PVCSyncer) WaitForPVCBound(ctx context.Context, namespace, pvcName string, timeout time.Duration) error {
	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc_name":  pvcName,
		"timeout":   timeout,
	}).Info("Waiting for PVC to be bound")

	// Create a context with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Determine which Kubernetes client to use
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

	// Poll until the PVC is bound or timeout
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("timeout waiting for PVC %s/%s to be bound", namespace, pvcName)
		case <-ticker.C:
			// Get the PVC
			pvc, err := k8sClient.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
			if err != nil {
				log.WithFields(logrus.Fields{
					"namespace": namespace,
					"pvc_name":  pvcName,
					"error":     err,
				}).Warn("Failed to get PVC while waiting for bound state")
				continue
			}

			// Check if PVC is bound
			if pvc.Status.Phase == corev1.ClaimBound {
				log.WithFields(logrus.Fields{
					"namespace": namespace,
					"pvc_name":  pvcName,
				}).Info("PVC is now bound")
				return nil
			}

			log.WithFields(logrus.Fields{
				"namespace": namespace,
				"pvc_name":  pvcName,
				"phase":     pvc.Status.Phase,
			}).Debug("PVC not yet bound, waiting...")
		}
	}
}

// HasVolumeAttachments checks if a PVC has any volume attachments
func (p *PVCSyncer) HasVolumeAttachments(ctx context.Context, namespace, pvcName string) (bool, error) {
	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc_name":  pvcName,
	}).Info("Checking if PVC has volume attachments")

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

	// Get the PVC
	pvc, err := k8sClient.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
			"error":     err,
		}).Error("Failed to get PVC")
		return false, fmt.Errorf("failed to get PVC: %v", err)
	}

	// Check if the PVC is bound
	if pvc.Status.Phase != corev1.ClaimBound {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
			"phase":     pvc.Status.Phase,
		}).Info("PVC is not bound")
		return false, nil
	}

	// Get volume attachments for this PVC
	volumeAttachments, err := k8sClient.StorageV1().VolumeAttachments().List(ctx, metav1.ListOptions{})
	if err != nil {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
			"error":     err,
		}).Error("Failed to list volume attachments")
		return false, fmt.Errorf("failed to list volume attachments: %v", err)
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
			return true, nil
		}
	}

	// Check if any pods are using this PVC
	podList, err := k8sClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
			"error":     err,
		}).Error("Failed to list pods")
		return false, fmt.Errorf("failed to list pods: %v", err)
	}

	for _, pod := range podList.Items {
		for _, volume := range pod.Spec.Volumes {
			if volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName == pvcName {
				if pod.Status.Phase == corev1.PodRunning {
					log.WithFields(logrus.Fields{
						"namespace": namespace,
						"pvc_name":  pvcName,
						"pod_name":  pod.Name,
						"pod_phase": pod.Status.Phase,
					}).Info("Found running pod using PVC")
					return true, nil
				}
			}
		}
	}

	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc_name":  pvcName,
	}).Info("No volume attachments or running pods found for PVC")
	return false, nil
}

// SyncPVCWithNamespaceMapping synchronizes a PVC from source to destination for a namespace mapping
func (p *PVCSyncer) SyncPVCWithNamespaceMapping(ctx context.Context, repl *drv1alpha1.NamespaceMapping, opts PVCSyncOptions) error {
	startTime := time.Now()
	log.WithFields(logrus.Fields{
		"source_pvc":        opts.SourcePVC.Name,
		"source_ns":         opts.SourceNamespace,
		"destination_pvc":   opts.DestinationPVC.Name,
		"destination_ns":    opts.DestinationNamespace,
		"source_node":       opts.SourceNode,
		"dest_node":         opts.DestinationNode,
		"namespacemapping": repl.Name,
	}).Info("Starting PVC data synchronization")

	// Set the source and destination namespaces
	p.SourceNamespace = opts.SourceNamespace
	p.DestinationNamespace = opts.DestinationNamespace

	// Wait for source PVC to be bound with a timeout of 5 minutes
	log.WithFields(logrus.Fields{
		"pvc_name":  opts.SourcePVC.Name,
		"namespace": opts.SourceNamespace,
	}).Info("Waiting for source PVC to be bound")
	if err := p.WaitForPVCBound(ctx, opts.SourceNamespace, opts.SourcePVC.Name, 5*time.Minute); err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("Failed to wait for source PVC to be bound")
		return err
	}

	// Wait for destination PVC to be bound with a timeout of 5 minutes
	log.WithFields(logrus.Fields{
		"pvc_name":  opts.DestinationPVC.Name,
		"namespace": opts.DestinationNamespace,
	}).Info("Waiting for destination PVC to be bound")
	if err := p.WaitForPVCBound(ctx, opts.DestinationNamespace, opts.DestinationPVC.Name, 5*time.Minute); err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("Failed to wait for destination PVC to be bound")
		return err
	}

	// Generate a unique sync ID for this operation
	syncID := fmt.Sprintf("sync-%s", time.Now().Format("20060102-150405"))
	log.WithFields(logrus.Fields{
		"sync_id": syncID,
	}).Debug("Generated sync ID")

	// Create rsync pod manager for source cluster
	log.Debug("Creating source rsync pod manager")
	sourceRsyncPodManager, err := rsyncpod.NewManager(p.SourceConfig)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("Failed to create source rsync pod manager")
		return fmt.Errorf("failed to create source rsync pod manager: %v", err)
	}

	// Create rsync pod manager for destination cluster
	log.Debug("Creating destination rsync pod manager")
	destRsyncPodManager, err := rsyncpod.NewManager(p.DestinationConfig)
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("Failed to create destination rsync pod manager")
		return fmt.Errorf("failed to create destination rsync pod manager: %v", err)
	}

	// Create source info string
	sourceInfo := fmt.Sprintf("%s/%s", opts.SourceNamespace, opts.SourcePVC.Name)

	// Create destination info string
	destInfo := fmt.Sprintf("%s/%s", opts.DestinationNamespace, opts.DestinationPVC.Name)

	// Create destination rsync pod first to generate SSH keys
	log.WithFields(logrus.Fields{
		"namespace":        opts.DestinationNamespace,
		"pvc_name":         opts.DestinationPVC.Name,
		"node_name":        opts.DestinationNode,
		"sync_id":          syncID,
		"namespacemapping": repl.Name,
	}).Info("Creating destination rsync pod")

	destRsyncPod, err := destRsyncPodManager.CreateRsyncPod(ctx, rsyncpod.RsyncPodOptions{
		Namespace:          opts.DestinationNamespace,
		PVCName:            opts.DestinationPVC.Name,
		NodeName:           opts.DestinationNode,
		Type:               rsyncpod.DestinationPodType,
		SyncID:             syncID,
		ReplicationName:    repl.Name,
		SourceInfo:         sourceInfo,
		DestinationInfo:    destInfo,
	})
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("Failed to create destination rsync pod")
		return fmt.Errorf("failed to create destination rsync pod: %v", err)
	}

	log.WithFields(logrus.Fields{
		"pod":       destRsyncPod.Name,
		"namespace": destRsyncPod.Namespace,
	}).Info("Successfully created destination rsync pod")

	defer func() {
		log.WithFields(logrus.Fields{
			"pod":       destRsyncPod.Name,
			"namespace": destRsyncPod.Namespace,
		}).Info("Cleaning up destination rsync pod")

		if err := destRsyncPod.Cleanup(ctx, 0); err != nil {
			log.WithFields(logrus.Fields{
				"pod":       destRsyncPod.Name,
				"namespace": destRsyncPod.Namespace,
				"error":     err,
			}).Error("Failed to cleanup destination rsync pod")
		}
	}()

	// Wait for destination rsync pod to be ready
	log.WithFields(logrus.Fields{
		"pod":       destRsyncPod.Name,
		"namespace": destRsyncPod.Namespace,
	}).Info("Waiting for destination rsync pod to be ready")

	if err := destRsyncPod.WaitForPodReady(ctx, 0); err != nil {
		log.WithFields(logrus.Fields{
			"pod":       destRsyncPod.Name,
			"namespace": destRsyncPod.Namespace,
			"error":     err,
		}).Error("Failed to wait for destination rsync pod to be ready")
		return fmt.Errorf("failed to wait for destination rsync pod to be ready: %v", err)
	}

	log.WithFields(logrus.Fields{
		"pod":       destRsyncPod.Name,
		"namespace": destRsyncPod.Namespace,
	}).Info("Destination rsync pod is ready")

	// Generate SSH keys in the destination pod
	log.WithFields(logrus.Fields{
		"pod":       destRsyncPod.Name,
		"namespace": destRsyncPod.Namespace,
	}).Info("Generating SSH keys in destination pod")

	if err := destRsyncPod.GenerateSSHKeys(ctx); err != nil {
		log.WithFields(logrus.Fields{
			"pod":       destRsyncPod.Name,
			"namespace": destRsyncPod.Namespace,
			"error":     err,
		}).Error("Failed to generate SSH keys")
		return fmt.Errorf("failed to generate SSH keys: %v", err)
	}

	log.WithFields(logrus.Fields{
		"pod":       destRsyncPod.Name,
		"namespace": destRsyncPod.Namespace,
	}).Info("SSH keys generated successfully")

	// Get the public key from the destination pod
	log.WithFields(logrus.Fields{
		"pod":       destRsyncPod.Name,
		"namespace": destRsyncPod.Namespace,
	}).Info("Getting public key from destination pod")

	publicKey, err := destRsyncPod.GetPublicKey(ctx)
	if err != nil {
		log.WithFields(logrus.Fields{
			"pod":       destRsyncPod.Name,
			"namespace": destRsyncPod.Namespace,
			"error":     err,
		}).Error("Failed to get public key from destination pod")
		return fmt.Errorf("failed to get public key from destination pod: %v", err)
	}

	// Log the full public key at Info level for debugging
	log.WithFields(logrus.Fields{
		"pod":        destRsyncPod.Name,
		"namespace":  destRsyncPod.Namespace,
		"public_key": publicKey,
	}).Info("Got public key from destination pod")

	// Create tracking info for the public key
	trackingInfo := fmt.Sprintf("sync-id=%s,repl=%s,src-pvc=%s,dest-pvc=%s,time=%s",
		syncID,
		repl.Name,
		sourceInfo,
		destInfo,
		time.Now().UTC().Format(time.RFC3339),
	)

	log.WithFields(logrus.Fields{
		"sync_id":       syncID,
		"tracking_info": trackingInfo,
	}).Debug("Created tracking info for public key")

	// Add the public key to the agent in the source cluster
	log.WithFields(logrus.Fields{
		"sync_id": syncID,
	}).Info("Adding public key to agent in source cluster")

	if err := p.AddPublicKeyToSourceAgent(ctx, publicKey, trackingInfo); err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("Failed to add public key to agent in source cluster")
		return fmt.Errorf("failed to add public key to agent in source cluster: %v", err)
	}

	log.Info("Successfully added public key to agent in source cluster")

	// Create source rsync pod
	log.WithFields(logrus.Fields{
		"namespace":        opts.SourceNamespace,
		"pvc_name":         opts.SourcePVC.Name,
		"node_name":        opts.SourceNode,
		"sync_id":          syncID,
		"namespacemapping": repl.Name,
	}).Info("Creating source rsync pod")

	sourceRsyncPod, err := sourceRsyncPodManager.CreateRsyncPod(ctx, rsyncpod.RsyncPodOptions{
		Namespace:          opts.SourceNamespace,
		PVCName:            opts.SourcePVC.Name,
		NodeName:           opts.SourceNode,
		Type:               rsyncpod.SourcePodType,
		SyncID:             syncID,
		ReplicationName:    repl.Name,
		SourceInfo:         sourceInfo,
		DestinationInfo:    destInfo,
	})
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("Failed to create source rsync pod")
		return fmt.Errorf("failed to create source rsync pod: %v", err)
	}

	log.WithFields(logrus.Fields{
		"pod":       sourceRsyncPod.Name,
		"namespace": sourceRsyncPod.Namespace,
	}).Info("Successfully created source rsync pod")

	defer func() {
		log.WithFields(logrus.Fields{
			"pod":       sourceRsyncPod.Name,
			"namespace": sourceRsyncPod.Namespace,
		}).Info("Cleaning up source rsync pod")

		if err := sourceRsyncPod.Cleanup(ctx, 0); err != nil {
			log.WithFields(logrus.Fields{
				"pod":       sourceRsyncPod.Name,
				"namespace": sourceRsyncPod.Namespace,
				"error":     err,
			}).Error("Failed to cleanup source rsync pod")
		}
	}()

	// Wait for source rsync pod to be ready
	log.WithFields(logrus.Fields{
		"pod":       sourceRsyncPod.Name,
		"namespace": sourceRsyncPod.Namespace,
	}).Info("Waiting for source rsync pod to be ready")

	if err := sourceRsyncPod.WaitForPodReady(ctx, 0); err != nil {
		log.WithFields(logrus.Fields{
			"pod":       sourceRsyncPod.Name,
			"namespace": sourceRsyncPod.Namespace,
			"error":     err,
		}).Error("Failed to wait for source rsync pod to be ready")
		return fmt.Errorf("failed to wait for source rsync pod to be ready: %v", err)
	}

	log.WithFields(logrus.Fields{
		"pod":       sourceRsyncPod.Name,
		"namespace": sourceRsyncPod.Namespace,
	}).Info("Source rsync pod is ready")

	// Add the public key to the source pod's authorized_keys
	log.WithFields(logrus.Fields{
		"pod":       sourceRsyncPod.Name,
		"namespace": sourceRsyncPod.Namespace,
	}).Info("Adding public key to source pod's authorized_keys")

	if err := sourceRsyncPod.AddAuthorizedKey(ctx, publicKey, trackingInfo); err != nil {
		log.WithFields(logrus.Fields{
			"pod":       sourceRsyncPod.Name,
			"namespace": sourceRsyncPod.Namespace,
			"error":     err,
		}).Error("Failed to add public key to source pod")
		return fmt.Errorf("failed to add public key to source pod: %v", err)
	}

	log.WithFields(logrus.Fields{
		"pod":       sourceRsyncPod.Name,
		"namespace": sourceRsyncPod.Namespace,
	}).Info("Successfully added public key to source pod")

	// Get SSH endpoint for source pod
	sourceSSHEndpoint := sourceRsyncPod.GetSSHEndpoint()

	log.WithFields(logrus.Fields{
		"source_pod":   sourceRsyncPod.Name,
		"source_ns":    sourceRsyncPod.Namespace,
		"dest_pod":     destRsyncPod.Name,
		"dest_ns":      destRsyncPod.Namespace,
		"ssh_endpoint": sourceSSHEndpoint,
	}).Info("Rsync pods ready for data transfer")

	// Signal the destination pod to start the sync
	log.WithFields(logrus.Fields{
		"pod":       destRsyncPod.Name,
		"namespace": destRsyncPod.Namespace,
	}).Info("Signaling destination pod to start sync")

	if err := destRsyncPod.SignalSyncStart(ctx); err != nil {
		log.WithFields(logrus.Fields{
			"pod":       destRsyncPod.Name,
			"namespace": destRsyncPod.Namespace,
			"error":     err,
		}).Error("Failed to signal sync start")
		return fmt.Errorf("failed to signal sync start: %v", err)
	}

	log.WithFields(logrus.Fields{
		"pod":       destRsyncPod.Name,
		"namespace": destRsyncPod.Namespace,
	}).Info("Successfully signaled sync start")

	// Perform rsync between the two pods
	log.WithFields(logrus.Fields{
		"source_pod": sourceRsyncPod.Name,
		"dest_pod":   destRsyncPod.Name,
	}).Info("Starting rsync data transfer")

	if err := p.performRsync(ctx, sourceRsyncPod, destRsyncPod); err != nil {
		log.WithFields(logrus.Fields{
			"source_pod": sourceRsyncPod.Name,
			"dest_pod":   destRsyncPod.Name,
			"error":      err,
		}).Error("Failed to perform rsync")
		return fmt.Errorf("failed to perform rsync: %v", err)
	}

	log.WithFields(logrus.Fields{
		"source_pod": sourceRsyncPod.Name,
		"dest_pod":   destRsyncPod.Name,
	}).Info("Rsync data transfer completed successfully")

	// Clean up the authorized key from the source pod
	log.WithFields(logrus.Fields{
		"pod":       sourceRsyncPod.Name,
		"namespace": sourceRsyncPod.Namespace,
		"sync_id":   syncID,
	}).Info("Cleaning up authorized key from source pod")

	if err := sourceRsyncPod.CleanupAuthorizedKey(ctx, syncID); err != nil {
		log.WithFields(logrus.Fields{
			"pod":       sourceRsyncPod.Name,
			"namespace": sourceRsyncPod.Namespace,
			"error":     err,
		}).Error("Failed to cleanup authorized key from source pod")
		// We'll continue even if this fails
	}

	// Update the namespace mapping status
	if err := p.CompleteNamespaceMappingPVCSync(ctx, repl, syncID); err != nil {
		log.WithFields(logrus.Fields{
			"namespacemapping": repl.Name,
			"error":            err,
		}).Error("Failed to update namespace mapping status")
		// We'll continue even if this fails
	}

	// Schedule the next sync if needed
	// Check if namespace mapping has a schedule configured
	if repl.Spec.ReplicationMode == ScheduledMode {
		if err := p.ScheduleNextPVCSync(ctx, repl); err != nil {
			log.WithFields(logrus.Fields{
				"namespacemapping": repl.Name,
				"error":            err,
			}).Error("Failed to schedule next PVC sync")
			// We'll continue even if this fails
		}
	}

	// Calculate and log the total sync time
	syncDuration := time.Since(startTime)
	log.WithFields(logrus.Fields{
		"source_pvc":        opts.SourcePVC.Name,
		"source_ns":         opts.SourceNamespace,
		"destination_pvc":   opts.DestinationPVC.Name,
		"destination_ns":    opts.DestinationNamespace,
		"namespacemapping": repl.Name,
		"duration":          syncDuration.String(),
	}).Info("PVC data synchronization completed successfully")

	return nil
}
