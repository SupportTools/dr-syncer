/*
Copyright 2024 Support Tools.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package replication

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	snapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
)

const (
	// DefaultSnapshotTimeout is the default time to wait for a snapshot to become ready
	DefaultSnapshotTimeout = 5 * time.Minute

	// snapshotNamePrefix is the prefix for snapshot names
	snapshotNamePrefix = "dr-syncer-snap"

	// restorePVCNamePrefix is the prefix for restored PVC names
	restorePVCNamePrefix = "dr-syncer-snap-restore"
)

// SnapshotManager handles VolumeSnapshot lifecycle for PVC sync operations.
// It provides methods to create snapshots, wait for them to be ready,
// restore them to temporary PVCs, and clean up resources.
type SnapshotManager struct {
	client client.Client
	log    *logrus.Entry
}

// SnapshotResult contains the results of a snapshot creation operation
type SnapshotResult struct {
	// Snapshot is the created VolumeSnapshot
	Snapshot *snapshotv1.VolumeSnapshot

	// RestorePVC is the temporary PVC created from the snapshot
	RestorePVC *corev1.PersistentVolumeClaim

	// SnapshotInfo contains metadata for status tracking
	SnapshotInfo *drv1alpha1.SnapshotSyncInfo
}

// NewSnapshotManager creates a new SnapshotManager
func NewSnapshotManager(c client.Client) *SnapshotManager {
	return &SnapshotManager{
		client: c,
		log:    log.WithField("component", "snapshot-manager"),
	}
}

// CanSnapshot checks if the PVC supports CSI snapshots.
// It verifies that:
// 1. The PVC is bound to a PV
// 2. The PV uses a CSI driver
// 3. A VolumeSnapshotClass exists for the storage class
func (m *SnapshotManager) CanSnapshot(ctx context.Context, namespace, pvcName string) (bool, string, error) {
	logger := m.log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc":       pvcName,
	})

	// Get the PVC
	pvc := &corev1.PersistentVolumeClaim{}
	if err := m.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: pvcName}, pvc); err != nil {
		return false, "", fmt.Errorf("failed to get PVC: %w", err)
	}

	// Check if PVC is bound
	if pvc.Status.Phase != corev1.ClaimBound {
		logger.Debug("PVC is not bound, cannot snapshot")
		return false, "", nil
	}

	// Get the PV
	pvName := pvc.Spec.VolumeName
	if pvName == "" {
		logger.Debug("PVC has no volume name, cannot snapshot")
		return false, "", nil
	}

	pv := &corev1.PersistentVolume{}
	if err := m.client.Get(ctx, types.NamespacedName{Name: pvName}, pv); err != nil {
		return false, "", fmt.Errorf("failed to get PV %s: %w", pvName, err)
	}

	// Check if PV uses CSI
	if pv.Spec.CSI == nil {
		logger.Debug("PV does not use CSI driver, cannot snapshot")
		return false, "", nil
	}

	csiDriver := pv.Spec.CSI.Driver
	logger.WithField("csiDriver", csiDriver).Debug("PV uses CSI driver")

	// Find a VolumeSnapshotClass for this CSI driver
	snapshotClassName, err := m.findVolumeSnapshotClass(ctx, csiDriver)
	if err != nil {
		return false, "", err
	}

	if snapshotClassName == "" {
		logger.Debug("No VolumeSnapshotClass found for CSI driver")
		return false, "", nil
	}

	logger.WithField("volumeSnapshotClass", snapshotClassName).Debug("Found VolumeSnapshotClass for CSI driver")
	return true, snapshotClassName, nil
}

// findVolumeSnapshotClass finds a VolumeSnapshotClass for the given CSI driver
func (m *SnapshotManager) findVolumeSnapshotClass(ctx context.Context, csiDriver string) (string, error) {
	snapshotClassList := &snapshotv1.VolumeSnapshotClassList{}
	if err := m.client.List(ctx, snapshotClassList); err != nil {
		// If CRDs are not installed, this will fail - treat as "no snapshot support"
		if errors.IsNotFound(err) || isKindNotFoundError(err) {
			m.log.Debug("VolumeSnapshotClass CRD not installed")
			return "", nil
		}
		return "", fmt.Errorf("failed to list VolumeSnapshotClasses: %w", err)
	}

	// Find a snapshot class that matches the CSI driver
	for _, sc := range snapshotClassList.Items {
		if sc.Driver == csiDriver {
			return sc.Name, nil
		}
	}

	return "", nil
}

// isKindNotFoundError checks if the error is a "kind not found" error
func isKindNotFoundError(err error) bool {
	// Check for API discovery errors when CRDs aren't installed
	return err != nil && (errors.IsNotFound(err) ||
		// Match discovery errors
		fmt.Sprintf("%v", err) == "no matches for kind \"VolumeSnapshotClass\" in version \"snapshot.storage.k8s.io/v1\"")
}

// CreateSnapshot creates a VolumeSnapshot from a PVC.
// The snapshot name follows the pattern: {prefix}-{pvcName}-{timestamp}
func (m *SnapshotManager) CreateSnapshot(ctx context.Context, namespace, pvcName, snapshotClassName string) (*snapshotv1.VolumeSnapshot, error) {
	logger := m.log.WithFields(logrus.Fields{
		"namespace":           namespace,
		"pvc":                 pvcName,
		"volumeSnapshotClass": snapshotClassName,
	})

	// Generate snapshot name
	timestamp := time.Now().Format("20060102-150405")
	snapshotName := fmt.Sprintf("%s-%s-%s", snapshotNamePrefix, truncateName(pvcName, 30), timestamp)

	logger.WithField("snapshotName", snapshotName).Info("Creating VolumeSnapshot")

	snapshot := &snapshotv1.VolumeSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      snapshotName,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "dr-syncer",
				"dr-syncer.io/source-pvc":      pvcName,
			},
		},
		Spec: snapshotv1.VolumeSnapshotSpec{
			VolumeSnapshotClassName: &snapshotClassName,
			Source: snapshotv1.VolumeSnapshotSource{
				PersistentVolumeClaimName: &pvcName,
			},
		},
	}

	if err := m.client.Create(ctx, snapshot); err != nil {
		return nil, fmt.Errorf("failed to create VolumeSnapshot: %w", err)
	}

	logger.Info("VolumeSnapshot created successfully")
	return snapshot, nil
}

// WaitForSnapshotReady waits for a VolumeSnapshot to become ready.
// Returns an error if the snapshot doesn't become ready within the timeout.
func (m *SnapshotManager) WaitForSnapshotReady(ctx context.Context, namespace, snapshotName string, timeout time.Duration) error {
	logger := m.log.WithFields(logrus.Fields{
		"namespace": namespace,
		"snapshot":  snapshotName,
		"timeout":   timeout,
	})

	logger.Info("Waiting for VolumeSnapshot to become ready")

	pollInterval := 5 * time.Second
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		snapshot := &snapshotv1.VolumeSnapshot{}
		if err := m.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: snapshotName}, snapshot); err != nil {
			return fmt.Errorf("failed to get VolumeSnapshot: %w", err)
		}

		if snapshot.Status != nil && snapshot.Status.ReadyToUse != nil && *snapshot.Status.ReadyToUse {
			logger.Info("VolumeSnapshot is ready")
			return nil
		}

		// Check for error
		if snapshot.Status != nil && snapshot.Status.Error != nil && snapshot.Status.Error.Message != nil {
			return fmt.Errorf("snapshot failed: %s", *snapshot.Status.Error.Message)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
			// Continue polling
		}
	}

	return fmt.Errorf("timeout waiting for snapshot %s to become ready", snapshotName)
}

// RestoreSnapshotToPVC creates a temporary PVC from a snapshot.
// The PVC is created with the same access modes and storage class as the original PVC.
func (m *SnapshotManager) RestoreSnapshotToPVC(ctx context.Context, namespace, snapshotName string, originalPVC *corev1.PersistentVolumeClaim) (*corev1.PersistentVolumeClaim, error) {
	logger := m.log.WithFields(logrus.Fields{
		"namespace":   namespace,
		"snapshot":    snapshotName,
		"originalPVC": originalPVC.Name,
	})

	// Generate a unique name for the restored PVC
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s-%s-%d", snapshotName, originalPVC.Name, time.Now().UnixNano())))
	hashStr := hex.EncodeToString(hash[:])[:8]
	restorePVCName := fmt.Sprintf("%s-%s-%s", restorePVCNamePrefix, truncateName(originalPVC.Name, 30), hashStr)

	logger.WithField("restorePVC", restorePVCName).Info("Creating temporary PVC from snapshot")

	// Get the storage request from original PVC
	storageRequest := originalPVC.Spec.Resources.Requests[corev1.ResourceStorage]
	if storageRequest.IsZero() {
		// Use status capacity if requests is empty
		if cap, ok := originalPVC.Status.Capacity[corev1.ResourceStorage]; ok {
			storageRequest = cap
		} else {
			storageRequest = resource.MustParse("1Gi") // Fallback
		}
	}

	// Create PVC from snapshot
	apiGroup := "snapshot.storage.k8s.io"
	restorePVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      restorePVCName,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "dr-syncer",
				"dr-syncer.io/source-pvc":      originalPVC.Name,
				"dr-syncer.io/snapshot":        snapshotName,
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      originalPVC.Spec.AccessModes,
			StorageClassName: originalPVC.Spec.StorageClassName,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: storageRequest,
				},
			},
			DataSource: &corev1.TypedLocalObjectReference{
				APIGroup: &apiGroup,
				Kind:     "VolumeSnapshot",
				Name:     snapshotName,
			},
		},
	}

	// If original PVC has volume mode, copy it
	if originalPVC.Spec.VolumeMode != nil {
		restorePVC.Spec.VolumeMode = originalPVC.Spec.VolumeMode
	}

	if err := m.client.Create(ctx, restorePVC); err != nil {
		return nil, fmt.Errorf("failed to create restore PVC: %w", err)
	}

	logger.Info("Restore PVC created successfully")

	// Wait for the PVC to be bound
	if err := m.waitForPVCBound(ctx, namespace, restorePVCName, 5*time.Minute); err != nil {
		// Clean up the PVC if binding fails
		_ = m.client.Delete(ctx, restorePVC)
		return nil, fmt.Errorf("failed waiting for restore PVC to bind: %w", err)
	}

	return restorePVC, nil
}

// waitForPVCBound waits for a PVC to become bound
func (m *SnapshotManager) waitForPVCBound(ctx context.Context, namespace, pvcName string, timeout time.Duration) error {
	pollInterval := 5 * time.Second
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		pvc := &corev1.PersistentVolumeClaim{}
		if err := m.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: pvcName}, pvc); err != nil {
			return fmt.Errorf("failed to get PVC: %w", err)
		}

		if pvc.Status.Phase == corev1.ClaimBound {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
			// Continue polling
		}
	}

	return fmt.Errorf("timeout waiting for PVC %s to become bound", pvcName)
}

// CleanupSnapshot deletes the snapshot and temporary PVC.
// Errors are logged but not returned to avoid failing the sync due to cleanup issues.
func (m *SnapshotManager) CleanupSnapshot(ctx context.Context, namespace, snapshotName, tempPVCName string) {
	logger := m.log.WithFields(logrus.Fields{
		"namespace": namespace,
		"snapshot":  snapshotName,
		"tempPVC":   tempPVCName,
	})

	logger.Info("Cleaning up snapshot resources")

	// Delete the temporary PVC first (releases the snapshot)
	if tempPVCName != "" {
		tempPVC := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      tempPVCName,
				Namespace: namespace,
			},
		}
		if err := m.client.Delete(ctx, tempPVC); err != nil && !errors.IsNotFound(err) {
			logger.WithError(err).Warn("Failed to delete temporary PVC")
		} else {
			logger.Debug("Temporary PVC deleted")
		}
	}

	// Delete the snapshot
	if snapshotName != "" {
		snapshot := &snapshotv1.VolumeSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:      snapshotName,
				Namespace: namespace,
			},
		}
		if err := m.client.Delete(ctx, snapshot); err != nil && !errors.IsNotFound(err) {
			logger.WithError(err).Warn("Failed to delete VolumeSnapshot")
		} else {
			logger.Debug("VolumeSnapshot deleted")
		}
	}

	logger.Info("Snapshot cleanup completed")
}

// GetSnapshotInfo retrieves metadata about a snapshot for status tracking
func (m *SnapshotManager) GetSnapshotInfo(ctx context.Context, namespace, snapshotName string) (*drv1alpha1.SnapshotSyncInfo, error) {
	snapshot := &snapshotv1.VolumeSnapshot{}
	if err := m.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: snapshotName}, snapshot); err != nil {
		return nil, fmt.Errorf("failed to get snapshot: %w", err)
	}

	info := &drv1alpha1.SnapshotSyncInfo{
		SnapshotName: snapshotName,
	}

	if snapshot.Status != nil {
		if snapshot.Status.CreationTime != nil {
			info.SnapshotReadyTime = snapshot.Status.CreationTime
		}
		if snapshot.Status.RestoreSize != nil {
			size := snapshot.Status.RestoreSize.Value()
			info.SnapshotSize = &size
		}
	}

	return info, nil
}

// GetStorageClassForPVC retrieves the StorageClass for a PVC
func (m *SnapshotManager) GetStorageClassForPVC(ctx context.Context, pvc *corev1.PersistentVolumeClaim) (*storagev1.StorageClass, error) {
	if pvc.Spec.StorageClassName == nil || *pvc.Spec.StorageClassName == "" {
		return nil, nil
	}

	sc := &storagev1.StorageClass{}
	if err := m.client.Get(ctx, types.NamespacedName{Name: *pvc.Spec.StorageClassName}, sc); err != nil {
		return nil, err
	}

	return sc, nil
}

// truncateName truncates a name to maxLen characters
func truncateName(name string, maxLen int) string {
	if len(name) <= maxLen {
		return name
	}
	return name[:maxLen]
}
