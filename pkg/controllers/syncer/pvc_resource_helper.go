package syncer

import (
	"context"
	"fmt"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
	syncerrors "github.com/supporttools/dr-syncer/pkg/controllers/syncer/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// applyStorageClassMapping applies storage class overrides and mappings to a PVC.
// It first checks for a label-based override (dr-syncer.io/storage-class), then falls
// back to the StorageClassMappings from PVCConfig.
func applyStorageClassMapping(pvc *corev1.PersistentVolumeClaim, pvcConfig *drv1alpha1.PVCConfig) {
	if pvcConfig == nil || len(pvcConfig.StorageClassMappings) == 0 {
		return
	}

	if override, exists := pvc.Labels["dr-syncer.io/storage-class"]; exists {
		storageClass := override
		pvc.Spec.StorageClassName = &storageClass
		return
	}

	for _, m := range pvcConfig.StorageClassMappings {
		if pvc.Spec.StorageClassName != nil && *pvc.Spec.StorageClassName == m.From {
			storageClass := m.To
			pvc.Spec.StorageClassName = &storageClass
			break
		}
	}
}

// applyAccessModeMapping applies access mode mappings to a PVC.
func applyAccessModeMapping(pvc *corev1.PersistentVolumeClaim, pvcConfig *drv1alpha1.PVCConfig) {
	if pvcConfig == nil || len(pvcConfig.AccessModeMappings) == 0 {
		return
	}

	for _, m := range pvcConfig.AccessModeMappings {
		for i, mode := range pvc.Spec.AccessModes {
			if string(mode) == m.From {
				pvc.Spec.AccessModes[i] = corev1.PersistentVolumeAccessMode(m.To)
			}
		}
	}
}

// preparePVCForCreation clears fields that prevent a PVC from being created in the
// destination cluster: binding annotations, resourceVersion, and optionally volume
// attributes and volumeName.
func preparePVCForCreation(pvc *corev1.PersistentVolumeClaim, pvcConfig *drv1alpha1.PVCConfig) {
	syncPV := pvcConfig != nil && pvcConfig.SyncPersistentVolumes
	if !syncPV {
		pvc.Spec.VolumeName = ""
	}

	if pvc.Annotations == nil {
		pvc.Annotations = make(map[string]string)
	}
	delete(pvc.Annotations, "pv.kubernetes.io/bind-completed")
	delete(pvc.Annotations, "pv.kubernetes.io/bound-by-controller")
	delete(pvc.Annotations, "volume.kubernetes.io/selected-node")

	if (pvcConfig == nil || !pvcConfig.PreserveVolumeAttributes) && !syncPV {
		pvc.Spec.VolumeMode = nil
		pvc.Spec.Selector = nil
		pvc.Spec.DataSource = nil
		pvc.Spec.DataSourceRef = nil
	}

	pvc.ResourceVersion = ""
}

// createOrUpdateDestPVC creates the PVC in the destination cluster if it doesn't exist,
// or updates its mutable fields (resources.requests) if it does. Returns the resulting PVC.
func createOrUpdateDestPVC(ctx context.Context, destClient kubernetes.Interface,
	destPVC *corev1.PersistentVolumeClaim, dstNamespace string,
	pvcConfig *drv1alpha1.PVCConfig) (*corev1.PersistentVolumeClaim, error) {

	existingPVC, getErr := destClient.CoreV1().PersistentVolumeClaims(dstNamespace).Get(ctx, destPVC.Name, metav1.GetOptions{})
	pvcExists := getErr == nil

	if !pvcExists {
		preparePVCForCreation(destPVC, pvcConfig)
		createdPVC, createErr := destClient.CoreV1().PersistentVolumeClaims(dstNamespace).Create(ctx, destPVC, metav1.CreateOptions{})
		if createErr != nil {
			return nil, syncerrors.NewRetryableError(
				fmt.Errorf("failed to create PVC %s: %w", destPVC.Name, createErr),
				fmt.Sprintf("PersistentVolumeClaim/%s", destPVC.Name),
			)
		}
		return createdPVC, nil
	}

	updatePVC := existingPVC.DeepCopy()
	updatePVC.Spec.Resources = destPVC.Spec.Resources
	updatedPVC, updateErr := destClient.CoreV1().PersistentVolumeClaims(dstNamespace).Update(ctx, updatePVC, metav1.UpdateOptions{})
	if updateErr != nil {
		return nil, syncerrors.NewRetryableError(
			fmt.Errorf("failed to update PVC %s: %w", destPVC.Name, updateErr),
			fmt.Sprintf("PersistentVolumeClaim/%s", destPVC.Name),
		)
	}
	return updatedPVC, nil
}
