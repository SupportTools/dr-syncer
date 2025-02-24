package sync

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
)

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
				// Select target node (round-robin)
				targetNode := targetNodes.Items[workerID%len(targetNodes.Items)]

				// Build target path
				targetPath := filepath.Join(kubeletPath, "pvc-sync", pvc.Namespace, pvc.Name)

				// Sync with retries
				if err := c.syncWithRetry(ctx, pvc, targetNode.Name, targetPath); err != nil {
					errors <- fmt.Errorf("failed to sync PVC %s/%s: %v", pvc.Namespace, pvc.Name, err)
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
