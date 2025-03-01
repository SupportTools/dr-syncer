package syncer

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/supporttools/dr-syncer/pkg/pvcmounter"
)

var pvcLog = logrus.WithField("component", "pvcmount-handler")

// PVCMountManager handles PVC mounting for synchronization
type PVCMountManager struct {
	sourceMounter *pvcmounter.PVCMounter
	targetMounter *pvcmounter.PVCMounter
}

// NewPVCMountManager creates a new manager for mounting PVCs during synchronization
func NewPVCMountManager(sourceClient, targetClient kubernetes.Interface) *PVCMountManager {
	// Create the source PVC mounter with default configuration
	sourceMounter := pvcmounter.NewPVCMounter(sourceClient, &pvcmounter.MountPodConfig{
		PodNamePrefix: "dr-syncer-source-mount",
		Labels: map[string]string{
			"app.kubernetes.io/managed-by": "dr-syncer",
			"app.kubernetes.io/name":       "dr-source-mount-pod",
			"app.kubernetes.io/part-of":    "dr-syncer",
			"dr-syncer.io/role":            "source-mount",
		},
	})

	// Create the target PVC mounter with default configuration
	targetMounter := pvcmounter.NewPVCMounter(targetClient, &pvcmounter.MountPodConfig{
		PodNamePrefix: "dr-syncer-target-mount",
		Labels: map[string]string{
			"app.kubernetes.io/managed-by": "dr-syncer",
			"app.kubernetes.io/name":       "dr-target-mount-pod",
			"app.kubernetes.io/part-of":    "dr-syncer",
			"dr-syncer.io/role":            "target-mount",
		},
	})

	return &PVCMountManager{
		sourceMounter: sourceMounter,
		targetMounter: targetMounter,
	}
}

// EnsurePVCMounted ensures a PVC is mounted in the specified cluster
func (m *PVCMountManager) EnsurePVCMounted(ctx context.Context, isSource bool, namespace, pvcName string) error {
	mounter := m.targetMounter
	clusterDesc := "target"
	if isSource {
		mounter = m.sourceMounter
		clusterDesc = "source"
	}

	pvcLog.Infof("Ensuring PVC %s is mounted in %s cluster namespace %s", pvcName, clusterDesc, namespace)
	
	return mounter.EnsurePVCMounted(ctx, namespace, pvcName)
}

// EnsurePVCsMounted ensures PVCs are mounted in both source and target clusters
func (m *PVCMountManager) EnsurePVCsMounted(ctx context.Context, sourcePVC, targetPVC *corev1.PersistentVolumeClaim) error {
	// Ensure source PVC is mounted
	if err := m.EnsurePVCMounted(ctx, true, sourcePVC.Namespace, sourcePVC.Name); err != nil {
		return fmt.Errorf("failed to mount source PVC %s/%s: %w", 
			sourcePVC.Namespace, sourcePVC.Name, err)
	}

	// Ensure target PVC is mounted
	if err := m.EnsurePVCMounted(ctx, false, targetPVC.Namespace, targetPVC.Name); err != nil {
		return fmt.Errorf("failed to mount target PVC %s/%s: %w", 
			targetPVC.Namespace, targetPVC.Name, err)
	}

	return nil
}

// CleanupMountPods removes mount pods for PVCs
func (m *PVCMountManager) CleanupMountPods(ctx context.Context, sourcePVC, targetPVC *corev1.PersistentVolumeClaim) error {
	// Cleanup source mount pod
	if err := m.sourceMounter.CleanupMountPod(ctx, sourcePVC.Namespace, sourcePVC.Name); err != nil {
		pvcLog.Warnf("Failed to cleanup source mount pod for PVC %s/%s: %v", 
			sourcePVC.Namespace, sourcePVC.Name, err)
		// Continue anyway to try to clean up target mount pod
	}

	// Cleanup target mount pod
	if err := m.targetMounter.CleanupMountPod(ctx, targetPVC.Namespace, targetPVC.Name); err != nil {
		return fmt.Errorf("failed to cleanup target mount pod for PVC %s/%s: %w",
			targetPVC.Namespace, targetPVC.Name, err)
	}

	return nil
}

// IsPVCMounted checks if a PVC is mounted in the specified cluster
func (m *PVCMountManager) IsPVCMounted(ctx context.Context, isSource bool, namespace, pvcName string) (bool, error) {
	mounter := m.targetMounter
	if isSource {
		mounter = m.sourceMounter
	}

	return mounter.IsPVCMounted(ctx, namespace, pvcName)
}
