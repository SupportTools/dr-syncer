package sync

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
)

// matchNodeLabels checks if target node has matching labels from source node
func matchNodeLabels(sourceLabels, targetLabels map[string]string) bool {
	// Labels to match for node affinity
	matchLabels := []string{
		"kubernetes.io/arch",
		"kubernetes.io/os",
		"kubernetes.io/hostname",
		"topology.kubernetes.io/zone",
		"topology.kubernetes.io/region",
		"node.kubernetes.io/instance-type",
	}

	for _, label := range matchLabels {
		sourceVal, sourceExists := sourceLabels[label]
		targetVal, targetExists := targetLabels[label]
		if sourceExists && targetExists && sourceVal == targetVal {
			return true
		}
	}

	return false
}

// containsAccessMode checks if a list of access modes contains a specific mode
func containsAccessMode(modes []corev1.PersistentVolumeAccessMode, mode corev1.PersistentVolumeAccessMode) bool {
	for _, m := range modes {
		if m == mode {
			return true
		}
	}
	return false
}

// SyncCoordinator manages PVC sync operations between clusters
type SyncCoordinator struct {
	sourceClient client.Client
	targetClient client.Client
	pvcManager   *PVCManager
	concurrency  int
	retryConfig  *drv1alpha1.PVCSyncRetryConfig
}

// NewSyncCoordinator creates a new sync coordinator
func NewSyncCoordinator(sourceClient, targetClient client.Client, concurrency int, retryConfig *drv1alpha1.PVCSyncRetryConfig) *SyncCoordinator {
	return &SyncCoordinator{
		sourceClient: sourceClient,
		targetClient: targetClient,
		pvcManager:   NewPVCManager(sourceClient),
		concurrency:  concurrency,
		retryConfig:  retryConfig,
	}
}

