package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/supporttools/dr-syncer/pkg/logging"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// Label and annotation keys matching the controller's standby PVC conventions.
	labelStandby        = "dr-syncer.io/standby"
	labelManagedBy      = "dr-syncer.io/managed-by"
	labelSourcePVC      = "dr-syncer.io/source-pvc"
	annoLastRestoreTime = "dr-syncer.io/last-restore-time"
	annoLastRestoreSnap = "dr-syncer.io/last-restore-snapshot-id"
	standbyPVCSuffix    = "-standby"
	managedByValue      = "dr-syncer"
)

// StandbyPVCInfo holds information about a standby PVC on the DR cluster.
type StandbyPVCInfo struct {
	Name            string
	SourcePVCName   string
	LastRestoreTime *time.Time
	SnapshotID      string
	Phase           corev1.PersistentVolumeClaimPhase
}

// listStandbyPVCs lists all standby PVCs in the destination namespace.
func listStandbyPVCs(ctx context.Context, client kubernetes.Interface, namespace string) ([]StandbyPVCInfo, error) {
	selector := fmt.Sprintf("%s=true,%s=%s", labelStandby, labelManagedBy, managedByValue)
	pvcs, err := client.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list standby PVCs: %v", err)
	}

	var infos []StandbyPVCInfo
	for _, pvc := range pvcs.Items {
		info := StandbyPVCInfo{
			Name:  pvc.Name,
			Phase: pvc.Status.Phase,
		}

		if sourceName, ok := pvc.Labels[labelSourcePVC]; ok {
			info.SourcePVCName = sourceName
		}
		if restoreTime, ok := pvc.Annotations[annoLastRestoreTime]; ok {
			if t, err := time.Parse(time.RFC3339, restoreTime); err == nil {
				info.LastRestoreTime = &t
			}
		}
		if snapID, ok := pvc.Annotations[annoLastRestoreSnap]; ok {
			info.SnapshotID = snapID
		}
		infos = append(infos, info)
	}

	return infos, nil
}

// reportStandbyPVCStatus logs the readiness status of standby PVCs.
func reportStandbyPVCStatus(pvcs []StandbyPVCInfo) {
	log := logging.SetupLogging()

	if len(pvcs) == 0 {
		log.Warn("No standby PVCs found in destination namespace")
		return
	}

	log.Infof("Found %d standby PVC(s) in destination namespace:", len(pvcs))
	allReady := true
	for _, pvc := range pvcs {
		status := "Ready"
		if pvc.Phase != corev1.ClaimBound {
			status = fmt.Sprintf("Not Ready (phase: %s)", pvc.Phase)
			allReady = false
		}

		freshness := "never restored"
		if pvc.LastRestoreTime != nil {
			age := time.Since(*pvc.LastRestoreTime)
			freshness = fmt.Sprintf("restored %s ago (snapshot: %s)", age.Round(time.Second), pvc.SnapshotID)
		} else {
			allReady = false
		}

		log.Infof("  %s (source: %s) - %s - %s", pvc.Name, pvc.SourcePVCName, status, freshness)
	}

	if allReady {
		log.Info("All standby PVCs are ready for cutover")
	} else {
		log.Warn("Some standby PVCs are not ready - cutover may fail")
	}
}

