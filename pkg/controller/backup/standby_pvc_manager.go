package backup

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
)

const (
	// Labels applied to standby PVCs for identification and filtering.
	labelManagedBy      = "dr-syncer.io/managed-by"
	labelStandby        = "dr-syncer.io/standby"
	labelSourcePVC      = "dr-syncer.io/source-pvc"
	labelSourceNS       = "dr-syncer.io/source-namespace"
	labelMappingRef     = "dr-syncer.io/mapping"
	labelManagedByValue = "dr-syncer"

	// Annotation for tracking when the standby PVC was created.
	annotationCreatedAt = "dr-syncer.io/created-at"
)

// StandbyPVCManagerImpl implements StandbyPVCManager using a controller-runtime client.
type StandbyPVCManagerImpl struct {
	// DestClient is the Kubernetes client for the DR (destination) cluster.
	DestClient client.Client

	// StandbyConfig holds standby PVC configuration overrides.
	// May be nil if no overrides are configured.
	StandbyConfig *drv1alpha1.StandbyPVCConfig

	// PVCConfig holds storage class and access mode mappings.
	// May be nil if no mappings are configured.
	PVCConfig *drv1alpha1.PVCConfig

	// MappingName is the NamespaceMapping name (used for labeling).
	MappingName string

	Log *logrus.Entry
}

// NewStandbyPVCManager creates a new StandbyPVCManagerImpl.
func NewStandbyPVCManager(
	destClient client.Client,
	standbyConfig *drv1alpha1.StandbyPVCConfig,
	pvcConfig *drv1alpha1.PVCConfig,
	mappingName string,
) *StandbyPVCManagerImpl {
	return &StandbyPVCManagerImpl{
		DestClient:    destClient,
		StandbyConfig: standbyConfig,
		PVCConfig:     pvcConfig,
		MappingName:   mappingName,
		Log:           logrus.WithField("component", "standby-pvc-manager"),
	}
}

// EnsureStandbyPVC creates or verifies a standby PVC on the DR cluster.
// Returns the PVC reference and whether it was newly created.
func (m *StandbyPVCManagerImpl) EnsureStandbyPVC(
	ctx context.Context,
	sourcePVC drv1alpha1.PVCReference,
	destNamespace string,
	sourcePVCSpec *corev1.PersistentVolumeClaimSpec,
) (*drv1alpha1.PVCReference, bool, error) {
	standbyName := sourcePVC.Name + standbyPVCSuffix
	log := m.Log.WithFields(logrus.Fields{
		"source_pvc":  sourcePVC.Name,
		"source_ns":   sourcePVC.Namespace,
		"dest_ns":     destNamespace,
		"standby_pvc": standbyName,
	})

	// Check if the standby PVC already exists.
	existing := &corev1.PersistentVolumeClaim{}
	key := types.NamespacedName{Name: standbyName, Namespace: destNamespace}
	err := m.DestClient.Get(ctx, key, existing)
	if err == nil {
		log.Debug("Standby PVC already exists")
		return &drv1alpha1.PVCReference{
			Namespace: destNamespace,
			Name:      standbyName,
		}, false, nil
	}
	if !apierrors.IsNotFound(err) {
		return nil, false, fmt.Errorf("check existing standby PVC: %w", err)
	}

	// PVC does not exist — create it.
	if sourcePVCSpec == nil {
		return nil, false, fmt.Errorf("source PVC spec is required to create standby PVC %s/%s", destNamespace, standbyName)
	}

	storageClass := m.resolveStorageClass(sourcePVCSpec)
	accessModes := m.resolveAccessModes(sourcePVCSpec)

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      standbyName,
			Namespace: destNamespace,
			Labels: map[string]string{
				labelManagedBy:  labelManagedByValue,
				labelStandby:    "true",
				labelSourcePVC:  sourcePVC.Name,
				labelSourceNS:   sourcePVC.Namespace,
				labelMappingRef: m.MappingName,
			},
			Annotations: map[string]string{
				annotationCreatedAt: time.Now().UTC().Format(time.RFC3339),
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: accessModes,
			Resources:   m.resolveResources(sourcePVCSpec),
		},
	}

	if storageClass != "" {
		pvc.Spec.StorageClassName = &storageClass
	}

	if err := m.DestClient.Create(ctx, pvc); err != nil {
		return nil, false, fmt.Errorf("create standby PVC %s/%s: %w", destNamespace, standbyName, err)
	}

	log.Info("Standby PVC created")
	return &drv1alpha1.PVCReference{
		Namespace: destNamespace,
		Name:      standbyName,
	}, true, nil
}