// SyncPVCs syncs PVCs from source to target cluster
func (c *SyncCoordinator) SyncPVCs(ctx context.Context) error {
	logger := log.FromContext(ctx)

	// Get nodes from source cluster
	sourceNodes := &corev1.NodeList{}
	if err := c.sourceClient.List(ctx, sourceNodes); err != nil {
		return fmt.Errorf("failed to list source nodes: %v", err)
	}

	// Get nodes from target cluster
	targetNodes := &corev1.NodeList{}
	if err := c.targetClient.List(ctx, targetNodes); err != nil {
		return fmt.Errorf("failed to list target nodes: %v", err)
	}

	if len(targetNodes.Items) == 0 {
		return fmt.Errorf("no nodes found in target cluster")
	}

	// Discover PVCs on each source node
	var allPVCs []PVCInfo
	for _, node := range sourceNodes.Items {
		pvcs, err := c.pvcManager.DiscoverPVCs(ctx, node.Name)
		if err != nil {
			logger.Error(err, "Failed to discover PVCs", "node", node.Name)
			continue
		}
		allPVCs = append(allPVCs, pvcs...)
	}

	// Create work queue
	queue := make(chan PVCInfo, len(allPVCs))
	for _, pvc := range allPVCs {
		queue <- pvc
	}
	close(queue)

	// Create wait group for workers
	var wg sync.WaitGroup
	errors := make(chan error, len(allPVCs))

	// Start workers
	for i := 0; i < c.concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for pvc := range queue {
				// Get source node's labels
				sourceNode := &corev1.Node{}
				if err := c.sourceClient.Get(ctx, types.NamespacedName{Name: pvc.Node}, sourceNode); err != nil {
					errors <- fmt.Errorf("failed to get source node %s: %v", pvc.Node, err)
					continue
				}

				// Find matching target node based on labels
				var targetNode *corev1.Node
				for _, node := range targetNodes.Items {
					if matchNodeLabels(sourceNode.Labels, node.Labels) {
						targetNode = &node
						break
					}
				}

				// If no matching node found, use first available node
				if targetNode == nil {
					if len(targetNodes.Items) == 0 {
						errors <- fmt.Errorf("no target nodes available for PVC %s/%s", pvc.Namespace, pvc.Name)
						continue
					}
					targetNode = &targetNodes.Items[0]
					logger.Info("No matching target node found, using first available node",
						"pvc", pvc.Name,
						"namespace", pvc.Namespace,
						"sourceNode", pvc.Node,
						"targetNode", targetNode.Name)
				}

				// Get source PVC size
				sourceSize := resource.MustParse(pvc.Capacity)

				// Create target PVC if it doesn't exist
				targetPVC := &corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      pvc.Name,
						Namespace: pvc.Namespace,
						Labels: map[string]string{
							"dr-syncer.io/managed-by": "dr-syncer",
							"dr-syncer.io/source-pvc": pvc.Name,
							"dr-syncer.io/source-ns":  pvc.Namespace,
						},
						Annotations: map[string]string{
							"dr-syncer.io/source-size": pvc.Capacity,
						},
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: pvc.AccessModes,
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: sourceSize,
							},
						},
						StorageClassName: &pvc.StorageClass,
					},
				}
				if err := c.targetClient.Create(ctx, targetPVC); err != nil {
					if !k8serrors.IsAlreadyExists(err) {
						errors <- fmt.Errorf("failed to create target PVC %s/%s: %v", pvc.Namespace, pvc.Name, err)
						continue
					}
				}

				// Check if PVC already exists
				existingPVC := &corev1.PersistentVolumeClaim{}
				err := c.targetClient.Get(ctx, types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace}, existingPVC)
				if err == nil {
					// Check if size needs to be increased
					existingSize := existingPVC.Spec.Resources.Requests[corev1.ResourceStorage]
					if sourceSize.Cmp(existingSize) > 0 {
						existingPVC.Spec.Resources.Requests[corev1.ResourceStorage] = sourceSize
						if err := c.targetClient.Update(ctx, existingPVC); err != nil {
							logger.Error(err, "Failed to update PVC size",
								"pvc", pvc.Name,
								"namespace", pvc.Namespace,
								"currentSize", existingSize.String(),
								"newSize", sourceSize.String())
						}
					}
				} else if !k8serrors.IsNotFound(err) {
					errors <- fmt.Errorf("failed to check existing PVC: %v", err)
					continue
				} else {
					// Create new PVC
					if err := c.targetClient.Create(ctx, targetPVC); err != nil {
						errors <- fmt.Errorf("failed to create target PVC %s/%s: %v", pvc.Namespace, pvc.Name, err)
						continue
					}
				}

				// For ReadWriteMany PVCs, we can sync to any node
				var nodeName string
				if containsAccessMode(pvc.AccessModes, corev1.ReadWriteMany) {
					// Use any available node
					nodeName = targetNodes.Items[0].Name
				} else {
					nodeName = targetNode.Name
				}

				// Create sync pod in target cluster
				syncPod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("sync-%s", pvc.Name),
						Namespace: pvc.Namespace,
						Labels: map[string]string{
							"dr-syncer.io/managed-by": "dr-syncer",
							"dr-syncer.io/sync-pod":   "true",
							"dr-syncer.io/source-pvc": pvc.Name,
							"dr-syncer.io/source-ns":  pvc.Namespace,
						},
					},
					Spec: corev1.PodSpec{
						NodeName: nodeName,
						Containers: []corev1.Container{
							{
								Name:  "sync",
								Image: "busybox",
								Command: []string{
									"sleep",
									"infinity",
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
										ClaimName: pvc.Name,
									},
								},
							},
						},
					},
				}

				if err := c.targetClient.Create(ctx, syncPod); err != nil {
					if !k8serrors.IsAlreadyExists(err) {
						errors <- fmt.Errorf("failed to create sync pod for PVC %s/%s: %v", pvc.Namespace, pvc.Name, err)
						continue
					}
				}

				// Wait for sync pod to be ready
				if err := wait.PollImmediate(5*time.Second, 2*time.Minute, func() (bool, error) {
					pod := &corev1.Pod{}
					if err := c.targetClient.Get(ctx, types.NamespacedName{Name: syncPod.Name, Namespace: syncPod.Namespace}, pod); err != nil {
						return false, err
					}
					return pod.Status.Phase == corev1.PodRunning, nil
				}); err != nil {
					errors <- fmt.Errorf("failed waiting for sync pod %s/%s to be ready: %v", syncPod.Namespace, syncPod.Name, err)
					continue
				}

				// Get target pod's volume path
				targetPath := filepath.Join("/data")

				// Sync with retries
				if err := c.syncWithRetry(ctx, pvc, targetNode.Name, targetPath); err != nil {
					errors <- fmt.Errorf("failed to sync PVC %s/%s: %v", pvc.Namespace, pvc.Name, err)
				}

				// Delete sync pod with cleanup
				if err := c.cleanupSyncResources(ctx, syncPod); err != nil {
					logger.Error(err, "Failed to cleanup sync resources", "pod", syncPod.Name, "namespace", syncPod.Namespace)
				}
			}
		}(i)
	}

	// Wait for all workers to finish
	wg.Wait()
	close(errors)

	// Collect errors
	var syncErrors []error
	for err := range errors {
		syncErrors = append(syncErrors, err)
	}

	if len(syncErrors) > 0 {
		return fmt.Errorf("encountered %d sync errors: %v", len(syncErrors), syncErrors[0])
	}

	return nil
}