// updateWorkloadPVCReferences updates deployments and statefulsets to mount standby PVCs
// instead of their original PVC names. Returns the count of workloads updated.
func updateWorkloadPVCReferences(ctx context.Context, client kubernetes.Interface, namespace string, standbyPVCs []StandbyPVCInfo) (int, error) {
	log := logging.SetupLogging()

	// Build source-to-standby mapping.
	pvcMapping := make(map[string]string)
	for _, spvc := range standbyPVCs {
		if spvc.SourcePVCName != "" {
			pvcMapping[spvc.SourcePVCName] = spvc.Name
		}
	}

	if len(pvcMapping) == 0 {
		log.Warn("No source-to-standby PVC mappings found")
		return 0, nil
	}

	updated := 0

	// Update deployments.
	deployments, err := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, fmt.Errorf("failed to list deployments: %v", err)
	}

	for i := range deployments.Items {
		dep := &deployments.Items[i]
		if rewriteVolumes(&dep.Spec.Template.Spec, pvcMapping) {
			if _, err := client.AppsV1().Deployments(namespace).Update(ctx, dep, metav1.UpdateOptions{}); err != nil {
				log.Warnf("Failed to update deployment %s: %v", dep.Name, err)
				continue
			}
			log.Infof("Updated deployment %s to use standby PVCs", dep.Name)
			updated++
		}
	}

	// Update statefulsets.
	statefulsets, err := client.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return updated, fmt.Errorf("failed to list statefulsets: %v", err)
	}

	for i := range statefulsets.Items {
		sts := &statefulsets.Items[i]
		if hasVolumeClaimTemplates(sts) && vctMappingOverlap(sts, pvcMapping) {
			log.Warnf("StatefulSet %s uses volumeClaimTemplates which are immutable and cannot be rewritten during cutover; PVCs provisioned by volumeClaimTemplates require manual intervention", sts.Name)
		}
		if rewriteVolumes(&sts.Spec.Template.Spec, pvcMapping) {
			if _, err := client.AppsV1().StatefulSets(namespace).Update(ctx, sts, metav1.UpdateOptions{}); err != nil {
				log.Warnf("Failed to update statefulset %s: %v", sts.Name, err)
				continue
			}
			log.Infof("Updated statefulset %s to use standby PVCs", sts.Name)
			updated++
		}
	}

	return updated, nil
}

// revertWorkloadPVCReferences reverts deployments and statefulsets from standby PVCs
// back to their original PVC names. Returns the count of workloads updated.
func revertWorkloadPVCReferences(ctx context.Context, client kubernetes.Interface, namespace string, standbyPVCs []StandbyPVCInfo) (int, error) {
	log := logging.SetupLogging()

	// Build standby-to-source mapping (reverse of updateWorkloadPVCReferences).
	pvcMapping := make(map[string]string)
	for _, spvc := range standbyPVCs {
		if spvc.SourcePVCName != "" {
			pvcMapping[spvc.Name] = spvc.SourcePVCName
		}
	}

	if len(pvcMapping) == 0 {
		log.Warn("No standby-to-source PVC mappings found")
		return 0, nil
	}

	updated := 0

	// Revert deployments.
	deployments, err := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, fmt.Errorf("failed to list deployments: %v", err)
	}

	for i := range deployments.Items {
		dep := &deployments.Items[i]
		if rewriteVolumes(&dep.Spec.Template.Spec, pvcMapping) {
			if _, err := client.AppsV1().Deployments(namespace).Update(ctx, dep, metav1.UpdateOptions{}); err != nil {
				log.Warnf("Failed to revert deployment %s: %v", dep.Name, err)
				continue
			}
			log.Infof("Reverted deployment %s to original PVCs", dep.Name)
			updated++
		}
	}

	// Revert statefulsets.
	statefulsets, err := client.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return updated, fmt.Errorf("failed to list statefulsets: %v", err)
	}

	for i := range statefulsets.Items {
		sts := &statefulsets.Items[i]
		if hasVolumeClaimTemplates(sts) && vctMappingOverlap(sts, pvcMapping) {
			log.Warnf("StatefulSet %s uses volumeClaimTemplates which are immutable and cannot be rewritten during failback; PVCs provisioned by volumeClaimTemplates require manual intervention", sts.Name)
		}
		if rewriteVolumes(&sts.Spec.Template.Spec, pvcMapping) {
			if _, err := client.AppsV1().StatefulSets(namespace).Update(ctx, sts, metav1.UpdateOptions{}); err != nil {
				log.Warnf("Failed to revert statefulset %s: %v", sts.Name, err)
				continue
			}
			log.Infof("Reverted statefulset %s to original PVCs", sts.Name)
			updated++
		}
	}

	return updated, nil
}

// hasVolumeClaimTemplates checks if a StatefulSet defines volumeClaimTemplates.
func hasVolumeClaimTemplates(sts *appsv1.StatefulSet) bool {
	return len(sts.Spec.VolumeClaimTemplates) > 0
}

