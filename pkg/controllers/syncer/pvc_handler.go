package syncer

import (
	"context"
	"fmt"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
	controller "github.com/supporttools/dr-syncer/pkg/controller/replication"
	syncerrors "github.com/supporttools/dr-syncer/pkg/controllers/syncer/errors"
	"github.com/supporttools/dr-syncer/pkg/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Use the exported PVCClusterKey from replication package
var (
	// Explicitly import and use the PVCClusterKey from the replication package
	pvcClusterKey = controller.PVCClusterKey
)

// BackupPVCSyncResult holds the outcome of syncing a single PVC via backup.
type BackupPVCSyncResult struct {
	PVCName    string
	SnapshotID string
	Err        error
}

// BackupPVCSyncFunc is a function that performs backup-based PVC data sync.
// It is injected by the caller to avoid import cycles between syncer and backup packages.
type BackupPVCSyncFunc func(ctx context.Context, mapping *drv1alpha1.NamespaceMapping,
	pvcs []corev1.PersistentVolumeClaim, backupConfig *drv1alpha1.BackupConfig) []BackupPVCSyncResult

// isBackupPathEnabled returns true if the PVC config has backup-based sync enabled.
func isBackupPathEnabled(pvcConfig *drv1alpha1.PVCConfig) bool {
	return pvcConfig != nil &&
		pvcConfig.DataSyncConfig != nil &&
		pvcConfig.DataSyncConfig.BackupConfig != nil &&
		pvcConfig.DataSyncConfig.BackupConfig.Enabled
}

// routePVCSync routes PVC synchronization to either the Kopia backup path or the
// existing rsync path based on PVCConfig.DataSyncConfig.BackupConfig.Enabled.
// When BackupConfig is nil or Enabled is false, the rsync path is used (unchanged default).
func routePVCSync(ctx context.Context, syncer *ResourceSyncer, sourceClient, destClient kubernetes.Interface,
	srcNamespace, dstNamespace string, pvcConfig *drv1alpha1.PVCConfig, config *drv1alpha1.ImmutableResourceConfig,
	mapping *drv1alpha1.NamespaceMapping, backupSyncFunc BackupPVCSyncFunc) error {

	if isBackupPathEnabled(pvcConfig) {
		if backupSyncFunc == nil {
			return syncerrors.NewNonRetryableError(
				fmt.Errorf("backup sync path enabled but no backup sync function provided"),
				"PVCBackupSync",
			)
		}
		log.Info(fmt.Sprintf("PVC sync path: backup (Kopia) for %s -> %s", srcNamespace, dstNamespace))
		return syncPVCsViaBackup(ctx, syncer, sourceClient, srcNamespace, dstNamespace, pvcConfig, mapping, backupSyncFunc)
	}

	log.Info(fmt.Sprintf("PVC sync path: rsync for %s -> %s", srcNamespace, dstNamespace))
	return syncPersistentVolumeClaimsWithMounting(ctx, syncer, sourceClient, destClient, srcNamespace, dstNamespace, pvcConfig, config)
}

// syncPVCsViaBackup handles PVC synchronization using the Kopia backup/restore path.
// It lists source PVCs, creates/updates destination PVC resources, then delegates data
// sync to the provided backupSyncFunc.
func syncPVCsViaBackup(ctx context.Context, syncer *ResourceSyncer, sourceClient kubernetes.Interface,
	srcNamespace, dstNamespace string, pvcConfig *drv1alpha1.PVCConfig,
	mapping *drv1alpha1.NamespaceMapping, backupSyncFunc BackupPVCSyncFunc) error {

	log.Info(fmt.Sprintf("Syncing PVCs via backup path from %s to %s", srcNamespace, dstNamespace))

	// List source PVCs
	pvcs, err := sourceClient.CoreV1().PersistentVolumeClaims(srcNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return syncerrors.NewRetryableError(
			fmt.Errorf("failed to list PVCs: %w", err),
			"PersistentVolumeClaims",
		)
	}

	// Create/update PVC resources in destination using shared helpers
	var pvcList []corev1.PersistentVolumeClaim
	for _, pvc := range pvcs.Items {
		if utils.ShouldIgnoreResource(&pvc) {
			continue
		}

		destPVC := pvc.DeepCopy()
		destPVC.Namespace = dstNamespace

		applyStorageClassMapping(destPVC, pvcConfig)
		applyAccessModeMapping(destPVC, pvcConfig)

		resultPVC, err := createOrUpdateDestPVC(ctx, syncer.destClient, destPVC, dstNamespace, pvcConfig)
		if err != nil {
			return err
		}
		pvcList = append(pvcList, *resultPVC)
	}

	// If data sync is not enabled or no PVCs to sync, we're done
	if pvcConfig == nil || !pvcConfig.SyncData || len(pvcList) == 0 {
		log.Info(fmt.Sprintf("Backup PVC resource sync complete (%d PVCs), data sync not needed", len(pvcList)))
		return nil
	}

	// Delegate data sync to the injected backup sync function
	backupConfig := pvcConfig.DataSyncConfig.BackupConfig
	results := backupSyncFunc(ctx, mapping, pvcList, backupConfig)

	// Check results for failures
	var failCount int
	for _, r := range results {
		if r.Err != nil {
			failCount++
			log.Errorf("Backup sync failed for PVC %s: %v", r.PVCName, r.Err)
		}
	}

	if failCount > 0 && failCount == len(results) {
		return syncerrors.NewRetryableError(
			fmt.Errorf("all %d PVC backup syncs failed", failCount),
			"PVCBackupSync",
		)
	}

	if failCount > 0 {
		log.Errorf("Backup sync partial failure: %d of %d PVCs failed", failCount, len(results))
	}

	log.Info(fmt.Sprintf("Backup PVC sync complete: %d succeeded, %d failed", len(results)-failCount, failCount))
	return nil
}

// syncPersistentVolumeClaimsWithMounting synchronizes PVCs between namespaces
// This uses the rsync deployment to handle direct mounting and data transfer
func syncPersistentVolumeClaimsWithMounting(ctx context.Context, syncer *ResourceSyncer, sourceClient, targetClient kubernetes.Interface,
	srcNamespace, dstNamespace string, pvcConfig *drv1alpha1.PVCConfig, config *drv1alpha1.ImmutableResourceConfig) error {

	log.Info(fmt.Sprintf("Syncing persistent volume claims from %s to %s", srcNamespace, dstNamespace))

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

	// Process each PVC using shared helpers
	for _, pvc := range pvcs.Items {
		if utils.ShouldIgnoreResource(&pvc) {
			continue
		}

		destPVC := pvc.DeepCopy()
		destPVC.Namespace = dstNamespace

		applyStorageClassMapping(destPVC, pvcConfig)
		applyAccessModeMapping(destPVC, pvcConfig)

		resultPVC, err := createOrUpdateDestPVC(ctx, targetClient, destPVC, dstNamespace, pvcConfig)
		if err != nil {
			return err
		}
		syncedPVCs = append(syncedPVCs, *resultPVC)
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

			// Skip explicit PVC mounting as the rsync deployment will directly mount the destination PVC
			// and we'll use the existing mount of the source PVC
			log.Info(fmt.Sprintf("Skipping explicit mount for PVCs - rsync deployment will handle mounting for %s", sourcePVC.Name))

			// Find nodes where PVCs are mounted - this will now succeed because we've mounted them
			log.Info(fmt.Sprintf("Finding node for source PVC %s/%s", srcNamespace, sourcePVC.Name))
			fmt.Println("## FindPVCNode - Source PVC")

			// Create a modified context with the correct configuration for source cluster
			srcCtx := context.WithValue(ctx, pvcClusterKey, "source")
			sourceNode, err := pvcSyncer.FindPVCNode(srcCtx, pvcSyncer.SourceClient, srcNamespace, sourcePVC.Name)
			if err != nil {
				log.Errorf("Failed to find node for source PVC %s/%s: %v", srcNamespace, sourcePVC.Name, err)
				continue
			}

			log.Info(fmt.Sprintf("Finding node for destination PVC %s/%s", dstNamespace, destPVC.Name))
			fmt.Println("## FindPVCNode - Destination PVC")

			// Create a modified context with the correct configuration for destination cluster
			destCtx := context.WithValue(ctx, pvcClusterKey, "destination")
			destNode, err := pvcSyncer.FindPVCNode(destCtx, pvcSyncer.DestinationClient, dstNamespace, destPVC.Name)
			if err != nil {
				// For destination PVC, not finding a node is expected and shouldn't be treated as an error
				// The rsync deployment will handle mounting the PVC
				log.Info(fmt.Sprintf("No existing node found for destination PVC %s/%s - will be mounted by rsync deployment",
					dstNamespace, destPVC.Name))
				destNode = "" // Empty string indicates no node preference
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

			// Acquire global concurrency slot before syncing
			gcm := controller.GetGlobalConcurrencyManager()
			if gcm != nil {
				if err := gcm.Acquire(ctx, srcNamespace, sourcePVC.Name); err != nil {
					log.Errorf("Failed to acquire concurrency slot for PVC %s/%s: %v", srcNamespace, sourcePVC.Name, err)
					continue
				}
			}

			// Perform the actual data synchronization using rsync deployment
			syncErr := pvcSyncer.SyncPVCWithNamespaceMapping(ctx, dummyMapping, syncOpts)

			// Release concurrency slot after sync completes
			if gcm != nil {
				gcm.Release(srcNamespace, sourcePVC.Name)
			}

			if syncErr != nil {
				log.Errorf("Failed to sync data for PVC %s: %v", destPVC.Name, syncErr)
			} else {
				log.Info(fmt.Sprintf("Successfully synced data for PVC %s", destPVC.Name))
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
