package sync

import (
	"context"
	"fmt"
	"reflect"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
	"github.com/supporttools/dr-syncer/pkg/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// StorageClassLabel is used to override the storage class for a specific PVC
	StorageClassLabel = "dr-syncer.io/storage-class"
	// AccessModeLabel is used to override the access mode for a specific PVC
	AccessModeLabel = "dr-syncer.io/access-mode"
	// SyncPVLabel is used to force PV sync for a specific PVC
	SyncPVLabel = "dr-syncer.io/sync-pv"
)

// getStorageClassMapping returns the destination storage class name based on mappings
func getStorageClassMapping(srcStorageClass string, mappings []drv1alpha1.StorageClassMapping) string {
	for _, mapping := range mappings {
		if mapping.From == srcStorageClass {
			return mapping.To
		}
	}
	return srcStorageClass
}

// getAccessModeMapping returns the destination access mode based on mappings
func getAccessModeMapping(srcAccessMode corev1.PersistentVolumeAccessMode, mappings []drv1alpha1.AccessModeMapping) corev1.PersistentVolumeAccessMode {
	srcMode := string(srcAccessMode)
	for _, mapping := range mappings {
		if mapping.From == srcMode {
			return corev1.PersistentVolumeAccessMode(mapping.To)
		}
	}
	return srcAccessMode
}

// syncPersistentVolumeClaims synchronizes PVCs between source and destination namespaces
func syncPersistentVolumeClaims(ctx context.Context, sourceClient, destClient kubernetes.Interface, srcNamespace, dstNamespace string, pvcConfig *drv1alpha1.PVCConfig) error {
	pvcs, err := sourceClient.CoreV1().PersistentVolumeClaims(srcNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list PVCs: %v", err)
	}

	for _, pvc := range pvcs.Items {
		if utils.ShouldIgnoreResource(&pvc) {
			continue
		}

		// Create a copy of the PVC for destination
		destPVC := pvc.DeepCopy()
		destPVC.Namespace = dstNamespace
		utils.SanitizeMetadata(destPVC)

		// Handle storage class mapping
		srcStorageClass := pvc.Spec.StorageClassName
		if srcStorageClass != nil {
			if override, exists := pvc.Labels[StorageClassLabel]; exists {
				destPVC.Spec.StorageClassName = &override
			} else if pvcConfig != nil && len(pvcConfig.StorageClassMappings) > 0 {
				mappedClass := getStorageClassMapping(*srcStorageClass, pvcConfig.StorageClassMappings)
				destPVC.Spec.StorageClassName = &mappedClass
			}
		}

		// Handle access mode mapping
		if len(pvc.Spec.AccessModes) > 0 {
			if override, exists := pvc.Labels[AccessModeLabel]; exists {
				destPVC.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.PersistentVolumeAccessMode(override)}
			} else if pvcConfig != nil && len(pvcConfig.AccessModeMappings) > 0 {
				mappedModes := make([]corev1.PersistentVolumeAccessMode, len(pvc.Spec.AccessModes))
				for i, mode := range pvc.Spec.AccessModes {
					mappedModes[i] = getAccessModeMapping(mode, pvcConfig.AccessModeMappings)
				}
				destPVC.Spec.AccessModes = mappedModes
			}
		}

		// Handle volume attributes and PV syncing
		syncPV := false
		if val, exists := pvc.Labels[SyncPVLabel]; exists {
			syncPV = val == "true"
		} else if pvcConfig != nil {
			syncPV = pvcConfig.SyncPersistentVolumes
		}

		if !syncPV {
			destPVC.Spec.VolumeName = ""

			// Always clear volume attributes for now since we don't have PreserveVolumeAttributes yet
			destPVC.Spec.VolumeMode = nil
			destPVC.Spec.Selector = nil
			destPVC.Spec.DataSource = nil
			destPVC.Spec.DataSourceRef = nil
		}

		// Create or update PVC in destination
		existing, err := destClient.CoreV1().PersistentVolumeClaims(dstNamespace).Get(ctx, destPVC.Name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				_, err = destClient.CoreV1().PersistentVolumeClaims(dstNamespace).Create(ctx, destPVC, metav1.CreateOptions{})
				if err != nil {
					log.WithError(err).Error(fmt.Sprintf("failed to create PVC %s", destPVC.Name))
					continue
				}
				log.Info(fmt.Sprintf("created PVC %s", destPVC.Name))
			} else {
				log.WithError(err).Error(fmt.Sprintf("failed to get PVC %s", destPVC.Name))
				continue
			}
		} else {
			// Compare specs
			if !reflect.DeepEqual(destPVC.Spec, existing.Spec) {
				destPVC.ResourceVersion = existing.ResourceVersion
				_, err = destClient.CoreV1().PersistentVolumeClaims(dstNamespace).Update(ctx, destPVC, metav1.UpdateOptions{})
				if err != nil {
					log.WithError(err).Error(fmt.Sprintf("failed to update PVC %s", destPVC.Name))
					continue
				}
				log.Info(fmt.Sprintf("updated PVC %s", destPVC.Name))
			}
		}

		// Handle PV sync if enabled
		if syncPV && pvc.Spec.VolumeName != "" {
			if err := syncPersistentVolume(ctx, sourceClient, destClient, pvc.Spec.VolumeName); err != nil {
				log.WithError(err).Error(fmt.Sprintf("failed to sync PV %s", pvc.Spec.VolumeName))
			}
		}
	}

	return nil
}

// syncPersistentVolume synchronizes a single PV between clusters
func syncPersistentVolume(ctx context.Context, sourceClient, destClient kubernetes.Interface, pvName string) error {
	// Get PV from source cluster
	pv, err := sourceClient.CoreV1().PersistentVolumes().Get(ctx, pvName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get PV %s: %v", pvName, err)
	}

	// Create a copy of the PV for destination
	destPV := pv.DeepCopy()
	utils.SanitizeMetadata(destPV)

	// Create or update PV in destination
	existing, err := destClient.CoreV1().PersistentVolumes().Get(ctx, destPV.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			_, err = destClient.CoreV1().PersistentVolumes().Create(ctx, destPV, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create PV %s: %v", destPV.Name, err)
			}
			log.Info(fmt.Sprintf("created PV %s", destPV.Name))
		} else {
			return fmt.Errorf("failed to get PV %s: %v", destPV.Name, err)
		}
	} else {
		if !reflect.DeepEqual(destPV.Spec, existing.Spec) {
			destPV.ResourceVersion = existing.ResourceVersion
			_, err = destClient.CoreV1().PersistentVolumes().Update(ctx, destPV, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Errorf("failed to update PV %s: %v", destPV.Name, err)
			}
			log.Info(fmt.Sprintf("updated PV %s", destPV.Name))
		}
	}

	return nil
}
