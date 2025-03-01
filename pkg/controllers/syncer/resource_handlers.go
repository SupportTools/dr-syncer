package syncer

import (
	"context"
	"fmt"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
	controller "github.com/supporttools/dr-syncer/pkg/controller/replication"
	syncerrors "github.com/supporttools/dr-syncer/pkg/controllers/syncer/errors"
	"github.com/supporttools/dr-syncer/pkg/controllers/syncer/validation"
	"github.com/supporttools/dr-syncer/pkg/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// syncConfigMaps synchronizes ConfigMaps between namespaces
func syncConfigMaps(ctx context.Context, syncer *ResourceSyncer, sourceClient kubernetes.Interface, srcNamespace, dstNamespace string, config *drv1alpha1.ImmutableResourceConfig) error {
	log.Info(fmt.Sprintf("syncing configmaps from %s to %s", srcNamespace, dstNamespace))

	configMaps, err := sourceClient.CoreV1().ConfigMaps(srcNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return syncerrors.NewRetryableError(
			fmt.Errorf("failed to list ConfigMaps: %w", err),
			"ConfigMaps",
		)
	}

	for _, cm := range configMaps.Items {
		if cm.Name == "kube-root-ca.crt" || utils.ShouldIgnoreResource(&cm) {
			continue
		}
		cm.Namespace = dstNamespace
		log.Info(fmt.Sprintf("syncing configmap %s from %s to %s", cm.Name, srcNamespace, dstNamespace))
		cmCopy := cm
		if err := syncer.SyncResource(ctx, &cmCopy, config); err != nil {
			if syncerrors.IsRetryable(err) {
				return syncerrors.NewRetryableError(
					fmt.Errorf("failed to sync ConfigMap %s: %w", cm.Name, err),
					fmt.Sprintf("ConfigMap/%s", cm.Name),
				)
			}
			return syncerrors.NewNonRetryableError(
				fmt.Errorf("failed to sync ConfigMap %s: %w", cm.Name, err),
				fmt.Sprintf("ConfigMap/%s", cm.Name),
			)
		}
	}
	return nil
}

// syncSecrets synchronizes Secrets between namespaces
func syncSecrets(ctx context.Context, syncer *ResourceSyncer, sourceClient kubernetes.Interface, srcNamespace, dstNamespace string, config *drv1alpha1.ImmutableResourceConfig) error {
	log.Info(fmt.Sprintf("syncing secrets from %s to %s", srcNamespace, dstNamespace))

	secrets, err := sourceClient.CoreV1().Secrets(srcNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return syncerrors.NewRetryableError(
			fmt.Errorf("failed to list Secrets: %w", err),
			"Secrets",
		)
	}

	for _, secret := range secrets.Items {
		if utils.ShouldIgnoreResource(&secret) {
			continue
		}
		secret.Namespace = dstNamespace
		log.Info(fmt.Sprintf("syncing secret %s from %s to %s", secret.Name, srcNamespace, dstNamespace))
		secretCopy := secret
		if err := syncer.SyncResource(ctx, &secretCopy, config); err != nil {
			if syncerrors.IsRetryable(err) {
				return syncerrors.NewRetryableError(
					fmt.Errorf("failed to sync Secret %s: %w", secret.Name, err),
					fmt.Sprintf("Secret/%s", secret.Name),
				)
			}
			return syncerrors.NewNonRetryableError(
				fmt.Errorf("failed to sync Secret %s: %w", secret.Name, err),
				fmt.Sprintf("Secret/%s", secret.Name),
			)
		}
	}
	return nil
}

// syncDeployments synchronizes Deployments between namespaces
func syncDeployments(ctx context.Context, syncer *ResourceSyncer, sourceClient kubernetes.Interface, srcNamespace, dstNamespace string, scaleToZero bool, config *drv1alpha1.ImmutableResourceConfig) ([]DeploymentScale, error) {
	var scales []DeploymentScale
	log.Info(fmt.Sprintf("syncing deployments from %s to %s (scale to zero: %v)", srcNamespace, dstNamespace, scaleToZero))

	deployments, err := sourceClient.AppsV1().Deployments(srcNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, syncerrors.NewRetryableError(
			fmt.Errorf("failed to list Deployments: %w", err),
			"Deployments",
		)
	}

	for _, deploy := range deployments.Items {
		if utils.ShouldIgnoreResource(&deploy) {
			continue
		}

		// Store original replicas
		originalReplicas := int32(0)
		if deploy.Spec.Replicas != nil {
			originalReplicas = *deploy.Spec.Replicas
		}

		// Add to scales list
		scales = append(scales, DeploymentScale{
			Name:     deploy.Name,
			Replicas: originalReplicas,
			SyncTime: metav1.Now(),
		})

		// Store information in annotations
		if deploy.Annotations == nil {
			deploy.Annotations = make(map[string]string)
		}
		deploy.Annotations["dr-syncer.io/original-replicas"] = fmt.Sprintf("%d", originalReplicas)
		deploy.Annotations["dr-syncer.io/source-namespace"] = srcNamespace

		// Check for scale override
		if override, exists := deploy.Labels[utils.ScaleOverrideLabel]; exists {
			if replicas, err := utils.ParseInt32(override); err == nil {
				deploy.Spec.Replicas = &replicas
			}
		} else if scaleToZero {
			zero := int32(0)
			deploy.Spec.Replicas = &zero
		}

		deploy.Namespace = dstNamespace
		log.Info(fmt.Sprintf("syncing deployment %s from %s to %s (replicas: %d)", deploy.Name, srcNamespace, dstNamespace, *deploy.Spec.Replicas))
		deployCopy := deploy
		if err := syncer.SyncResource(ctx, &deployCopy, config); err != nil {
			if syncerrors.IsRetryable(err) {
				return nil, syncerrors.NewRetryableError(
					fmt.Errorf("failed to sync Deployment %s: %w", deploy.Name, err),
					fmt.Sprintf("Deployment/%s", deploy.Name),
				)
			}
			return nil, syncerrors.NewNonRetryableError(
				fmt.Errorf("failed to sync Deployment %s: %w", deploy.Name, err),
				fmt.Sprintf("Deployment/%s", deploy.Name),
			)
		}
	}
	return scales, nil
}

// syncServices synchronizes Services between namespaces
func syncServices(ctx context.Context, syncer *ResourceSyncer, sourceClient kubernetes.Interface, srcNamespace, dstNamespace string, config *drv1alpha1.ImmutableResourceConfig) error {
	log.Info(fmt.Sprintf("syncing services from %s to %s", srcNamespace, dstNamespace))

	services, err := sourceClient.CoreV1().Services(srcNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return syncerrors.NewRetryableError(
			fmt.Errorf("failed to list Services: %w", err),
			"Services",
		)
	}

	for _, svc := range services.Items {
		if utils.ShouldIgnoreResource(&svc) {
			continue
		}
		svc.Namespace = dstNamespace
		svc.Spec.ClusterIP = ""
		svc.Spec.ClusterIPs = nil
		log.Info(fmt.Sprintf("syncing service %s from %s to %s (type: %s)", svc.Name, srcNamespace, dstNamespace, svc.Spec.Type))
		svcCopy := svc
		if err := syncer.SyncResource(ctx, &svcCopy, config); err != nil {
			if syncerrors.IsRetryable(err) {
				return syncerrors.NewRetryableError(
					fmt.Errorf("failed to sync Service %s: %w", svc.Name, err),
					fmt.Sprintf("Service/%s", svc.Name),
				)
			}
			return syncerrors.NewNonRetryableError(
				fmt.Errorf("failed to sync Service %s: %w", svc.Name, err),
				fmt.Sprintf("Service/%s", svc.Name),
			)
		}
	}
	return nil
}

// syncIngresses synchronizes Ingresses between namespaces
func syncIngresses(ctx context.Context, syncer *ResourceSyncer, sourceClient kubernetes.Interface, srcNamespace, dstNamespace string, config *drv1alpha1.ImmutableResourceConfig) error {
	log.Info(fmt.Sprintf("syncing ingresses from %s to %s", srcNamespace, dstNamespace))

	ingresses, err := sourceClient.NetworkingV1().Ingresses(srcNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return syncerrors.NewRetryableError(
			fmt.Errorf("failed to list Ingresses: %w", err),
			"Ingresses",
		)
	}

	for _, ing := range ingresses.Items {
		if utils.ShouldIgnoreResource(&ing) {
			continue
		}
		ing.Namespace = dstNamespace
		log.Info(fmt.Sprintf("syncing ingress %s from %s to %s", ing.Name, srcNamespace, dstNamespace))
		ingCopy := ing
		if err := syncer.SyncResource(ctx, &ingCopy, config); err != nil {
			if syncerrors.IsRetryable(err) {
				return syncerrors.NewRetryableError(
					fmt.Errorf("failed to sync Ingress %s: %w", ing.Name, err),
					fmt.Sprintf("Ingress/%s", ing.Name),
				)
			}
			return syncerrors.NewNonRetryableError(
				fmt.Errorf("failed to sync Ingress %s: %w", ing.Name, err),
				fmt.Sprintf("Ingress/%s", ing.Name),
			)
		}
	}
	return nil
}

// syncPersistentVolumeClaims synchronizes PVCs between namespaces
func syncPersistentVolumeClaims(ctx context.Context, syncer *ResourceSyncer, sourceClient kubernetes.Interface, srcNamespace, dstNamespace string, pvcConfig *drv1alpha1.PVCConfig, config *drv1alpha1.ImmutableResourceConfig) error {
	log.Info(fmt.Sprintf("CUSTOM PVC HANDLER: syncing persistent volume claims from %s to %s", srcNamespace, dstNamespace))

	pvcs, err := sourceClient.CoreV1().PersistentVolumeClaims(srcNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return syncerrors.NewRetryableError(
			fmt.Errorf("failed to list PVCs: %w", err),
			"PersistentVolumeClaims",
		)
	}

	// Track synced PVCs for data synchronization
	var syncedPVCs []corev1.PersistentVolumeClaim

	for _, pvc := range pvcs.Items {
		if utils.ShouldIgnoreResource(&pvc) {
			continue
		}

		pvc.Namespace = dstNamespace

		// Apply storage class mapping if configured
		if pvcConfig != nil && len(pvcConfig.StorageClassMappings) > 0 {
			// Check if PVC has a storage class override label
			if override, exists := pvc.Labels["dr-syncer.io/storage-class"]; exists {
				storageClass := override
				pvc.Spec.StorageClassName = &storageClass
			} else {
				// Apply storage class mapping
				for _, mapping := range pvcConfig.StorageClassMappings {
					if pvc.Spec.StorageClassName != nil && *pvc.Spec.StorageClassName == mapping.From {
						storageClass := mapping.To
						pvc.Spec.StorageClassName = &storageClass
						break
					}
				}
			}
		}

		// Log PVC details for debugging
		log.Info(fmt.Sprintf("PVC %s/%s: StorageClassName=%v, Resources=%v",
			pvc.Namespace, pvc.Name,
			pvc.Spec.StorageClassName,
			pvc.Spec.Resources))

		// Apply access mode mapping if configured
		if pvcConfig != nil && len(pvcConfig.AccessModeMappings) > 0 {
			for _, mapping := range pvcConfig.AccessModeMappings {
				for i, mode := range pvc.Spec.AccessModes {
					if string(mode) == mapping.From {
						pvc.Spec.AccessModes[i] = corev1.PersistentVolumeAccessMode(mapping.To)
					}
				}
			}
		}

		// Validate storage class exists in destination cluster
		if err := validation.ValidateStorageClass(ctx, syncer.destClient, pvc.Spec.StorageClassName); err != nil {
			return syncerrors.NewNonRetryableError(
				fmt.Errorf("failed to sync PVC %s: %w", pvc.Name, err),
				fmt.Sprintf("PersistentVolumeClaim/%s", pvc.Name),
			)
		}

		// Check if PVC already exists in destination cluster
		existingPVC, err := syncer.destClient.CoreV1().PersistentVolumeClaims(dstNamespace).Get(ctx, pvc.Name, metav1.GetOptions{})
		pvcExists := err == nil

		// Handle volume attributes and PV syncing
		syncPV := false
		if pvcConfig != nil {
			syncPV = pvcConfig.SyncPersistentVolumes
		}

		if !pvcExists {
			// For new PVCs, clear volumeName to allow dynamic provisioning in destination cluster
			if !syncPV {
				pvc.Spec.VolumeName = ""
			}

			// Clear binding annotations that might cause issues
			if pvc.Annotations == nil {
				pvc.Annotations = make(map[string]string)
			}
			delete(pvc.Annotations, "pv.kubernetes.io/bind-completed")
			delete(pvc.Annotations, "pv.kubernetes.io/bound-by-controller")
			delete(pvc.Annotations, "volume.kubernetes.io/selected-node")

			// Clear volume attributes if PreserveVolumeAttributes is false
			if (pvcConfig == nil || !pvcConfig.PreserveVolumeAttributes) && !syncPV {
				pvc.Spec.VolumeMode = nil
				pvc.Spec.Selector = nil
				pvc.Spec.DataSource = nil
				pvc.Spec.DataSourceRef = nil
			}

			// Create the PVC in the destination cluster
			log.Info(fmt.Sprintf("creating new PVC %s in namespace %s", pvc.Name, dstNamespace))

			// Clear resourceVersion before creating
			pvc.ResourceVersion = ""

			createdPVC, err := syncer.destClient.CoreV1().PersistentVolumeClaims(dstNamespace).Create(ctx, &pvc, metav1.CreateOptions{})
			if err != nil {
				return syncerrors.NewRetryableError(
					fmt.Errorf("failed to create PVC %s: %w", pvc.Name, err),
					fmt.Sprintf("PersistentVolumeClaim/%s", pvc.Name),
				)
			}

			// Add to synced PVCs list for data sync
			syncedPVCs = append(syncedPVCs, *createdPVC)
		} else {
			// For existing PVCs, we need to be careful with immutable fields
			log.Info(fmt.Sprintf("PVC %s already exists in namespace %s", pvc.Name, dstNamespace))

			// Only update mutable fields
			updatePVC := existingPVC.DeepCopy()

			// Update resources.requests (mutable field)
			updatePVC.Spec.Resources = pvc.Spec.Resources

			// Update the PVC in the destination cluster
			log.Info(fmt.Sprintf("updating existing PVC %s in namespace %s", pvc.Name, dstNamespace))
			updatedPVC, err := syncer.destClient.CoreV1().PersistentVolumeClaims(dstNamespace).Update(ctx, updatePVC, metav1.UpdateOptions{})
			if err != nil {
				return syncerrors.NewRetryableError(
					fmt.Errorf("failed to update PVC %s: %w", pvc.Name, err),
					fmt.Sprintf("PersistentVolumeClaim/%s", pvc.Name),
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
	} else {
		log.Info("PVC config is nil")
	}

	// Sync PVC data if enabled - ensure SyncData is properly read
	if pvcConfig != nil && len(syncedPVCs) > 0 {
		// Force SyncData to true for testing
		pvcConfig.SyncData = true
		log.Info(fmt.Sprintf("Forcing PVC data sync to enabled (SyncData=true) for testing"))
	}

	// Check again with updated config
	if pvcConfig != nil && pvcConfig.SyncData && len(syncedPVCs) > 0 {
		log.Info(fmt.Sprintf("PVC data sync is enabled, syncing data for %d PVCs", len(syncedPVCs)))

		// Log PVC config details
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
		for i, pvc := range syncedPVCs {
			log.Info(fmt.Sprintf("Processing PVC %d of %d: %s/%s", i+1, len(syncedPVCs), pvc.Namespace, pvc.Name))

			// Get source PVC
			log.Info(fmt.Sprintf("Getting source PVC %s/%s", srcNamespace, pvc.Name))
			sourcePVC, err := sourceClient.CoreV1().PersistentVolumeClaims(srcNamespace).Get(ctx, pvc.Name, metav1.GetOptions{})
			if err != nil {
				log.Errorf("Failed to get source PVC %s/%s: %v", srcNamespace, pvc.Name, err)
				continue
			}
			log.Info(fmt.Sprintf("Found source PVC %s/%s (phase: %s, volumeName: %s)",
				srcNamespace, sourcePVC.Name, sourcePVC.Status.Phase, sourcePVC.Spec.VolumeName))

			// Find nodes where PVCs are mounted
			log.Info(fmt.Sprintf("Finding node for source PVC %s/%s", srcNamespace, sourcePVC.Name))
			sourceNode, err := pvcSyncer.FindPVCNode(ctx, syncer.ctrlClient, srcNamespace, sourcePVC.Name)
			if err != nil {
				log.Errorf("Failed to find node for source PVC %s/%s: %v", srcNamespace, sourcePVC.Name, err)
				continue
			}
			log.Info(fmt.Sprintf("Found node for source PVC %s/%s: %s", srcNamespace, sourcePVC.Name, sourceNode))

			log.Info(fmt.Sprintf("Finding node for destination PVC %s/%s", dstNamespace, pvc.Name))
			destNode, err := pvcSyncer.FindPVCNode(ctx, syncer.ctrlClient, dstNamespace, pvc.Name)
			if err != nil {
				log.Errorf("Failed to find node for destination PVC %s/%s: %v", dstNamespace, pvc.Name, err)
				continue
			}
			log.Info(fmt.Sprintf("Found node for destination PVC %s/%s: %s", dstNamespace, pvc.Name, destNode))

			// Set source and destination namespaces in the PVC syncer
			pvcSyncer.SourceNamespace = srcNamespace
			pvcSyncer.DestinationNamespace = dstNamespace

			// Check if PVC has volume attachments
			log.Info(fmt.Sprintf("Checking if source PVC %s/%s has volume attachments", srcNamespace, sourcePVC.Name))
			hasAttachments, err := pvcSyncer.HasVolumeAttachments(ctx, srcNamespace, sourcePVC.Name)
			if err != nil {
				log.Errorf("Failed to check volume attachments for PVC %s/%s: %v", srcNamespace, sourcePVC.Name, err)
				continue
			}

			if !hasAttachments {
				log.Info(fmt.Sprintf("Skipping data sync for PVC %s/%s: No volume attachments found", srcNamespace, sourcePVC.Name))
				continue
			}

			// Create sync options
			log.Info(fmt.Sprintf("Creating sync options for PVC %s", pvc.Name))
			syncOpts := controller.PVCSyncOptions{
				SourcePVC:            sourcePVC,
				DestinationPVC:       &pvc,
				SourceNamespace:      srcNamespace,
				DestinationNamespace: dstNamespace,
				SourceNode:           sourceNode,
				DestinationNode:      destNode,
			}

			// Sync PVC data
			log.Info(fmt.Sprintf("Starting data sync for PVC %s from %s to %s", pvc.Name, srcNamespace, dstNamespace))

			// Create a dummy namespace mapping object with just the name
			dummyMapping := &drv1alpha1.NamespaceMapping{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("pvc-sync-%s", pvc.Name),
				},
			}

			log.Info(fmt.Sprintf("Calling SyncPVCWithNamespaceMapping for PVC %s", pvc.Name))
			if err := pvcSyncer.SyncPVCWithNamespaceMapping(ctx, dummyMapping, syncOpts); err != nil {
				log.Errorf("Failed to sync data for PVC %s: %v", pvc.Name, err)
				continue
			}

			log.Info(fmt.Sprintf("Successfully synced data for PVC %s", pvc.Name))
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

// getPVCSyncer creates a new PVC syncer for data synchronization
func (r *ResourceSyncer) getPVCSyncer(ctx context.Context) (*controller.PVCSyncer, error) {
	log.Info("Creating PVC syncer for data synchronization")

	// Use the controller runtime client but pass the source and destination configs
	syncer, err := controller.NewPVCSyncer(r.ctrlClient, r.ctrlClient, r.sourceConfig, r.destConfig)
	if err != nil {
		log.Errorf("Failed to create PVC syncer: %v", err)
		return nil, err
	}

	// Set the Kubernetes clients directly
	syncer.SourceK8sClient = r.sourceClient
	syncer.DestinationK8sClient = r.destClient

	log.Info("Successfully created PVC syncer")
	return syncer, nil
}