// UpdateStandbyPVCStatus updates the standby PVC annotations after a restore.
// Uses Get-then-Update pattern to avoid etcd conflicts.
func (m *StandbyPVCManagerImpl) UpdateStandbyPVCStatus(
	ctx context.Context,
	pvcRef drv1alpha1.PVCReference,
	snapshotID string,
) error {
	pvc := &corev1.PersistentVolumeClaim{}
	key := types.NamespacedName{Name: pvcRef.Name, Namespace: pvcRef.Namespace}
	if err := m.DestClient.Get(ctx, key, pvc); err != nil {
		return fmt.Errorf("get standby PVC for status update: %w", err)
	}

	if pvc.Annotations == nil {
		pvc.Annotations = make(map[string]string)
	}
	pvc.Annotations[annotationLastRestoreTime] = time.Now().UTC().Format(time.RFC3339)
	pvc.Annotations[annotationLastRestoreSnapID] = snapshotID

	if err := m.DestClient.Update(ctx, pvc); err != nil {
		return fmt.Errorf("update standby PVC annotations: %w", err)
	}

	m.Log.WithFields(logrus.Fields{
		"pvc":         pvcRef.Name,
		"namespace":   pvcRef.Namespace,
		"snapshot_id": snapshotID,
	}).Debug("Standby PVC status updated")

	return nil
}

// CleanupStandbyPVCs deletes all standby PVCs for the given mapping in the destination namespace.
// PVCs that are currently mounted by pods are skipped to avoid disrupting running workloads.
func (m *StandbyPVCManagerImpl) CleanupStandbyPVCs(ctx context.Context, destNamespace string) error {
	log := m.Log.WithFields(logrus.Fields{
		"namespace": destNamespace,
		"mapping":   m.MappingName,
	})

	pvcs, err := m.ListStandbyPVCs(ctx, destNamespace)
	if err != nil {
		return fmt.Errorf("list standby PVCs for cleanup: %w", err)
	}

	var deleteErrors []error
	for i := range pvcs {
		pvc := &pvcs[i]

		inUse, err := m.isStandbyPVCInUse(ctx, pvc)
		if err != nil {
			log.WithError(err).WithField("pvc", pvc.Name).Warn("Failed to check if standby PVC is in use, skipping deletion")
			continue
		}
		if inUse {
			log.WithField("pvc", pvc.Name).Info("Standby PVC is in use, skipping deletion")
			continue
		}

		if err := m.DestClient.Delete(ctx, pvc); err != nil && !apierrors.IsNotFound(err) {
			deleteErrors = append(deleteErrors, fmt.Errorf("delete standby PVC %s: %w", pvc.Name, err))
			continue
		}
		log.WithField("pvc", pvc.Name).Info("Standby PVC deleted")
	}

	if len(deleteErrors) > 0 {
		return fmt.Errorf("failed to delete %d standby PVCs: %v", len(deleteErrors), deleteErrors[0])
	}
	return nil
}

// ListStandbyPVCs returns all standby PVCs for this mapping in the given namespace.
func (m *StandbyPVCManagerImpl) ListStandbyPVCs(ctx context.Context, namespace string) ([]corev1.PersistentVolumeClaim, error) {
	pvcList := &corev1.PersistentVolumeClaimList{}
	if err := m.DestClient.List(ctx, pvcList,
		client.InNamespace(namespace),
		client.MatchingLabels{
			labelStandby:    "true",
			labelManagedBy:  labelManagedByValue,
			labelMappingRef: m.MappingName,
		},
	); err != nil {
		return nil, fmt.Errorf("list standby PVCs: %w", err)
	}
	return pvcList.Items, nil
}