// vctMappingOverlap checks whether any VCT-provisioned PVC names for a StatefulSet
// overlap with keys in pvcMapping. VCT PVCs follow the naming pattern
// "<vct-name>-<sts-name>-<ordinal>", so we check if any mapping key has the prefix
// "<vct-name>-<sts-name>-".
func vctMappingOverlap(sts *appsv1.StatefulSet, pvcMapping map[string]string) bool {
	for _, vct := range sts.Spec.VolumeClaimTemplates {
		prefix := vct.Name + "-" + sts.Name + "-"
		for key := range pvcMapping {
			if strings.HasPrefix(key, prefix) {
				return true
			}
		}
	}
	return false
}

// rewriteVolumes rewrites PVC claim names in a pod spec according to the given mapping.
// Returns true if any volumes were changed.
func rewriteVolumes(podSpec *corev1.PodSpec, pvcMapping map[string]string) bool {
	changed := false
	for i := range podSpec.Volumes {
		vol := &podSpec.Volumes[i]
		if vol.PersistentVolumeClaim != nil {
			if newName, ok := pvcMapping[vol.PersistentVolumeClaim.ClaimName]; ok {
				vol.PersistentVolumeClaim.ClaimName = newName
				changed = true
			}
		}
	}
	return changed
}

// buildStandbyPVCName constructs the standby PVC name from the source PVC name.
func buildStandbyPVCName(sourcePVCName string) string {
	return sourcePVCName + standbyPVCSuffix
}

// isStandbyPVCName checks if a PVC name follows the standby naming convention.
func isStandbyPVCName(name string) bool {
	return strings.HasSuffix(name, standbyPVCSuffix)
}

// getSourcePVCName extracts the original source PVC name from a standby PVC name.
func getSourcePVCName(standbyName string) string {
	return strings.TrimSuffix(standbyName, standbyPVCSuffix)
}

// validateStandbyPVCsForCutover checks that standby PVCs exist for all source PVCs
// used by workloads and that they are bound and have been restored.
func validateStandbyPVCsForCutover(
	ctx context.Context,
	sourceClient kubernetes.Interface,
	destClient kubernetes.Interface,
	sourceNamespace string,
	destNamespace string,
) ([]StandbyPVCInfo, error) {
	log := logging.SetupLogging()

	// List standby PVCs.
	standbyPVCs, err := listStandbyPVCs(ctx, destClient, destNamespace)
	if err != nil {
		return nil, err
	}

	if len(standbyPVCs) == 0 {
		return nil, fmt.Errorf("no standby PVCs found in namespace %s; ensure the controller has backup-based sync enabled", destNamespace)
	}

	// Validate each standby PVC.
	var errors []string
	for _, spvc := range standbyPVCs {
		if spvc.Phase != corev1.ClaimBound {
			errors = append(errors, fmt.Sprintf("standby PVC %s is not bound (phase: %s)", spvc.Name, spvc.Phase))
		}
		if spvc.LastRestoreTime == nil {
			errors = append(errors, fmt.Sprintf("standby PVC %s has never been restored", spvc.Name))
		}
	}

	if len(errors) > 0 {
		log.Warnf("Standby PVC validation warnings: %s", strings.Join(errors, "; "))
	}

	return standbyPVCs, nil
}

// getWorkloadsUsingPVCs returns a list of deployments and statefulsets that reference PVCs.
func getWorkloadsUsingPVCs(ctx context.Context, client kubernetes.Interface, namespace string) ([]appsv1.Deployment, []appsv1.StatefulSet, error) {
	deployments, err := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("list deployments: %v", err)
	}

	statefulsets, err := client.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("list statefulsets: %v", err)
	}

	var pvcDeps []appsv1.Deployment
	for _, dep := range deployments.Items {
		if podSpecHasPVCs(&dep.Spec.Template.Spec) {
			pvcDeps = append(pvcDeps, dep)
		}
	}

	var pvcSts []appsv1.StatefulSet
	for _, sts := range statefulsets.Items {
		if podSpecHasPVCs(&sts.Spec.Template.Spec) || hasVolumeClaimTemplates(&sts) {
			pvcSts = append(pvcSts, sts)
		}
	}

	return pvcDeps, pvcSts, nil
}

// podSpecHasPVCs checks if a pod spec references any PVCs.
func podSpecHasPVCs(podSpec *corev1.PodSpec) bool {
	for _, vol := range podSpec.Volumes {
		if vol.PersistentVolumeClaim != nil {
			return true
		}
	}
	return false
}
