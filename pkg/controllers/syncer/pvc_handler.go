package syncer

import (
	"context"
	"fmt"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
	controller "github.com/supporttools/dr-syncer/pkg/controller/replication"
	syncerrors "github.com/supporttools/dr-syncer/pkg/controllers/syncer/errors"
	"github.com/supporttools/dr-syncer/pkg/controllers/utils"
	"github.com/supporttools/dr-syncer/pkg/pvcmounter"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// syncPersistentVolumeClaimsWithMounting synchronizes PVCs between namespaces and ensures they're mounted
// This is an enhanced version of syncPersistentVolumeClaims that uses the pvcmounter package
func syncPersistentVolumeClaimsWithMounting(ctx context.Context, syncer *ResourceSyncer, sourceClient, targetClient kubernetes.Interface, 
	srcNamespace, dstNamespace string, pvcConfig *drv1alpha1.PVCConfig, config *drv1alpha1.ImmutableResourceConfig) error {
	
	log.Info(fmt.Sprintf("Syncing persistent volume claims from %s to %s with mount support", srcNamespace, dstNamespace))

	// Initialize PVC mount manager
	mountManager := NewPVCMountManager(sourceClient, targetClient)
	
	// Get PVCs from source namespace
	pvcs, err := sourceClient.CoreV1().PersistentVolumeClaims(srcNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return syncerrors.NewRetryableError(
			fmt.Errorf("failed to list PVCs: %w", err),
			"PersistentVolumeClaims",
		)
	}

	// Track synced PVCs for data synchronization
	var syncedPVCs []corev1.PersistentVolumeClaim

	// Process each PVC
	for _, pvc := range pvcs.Items {
		if utils.ShouldIgnoreResource(&pvc) {
			continue
		}

		// Copy the PVC for the destination namespace
		destPVC := pvc.DeepCopy()
		destPVC.Namespace = dstNamespace

		// Apply storage class mapping if configured
		if pvcConfig != nil && len(pvcConfig.StorageClassMappings) > 0 {
			// Check if PVC has a storage class override label
			if override, exists := destPVC.Labels["dr-syncer.io/storage-class"]; exists {
				storageClass := override
				destPVC.Spec.StorageClassName = &storageClass
			} else {
				// Apply storage class mapping
				for _, mapping := range pvcConfig.StorageClassMappings {
					if destPVC.Spec.StorageClassName != nil && *destPVC.Spec.StorageClassName == mapping.From {
						storageClass := mapping.To
						destPVC.Spec.StorageClassName = &storageClass
						break
					}
				}
			}
		}

		// Apply access mode mapping if configured
		if pvcConfig != nil && len(pvcConfig.AccessModeMappings) > 0 {
			for _, mapping := range pvcConfig.AccessModeMappings {
				for i, mode := range destPVC.Spec.AccessModes {
					if string(mode) == mapping.From {
						destPVC.Spec.AccessModes[i] = corev1.PersistentVolumeAccessMode(mapping.To)
					}
				}
			}
		}

		// Handle volume attributes and PV syncing
		syncPV := false
		if pvcConfig != nil {
			syncPV = pvcConfig.SyncPersistentVolumes
		}

		// Check if PVC already exists in destination cluster
		existingPVC, err := targetClient.CoreV1().PersistentVolumeClaims(dstNamespace).Get(ctx, destPVC.Name, metav1.GetOptions{})
		pvcExists := err == nil

		if !pvcExists {
			// For new PVCs, clear volumeName to allow dynamic provisioning in destination cluster
			if !syncPV {
				destPVC.Spec.VolumeName = ""
			}

			// Clear binding annotations that might cause issues
			if destPVC.Annotations == nil {
				destPVC.Annotations = make(map[string]string)
			}
			delete(destPVC.Annotations, "pv.kubernetes.io/bind-completed")
			delete(destPVC.Annotations, "pv.kubernetes.io/bound-by-controller")
			delete(destPVC.Annotations, "volume.kubernetes.io/selected-node")

			// Clear volume attributes if PreserveVolumeAttributes is false
			if (pvcConfig == nil || !pvcConfig.PreserveVolumeAttributes) && !syncPV {
				destPVC.Spec.VolumeMode = nil
				destPVC.Spec.Selector = nil
				destPVC.Spec.DataSource = nil
				destPVC.Spec.DataSourceRef = nil
			}

			// Create the PVC in the destination cluster
			log.Info(fmt.Sprintf("Creating new PVC %s in namespace %s", destPVC.Name, dstNamespace))

			// Clear resourceVersion before creating
			destPVC.ResourceVersion = ""

			createdPVC, err := targetClient.CoreV1().PersistentVolumeClaims(dstNamespace).Create(ctx, destPVC, metav1.CreateOptions{})
			if err != nil {
				return syncerrors.NewRetryableError(
					fmt.Errorf("failed to create PVC %s: %w", destPVC.Name, err),
					fmt.Sprintf("PersistentVolumeClaim/%s", destPVC.Name),
				)
			}

			// Add to synced PVCs list for data sync
			syncedPVCs = append(syncedPVCs, *createdPVC)
		} else {
			// For existing PVCs, we need to be careful with immutable fields
			log.Info(fmt.Sprintf("PVC %s already exists in namespace %s", destPVC.Name, dstNamespace))

			// Only update mutable fields
			updatePVC := existingPVC.DeepCopy()

			// Update resources.requests (mutable field)
			updatePVC.Spec.Resources = destPVC.Spec.Resources

			// Update the PVC in the destination cluster
			log.Info(fmt.Sprintf("Updating existing PVC %s in namespace %s", destPVC.Name, dstNamespace))
			updatedPVC, err := targetClient.CoreV1().PersistentVolumeClaims(dstNamespace).Update(ctx, updatePVC, metav1.CreateOptions{})
			if err != nil {
				return syncerrors.NewRetryableError(
					fmt.Errorf("failed to update PVC %s: %w", destPVC.Name, err),
					fmt.Sprintf("PersistentVolumeClaim/%s", destPVC.Name),
				)
			}

			// Add to synced PVCs list for data sync
			syncedPVCs = append(syncedPVCs, *updatedPVC)
		}
	}

	// Log PVC config details for debugging
	if pvcConfig != nil {
		log.Info(fmt.Sprintf("PVC config: SyncData=%v, SyncPersistentVolumes=%v, PreserveVolumeAttributes=%v, StorageClassMappings=%d, AccessModeMappings=%d",
			pvcConfig.SyncData,
			pvcConfig.SyncPersistentVolumes,
			pvcConfig.PreserveVolumeAttributes,
			len(pvcConfig.StorageClassMappings),
			len(pvcConfig.AccessModeMappings)))

		if pvcConfig.DataSyncConfig != nil {
			log.Info(fmt.Sprintf("PVC data sync config: ConcurrentSyncs=%v, ExcludePaths=%v, RsyncOptions=%v",
				pvcConfig.DataSyncConfig.ConcurrentSyncs,
				pvcConfig.DataSyncConfig.ExcludePaths,
				pvcConfig.DataSyncConfig.RsyncOptions))
		}
	}

	// Sync PVC data if enabled
	if pvcConfig != nil && pvcConfig.SyncData && len(syncedPVCs) > 0 {
		log.Info(fmt.Sprintf("PVC data sync is enabled, syncing data for %d PVCs", len(syncedPVCs)))

		// Import the PVC sync package
		log.Info("Creating PVC syncer for data synchronization")
		pvcSyncer, err := syncer.getPVCSyncer(ctx)
		if err != nil {
			log.Errorf("Failed to create PVC syncer: %v", err)
			return syncerrors.NewRetryableError(
				fmt.Errorf("failed to create PVC syncer: %w", err),
				"PVCDataSync",
			)
		}
		log.Info("Successfully created PVC syncer")

		// Sync data for each PVC
		for i, destPVC := range syncedPVCs {
			log.Info(fmt.Sprintf("Processing PVC %d of %d: %s/%s", i+1, len(syncedPVCs), destPVC.Namespace, destPVC.Name))

			// Get source PVC
			sourcePVC, err := sourceClient.CoreV1().PersistentVolumeClaims(srcNamespace).Get(ctx, destPVC.Name, metav1.GetOptions{})
			if err != nil {
				log.Errorf("Failed to get source PVC %s/%s: %v", srcNamespace, destPVC.Name, err)
				continue
			}
			log.Info(fmt.Sprintf("Found source PVC %s/%s (phase: %s, volumeName: %s)",
				srcNamespace, sourcePVC.Name, sourcePVC.Status.Phase, sourcePVC.Spec.VolumeName))

			// Ensure both source and target PVCs are mounted
			log.Info(fmt.Sprintf("Ensuring PVCs are mounted for %s", sourcePVC.Name))
			err = mountManager.EnsurePVCsMounted(ctx, sourcePVC, &destPVC)
			if err != nil {
				log.Errorf("Failed to mount PVCs for %s: %v", sourcePVC.Name, err)
				continue
			}

			// Find nodes where PVCs are mounted - this will now succeed because we've mounted them
			log.Info(fmt.Sprintf("Finding node for source PVC %s/%s", srcNamespace, sourcePVC.Name))
			sourceNode, err := pvcSyncer.FindPVCNode(ctx, syncer.ctrlClient, srcNamespace, sourcePVC.Name)
			if err != nil {
				log.Errorf("Failed to find node for source PVC %s/%s: %v", srcNamespace, sourcePVC.Name, err)
				continue
			}

			log.Info(fmt.Sprintf("Finding node for destination PVC %s/%s", dstNamespace, destPVC.Name))
			destNode, err := pvcSyncer.FindPVCNode(ctx, syncer.ctrlClient, dstNamespace, destPVC.Name)
			if err != nil {
				log.Errorf("Failed to find node for destination PVC %s/%s: %v", dstNamespace, destPVC.Name, err)
				continue
			}

			// Set source and destination namespaces in the PVC syncer
			pvcSyncer.SourceNamespace = srcNamespace
			pvcSyncer.DestinationNamespace = dstNamespace

			// Create sync options
			syncOpts := controller.PVCSyncOptions{
				SourcePVC:            sourcePVC,
				DestinationPVC:       &destPVC,
				SourceNamespace:      srcNamespace,
				DestinationNamespace: dstNamespace,
				SourceNode:           sourceNode,
				DestinationNode:      destNode,
			}

			// Sync PVC data
			log.Info(fmt.Sprintf("Starting data sync for PVC %s from %s to %s", destPVC.Name, srcNamespace, dstNamespace))

			// Create a dummy namespace mapping object with just the name
			dummyMapping := &drv1alpha1.NamespaceMapping{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("pvc-sync-%s", destPVC.Name),
				},
			}

			// Perform the actual data synchronization
			if err := pvcSyncer.SyncPVCWithNamespaceMapping(ctx, dummyMapping, syncOpts); err != nil {
				log.Errorf("Failed to sync data for PVC %s: %v", destPVC.Name, err)
			} else {
				log.Info(fmt.Sprintf("Successfully synced data for PVC %s", destPVC.Name))
			}

			// Cleanup mount pods after sync
			log.Info(fmt.Sprintf("Cleaning up mount pods for PVC %s", destPVC.Name))
			if err := mountManager.CleanupMountPods(ctx, sourcePVC, &destPVC); err != nil {
				log.Warnf("Failed to cleanup mount pods for PVC %s: %v", destPVC.Name, err)
				// Continue anyway, this is just cleanup
			}
		}
	} else {
		if pvcConfig == nil {
			log.Info("PVC data sync is disabled: pvcConfig is nil")
		} else if !pvcConfig.SyncData {
			log.Info("PVC data sync is disabled: SyncData is false")
		} else if len(syncedPVCs) == 0 {
			log.Info("PVC data sync is enabled but no PVCs to sync")
		}
	}

	return nil
}

// createMountPodConfig creates configuration for mount pods
func createMountPodConfig(pvc *corev1.PersistentVolumeClaim, isSource bool) *pvcmounter.MountPodConfig {
	role := "target"
	if isSource {
		role = "source"
	}

	return &pvcmounter.MountPodConfig{
		PodNamePrefix: fmt.Sprintf("dr-syncer-%s-mount", role),
		Labels: map[string]string{
			"app.kubernetes.io/managed-by": "dr-syncer",
			"app.kubernetes.io/name":       fmt.Sprintf("dr-%s-mount-pod", role),
			"app.kubernetes.io/part-of":    "dr-syncer",
			"dr-syncer.io/role":            fmt.Sprintf("%s-mount", role),
			"dr-syncer.io/pvc":             pvc.Name,
		},
		Annotations: map[string]string{
			"dr-syncer.io/purpose": "pvc-mount",
			"dr-syncer.io/pvc":     pvc.Name,
		},
	}
}