// isStandbyPVCInUse checks whether the PVC is currently mounted by any pod.
func (m *StandbyPVCManagerImpl) isStandbyPVCInUse(ctx context.Context, pvc *corev1.PersistentVolumeClaim) (bool, error) {
	podList := &corev1.PodList{}
	if err := m.DestClient.List(ctx, podList, client.InNamespace(pvc.Namespace)); err != nil {
		return false, fmt.Errorf("list pods: %w", err)
	}

	for i := range podList.Items {
		pod := &podList.Items[i]
		for _, vol := range pod.Spec.Volumes {
			if vol.PersistentVolumeClaim != nil && vol.PersistentVolumeClaim.ClaimName == pvc.Name {
				return true, nil
			}
		}
	}

	return false, nil
}

// resolveStorageClass determines the storage class for the standby PVC.
// Priority: StandbyPVCConfig override > StorageClassMappings > source PVC storage class.
func (m *StandbyPVCManagerImpl) resolveStorageClass(sourcePVCSpec *corev1.PersistentVolumeClaimSpec) string {
	// Highest priority: explicit standby config override.
	if m.StandbyConfig != nil && m.StandbyConfig.StorageClassName != "" {
		return m.StandbyConfig.StorageClassName
	}

	sourceClass := ""
	if sourcePVCSpec.StorageClassName != nil {
		sourceClass = *sourcePVCSpec.StorageClassName
	}

	// Second priority: storage class mappings.
	if m.PVCConfig != nil && sourceClass != "" {
		for _, mapping := range m.PVCConfig.StorageClassMappings {
			if mapping.From == sourceClass {
				return mapping.To
			}
		}
	}

	// Fallback: use source PVC storage class.
	return sourceClass
}

// resolveAccessModes determines the access modes for the standby PVC.
// Priority: StandbyPVCConfig override > AccessModeMappings > source PVC access modes.
func (m *StandbyPVCManagerImpl) resolveAccessModes(sourcePVCSpec *corev1.PersistentVolumeClaimSpec) []corev1.PersistentVolumeAccessMode {
	// Highest priority: explicit standby config override.
	if m.StandbyConfig != nil && m.StandbyConfig.AccessMode != "" {
		return []corev1.PersistentVolumeAccessMode{
			corev1.PersistentVolumeAccessMode(m.StandbyConfig.AccessMode),
		}
	}

	// Start with source access modes.
	modes := make([]corev1.PersistentVolumeAccessMode, len(sourcePVCSpec.AccessModes))
	copy(modes, sourcePVCSpec.AccessModes)

	// Apply access mode mappings.
	if m.PVCConfig != nil {
		for _, mapping := range m.PVCConfig.AccessModeMappings {
			for i, mode := range modes {
				if string(mode) == mapping.From {
					modes[i] = corev1.PersistentVolumeAccessMode(mapping.To)
				}
			}
		}
	}

	return modes
}

// resolveResources returns the resource requirements for the standby PVC.
// Uses the source PVC's requested storage size.
func (m *StandbyPVCManagerImpl) resolveResources(sourcePVCSpec *corev1.PersistentVolumeClaimSpec) corev1.VolumeResourceRequirements {
	// Use the source PVC's storage request as the standby PVC size.
	result := corev1.VolumeResourceRequirements{
		Requests: corev1.ResourceList{},
	}

	if sourcePVCSpec.Resources.Requests != nil {
		if storageReq, ok := sourcePVCSpec.Resources.Requests[corev1.ResourceStorage]; ok {
			result.Requests[corev1.ResourceStorage] = storageReq.DeepCopy()
		}
	}

	// If no request found, use a sensible default.
	if _, ok := result.Requests[corev1.ResourceStorage]; !ok {
		result.Requests[corev1.ResourceStorage] = resource.MustParse("1Gi")
	}

	return result
}
