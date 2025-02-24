package sync

import (
	"context"
	"fmt"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/supporttools/dr-syncer/pkg/agent/rsync"
)

const (
	kubeletPath = "/var/lib/kubelet"
	podsDirName = "pods"
	volumesDir  = "volumes"
	k8sPrefix   = "kubernetes.io~"
)

// PVCInfo contains information about a PVC and its data location
type PVCInfo struct {
	Name          string
	Namespace     string
	Node          string
	VolumePath    string
	VolumeType    string
	StorageClass  string
	AccessModes   []corev1.PersistentVolumeAccessMode
	Capacity      string
	BoundPodNames []string
}

// PVCManager handles PVC discovery and sync operations
type PVCManager struct {
	client client.Client
}

// NewPVCManager creates a new PVC manager
func NewPVCManager(client client.Client) *PVCManager {
	return &PVCManager{
		client: client,
	}
}

// DiscoverPVCs finds all PVCs and their data locations on the node
func (p *PVCManager) DiscoverPVCs(ctx context.Context, nodeName string) ([]PVCInfo, error) {
	logger := log.FromContext(ctx)
	logger.Info("Discovering PVCs", "node", nodeName)

	// Get all PVCs in the cluster
	pvcList := &corev1.PersistentVolumeClaimList{}
	if err := p.client.List(ctx, pvcList); err != nil {
		return nil, fmt.Errorf("failed to list PVCs: %v", err)
	}

	// Get all pods on this node
	podList := &corev1.PodList{}
	if err := p.client.List(ctx, podList, client.MatchingFields{"spec.nodeName": nodeName}); err != nil {
		return nil, fmt.Errorf("failed to list pods: %v", err)
	}

	// Map PVCs to their pod volumes
	var pvcInfos []PVCInfo
	for _, pod := range podList.Items {
		for _, volume := range pod.Spec.Volumes {
			if volume.PersistentVolumeClaim == nil {
				continue
			}

			// Find the PVC
			var pvc *corev1.PersistentVolumeClaim
			for _, p := range pvcList.Items {
				if p.Name == volume.PersistentVolumeClaim.ClaimName && p.Namespace == pod.Namespace {
					pvc = &p
					break
				}
			}
			if pvc == nil {
				continue
			}

			// Get PV details
			pv := &corev1.PersistentVolume{}
			if err := p.client.Get(ctx, types.NamespacedName{Name: pvc.Spec.VolumeName}, pv); err != nil {
				logger.Error(err, "Failed to get PV", "pv", pvc.Spec.VolumeName)
				continue
			}

			// Determine volume path
			volumePath := p.getVolumePath(pod.UID, volume.Name, pv)
			if volumePath == "" {
				logger.Info("Could not determine volume path", "pod", pod.Name, "volume", volume.Name)
				continue
			}

			// Create PVC info
			pvcInfo := PVCInfo{
				Name:         pvc.Name,
				Namespace:    pvc.Namespace,
				Node:        nodeName,
				VolumePath:  volumePath,
				VolumeType:  p.getVolumeType(pv),
				StorageClass: derefString(pvc.Spec.StorageClassName),
				AccessModes: pvc.Spec.AccessModes,
				Capacity:    pvc.Status.Capacity.Storage().String(),
				BoundPodNames: []string{pod.Name},
			}

			// Check if PVC already exists in list
			found := false
			for i, info := range pvcInfos {
				if info.Name == pvcInfo.Name && info.Namespace == pvcInfo.Namespace {
					pvcInfos[i].BoundPodNames = append(pvcInfos[i].BoundPodNames, pod.Name)
					found = true
					break
				}
			}
			if !found {
				pvcInfos = append(pvcInfos, pvcInfo)
			}
		}
	}

	return pvcInfos, nil
}

// getVolumePath determines the path to the volume data
func (p *PVCManager) getVolumePath(podUID types.UID, volumeName string, pv *corev1.PersistentVolume) string {
	// Base path for pod volumes
	podVolumePath := filepath.Join(kubeletPath, podsDirName, string(podUID), volumesDir)

	// Handle different volume types
	var volumeSubPath string
	switch {
	case pv.Spec.HostPath != nil:
		return pv.Spec.HostPath.Path
	case pv.Spec.Local != nil:
		return pv.Spec.Local.Path
	case pv.Spec.CSI != nil:
		volumeSubPath = filepath.Join(k8sPrefix+"csi", pv.Spec.CSI.Driver, pv.Spec.CSI.VolumeHandle)
	default:
		// Handle other volume types as needed
		return ""
	}

	return filepath.Join(podVolumePath, volumeSubPath)
}

// getVolumeType returns a string representation of the volume type
func (p *PVCManager) getVolumeType(pv *corev1.PersistentVolume) string {
	switch {
	case pv.Spec.HostPath != nil:
		return "HostPath"
	case pv.Spec.Local != nil:
		return "Local"
	case pv.Spec.CSI != nil:
		return fmt.Sprintf("CSI (%s)", pv.Spec.CSI.Driver)
	default:
		return "Unknown"
	}
}

// derefString safely dereferences a string pointer
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// SyncPVC syncs a PVC's data to a target node
func (p *PVCManager) SyncPVC(ctx context.Context, pvcInfo PVCInfo, targetNode string, targetPath string) error {
	logger := log.FromContext(ctx)
	logger.Info("Syncing PVC", "pvc", pvcInfo.Name, "namespace", pvcInfo.Namespace, "source", pvcInfo.Node, "target", targetNode)

	// Build rsync source and target paths
	source := fmt.Sprintf("%s/", pvcInfo.VolumePath) // Trailing slash to sync contents
	target := fmt.Sprintf("syncer@%s:%s", targetNode, targetPath)

	// Configure rsync options
	opts := rsync.DefaultOptions()
	opts.Source = source
	opts.Destination = target
	opts.Archive = true
	opts.Delete = true
	opts.Compress = true

	// Execute rsync
	if err := rsync.Sync(opts); err != nil {
		return fmt.Errorf("failed to sync PVC data: %v", err)
	}

	return nil
}