// cleanupSyncResources handles cleanup of sync pod and related resources
func (c *SyncCoordinator) cleanupSyncResources(ctx context.Context, pod *corev1.Pod) error {
	logger := log.FromContext(ctx)

	// Delete sync pod
	if err := c.targetClient.Delete(ctx, pod); err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete sync pod: %v", err)
	}

	// Wait for pod deletion
	if err := wait.PollImmediate(2*time.Second, 30*time.Second, func() (bool, error) {
		if err := c.targetClient.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, &corev1.Pod{}); err != nil {
			if k8serrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	}); err != nil {
		logger.Error(err, "Failed waiting for sync pod deletion", "pod", pod.Name, "namespace", pod.Namespace)
	}

	// Check if any other sync pods are using the PVC
	podList := &corev1.PodList{}
	if err := c.targetClient.List(ctx, podList, client.MatchingLabels{
		"dr-syncer.io/sync-pod":   "true",
		"dr-syncer.io/source-pvc": pod.Labels["dr-syncer.io/source-pvc"],
		"dr-syncer.io/source-ns":  pod.Labels["dr-syncer.io/source-ns"],
	}); err != nil {
		return fmt.Errorf("failed to list sync pods: %v", err)
	}

	// If no other sync pods are using the PVC, we can clean it up
	if len(podList.Items) == 0 {
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pod.Labels["dr-syncer.io/source-pvc"],
				Namespace: pod.Labels["dr-syncer.io/source-ns"],
			},
		}
		if err := c.targetClient.Delete(ctx, pvc); err != nil && !k8serrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete PVC: %v", err)
		}
	}

	return nil
}

// syncWithRetry attempts to sync a PVC with retries based on configuration
func (c *SyncCoordinator) syncWithRetry(ctx context.Context, pvc PVCInfo, targetNode, targetPath string) error {
	logger := log.FromContext(ctx)
	maxRetries := int(c.retryConfig.MaxRetries)
	initialDelay, _ := time.ParseDuration(c.retryConfig.InitialDelay)
	maxDelay, _ := time.ParseDuration(c.retryConfig.MaxDelay)
	delay := initialDelay

	for attempt := 0; attempt <= maxRetries; attempt++ {
		err := c.pvcManager.SyncPVC(ctx, pvc, targetNode, targetPath)
		if err == nil {
			return nil
		}

		if attempt == maxRetries {
			return fmt.Errorf("max retries reached: %v", err)
		}

		logger.Error(err, "Sync attempt failed",
			"pvc", pvc.Name,
			"namespace", pvc.Namespace,
			"attempt", attempt+1,
			"maxRetries", maxRetries)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
			// Exponential backoff with max delay
			delay = time.Duration(float64(delay) * 2)
			if delay > maxDelay {
				delay = maxDelay
			}
		}
	}

	return nil
}
