package cli

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/supporttools/dr-syncer/pkg/logging"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// Annotation keys
const (
	OriginalReplicasAnnotation = "dr-syncer.io/original-replicas"
)

// executeStageModeSync handles the Stage mode operation:
// 1. Synchronize resources from source to destination
// 2. Scale down deployments in destination
func executeStageModeSync(
	ctx context.Context,
	sourceClient kubernetes.Interface,
	destClient kubernetes.Interface,
	sourceDynamicClient dynamic.Interface,
	destDynamicClient dynamic.Interface,
	config *Config,
) error {
	log := logging.SetupLogging()
	log.Info("Executing Stage mode sync")

	// Sync resources from source to destination
	if err := syncResources(ctx, sourceClient, destClient, sourceDynamicClient, destDynamicClient, config); err != nil {
		return fmt.Errorf("failed to sync resources: %v", err)
	}

	// Scale down deployments in destination
	log.Info("Scaling down deployments in destination")
	if err := scaleDeployments(ctx, destClient, config.DestNamespace, 0); err != nil {
		return fmt.Errorf("failed to scale down deployments in destination: %v", err)
	}

	// Handle PVC data migration if enabled
	if config.MigratePVCData {
		log.Info("PVC data migration is enabled")
		if err := migratePVCData(ctx, sourceClient, destClient, config); err != nil {
			return fmt.Errorf("failed to migrate PVC data: %v", err)
		}
	}

	log.Info("Stage mode sync completed successfully")
	return nil
}

// executeCutoverModeSync handles the Cutover mode operation:
// 1. Synchronize resources from source to destination
// 2. Scale down deployments in source
// 3. Scale up deployments in destination
func executeCutoverModeSync(
	ctx context.Context,
	sourceClient kubernetes.Interface,
	destClient kubernetes.Interface,
	sourceDynamicClient dynamic.Interface,
	destDynamicClient dynamic.Interface,
	config *Config,
) error {
	log := logging.SetupLogging()
	log.Info("Executing Cutover mode sync")

	// Sync resources from source to destination
	if err := syncResources(ctx, sourceClient, destClient, sourceDynamicClient, destDynamicClient, config); err != nil {
		return fmt.Errorf("failed to sync resources: %v", err)
	}

	// Annotate source deployments with original replica counts before scaling down
	log.Info("Annotating source deployments with original replica counts")
	if err := annotateOriginalReplicas(ctx, sourceClient, config.SourceNamespace); err != nil {
		return fmt.Errorf("failed to annotate original replicas: %v", err)
	}

	// Scale down deployments in source
	log.Info("Scaling down deployments in source")
	if err := scaleDeployments(ctx, sourceClient, config.SourceNamespace, 0); err != nil {
		return fmt.Errorf("failed to scale down deployments in source: %v", err)
	}

	// Scale up deployments in destination (based on source replica counts)
	log.Info("Scaling up deployments in destination")
	if err := restoreDeploymentScales(ctx, sourceClient, destClient, config.SourceNamespace, config.DestNamespace); err != nil {
		return fmt.Errorf("failed to scale up deployments in destination: %v", err)
	}

	// Handle final PVC data migration if enabled
	if config.MigratePVCData {
		log.Info("PVC data migration is enabled")
		if err := migratePVCData(ctx, sourceClient, destClient, config); err != nil {
			return fmt.Errorf("failed to migrate PVC data: %v", err)
		}
	}

	log.Info("Cutover mode sync completed successfully")
	return nil
}

// executeFailbackModeSync handles the Failback mode operation:
// 1. Optionally reverse sync specific resources
// 2. Scale down deployments in destination
// 3. Scale up deployments in source
func executeFailbackModeSync(
	ctx context.Context,
	sourceClient kubernetes.Interface,
	destClient kubernetes.Interface,
	sourceDynamicClient dynamic.Interface,
	destDynamicClient dynamic.Interface,
	config *Config,
) error {
	log := logging.SetupLogging()
	log.Info("Executing Failback mode sync")

	// Optionally reverse migrate PVC data
	if config.ReverseMigratePVCData {
		log.Info("Reverse PVC data migration is enabled")
		if err := migratePVCData(ctx, destClient, sourceClient, &Config{
			SourceKubeconfig: config.DestKubeconfig,
			DestKubeconfig:   config.SourceKubeconfig,
			SourceNamespace:  config.DestNamespace,
			DestNamespace:    config.SourceNamespace,
			MigratePVCData:   true,
			PVMigrateFlags:   config.PVMigrateFlags, // Pass the PV migrate flags to reverse migration
		}); err != nil {
			return fmt.Errorf("failed to reverse migrate PVC data: %v", err)
		}
	}

	// Scale down deployments in destination
	log.Info("Scaling down deployments in destination")
	if err := scaleDeployments(ctx, destClient, config.DestNamespace, 0); err != nil {
		return fmt.Errorf("failed to scale down deployments in destination: %v", err)
	}

	// Scale up deployments in source (restore original replica counts)
	log.Info("Scaling up deployments in source")
	if err := restoreOriginalReplicas(ctx, sourceClient, config.SourceNamespace); err != nil {
		return fmt.Errorf("failed to scale up deployments in source: %v", err)
	}

	log.Info("Failback mode sync completed successfully")
	return nil
}

// syncResources synchronizes resources from source to destination
func syncResources(
	ctx context.Context,
	sourceClient kubernetes.Interface,
	destClient kubernetes.Interface,
	sourceDynamicClient dynamic.Interface,
	destDynamicClient dynamic.Interface,
	config *Config,
) error {
	log := logging.SetupLogging()
	log.Info("Discovering resources in source namespace")

	// Get API resources
	apiResources, err := sourceClient.Discovery().ServerPreferredResources()
	if err != nil {
		return fmt.Errorf("failed to get API resources: %v", err)
	}

	// Process API resources
	for _, resourceList := range apiResources {
		gv, err := schema.ParseGroupVersion(resourceList.GroupVersion)
		if err != nil {
			// Skip unparseable group versions
			continue
		}

		for _, resource := range resourceList.APIResources {
			// Skip if resource is not namespaced
			if !resource.Namespaced {
				continue
			}

			// Skip subresources (contains /)
			if resource.Name != resource.SingularName && resource.SingularName != "" {
				continue
			}

			// Check if this resource type should be synced
			isCustomResource := false
			if gv.Group != "" && gv.Group != "apps" && gv.Group != "batch" &&
				gv.Group != "extensions" && gv.Group != "networking.k8s.io" {
				isCustomResource = true
			}

			if !config.ShouldSyncResourceType(resource.Name, isCustomResource) {
				log.Infof("Skipping resource type %s (not in sync list)", resource.Name)
				continue
			}

			gvr := schema.GroupVersionResource{
				Group:    gv.Group,
				Version:  gv.Version,
				Resource: resource.Name,
			}

			log.Infof("Processing resource type: %s", resource.Name)

			// Get resources from source namespace
			resourceList, err := sourceDynamicClient.Resource(gvr).Namespace(config.SourceNamespace).List(ctx, metav1.ListOptions{})
			if err != nil {
				log.Warnf("Failed to list resources of type %s: %v", resource.Name, err)
				continue
			}

			// Process each resource
			for _, item := range resourceList.Items {
				log.Infof("Processing resource: %s/%s", item.GetKind(), item.GetName())

				// Transform resource for destination
				transformedResource, err := transformResource(&item, config.DestNamespace)
				if err != nil {
					log.Warnf("Failed to transform resource %s/%s: %v", item.GetKind(), item.GetName(), err)
					continue
				}

				// Apply resource to destination
				_, err = destDynamicClient.Resource(gvr).Namespace(config.DestNamespace).Create(ctx, transformedResource, metav1.CreateOptions{})
				if err != nil {
					// Check if error is "already exists"
					statusErr, ok := err.(interface {
						Status() interface {
							Reason() string
						}
					})
					if !ok || statusErr.Status().Reason() != "AlreadyExists" {
						log.Warnf("Failed to create resource %s/%s: %v", item.GetKind(), item.GetName(), err)
						continue
					}

					// Resource already exists, update it
					_, err = destDynamicClient.Resource(gvr).Namespace(config.DestNamespace).Update(ctx, transformedResource, metav1.UpdateOptions{})
					if err != nil {
						log.Warnf("Failed to update resource %s/%s: %v", item.GetKind(), item.GetName(), err)
						continue
					}
				}

				log.Infof("Successfully synced resource: %s/%s", item.GetKind(), item.GetName())
			}
		}
	}

	return nil
}

// transformResource transforms a resource for the destination cluster
func transformResource(resource *unstructured.Unstructured, destNamespace string) (*unstructured.Unstructured, error) {
	transformed := resource.DeepCopy()

	// Common transformations for all resources
	transformed.SetNamespace(destNamespace)
	transformed.SetResourceVersion("")
	transformed.SetUID("")
	transformed.SetCreationTimestamp(metav1.Time{})
	transformed.SetSelfLink("")
	transformed.SetManagedFields(nil)

	// Remove status
	transformed.Object["status"] = nil

	// Handle specific resource types
	switch resource.GetKind() {
	case "Service":
		handleServiceTransform(transformed)
	case "Deployment":
		handleDeploymentTransform(transformed)
	case "StatefulSet":
		handleStatefulSetTransform(transformed)
	case "Ingress":
		handleIngressTransform(transformed)
	}

	return transformed, nil
}

// handleServiceTransform handles Service-specific transformations
func handleServiceTransform(service *unstructured.Unstructured) {
	// Remove cluster-specific fields
	unstructured.RemoveNestedField(service.Object, "spec", "clusterIP")
	unstructured.RemoveNestedField(service.Object, "spec", "clusterIPs")
	unstructured.RemoveNestedField(service.Object, "spec", "healthCheckNodePort")
	unstructured.RemoveNestedField(service.Object, "spec", "externalTrafficPolicy")
	unstructured.RemoveNestedField(service.Object, "spec", "ipFamilyPolicy")
	unstructured.RemoveNestedField(service.Object, "spec", "ipFamilies")
	unstructured.RemoveNestedField(service.Object, "spec", "allocateLoadBalancerNodePorts")
	unstructured.RemoveNestedField(service.Object, "spec", "loadBalancerIP")
	unstructured.RemoveNestedField(service.Object, "spec", "loadBalancerSourceRanges")
	unstructured.RemoveNestedField(service.Object, "spec", "externalIPs")
	unstructured.RemoveNestedField(service.Object, "spec", "sessionAffinity")
	unstructured.RemoveNestedField(service.Object, "spec", "sessionAffinityConfig")
}

// handleDeploymentTransform handles Deployment-specific transformations
func handleDeploymentTransform(deployment *unstructured.Unstructured) {
	// Store original replicas in an annotation
	replicas, found, _ := unstructured.NestedInt64(deployment.Object, "spec", "replicas")
	if found {
		annotations := deployment.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}
		annotations[OriginalReplicasAnnotation] = fmt.Sprintf("%d", replicas)
		deployment.SetAnnotations(annotations)
	}

	// Remove owner references
	deployment.SetOwnerReferences(nil)
}

// handleStatefulSetTransform handles StatefulSet-specific transformations
func handleStatefulSetTransform(statefulSet *unstructured.Unstructured) {
	// Store original replicas in an annotation
	replicas, found, _ := unstructured.NestedInt64(statefulSet.Object, "spec", "replicas")
	if found {
		annotations := statefulSet.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}
		annotations[OriginalReplicasAnnotation] = fmt.Sprintf("%d", replicas)
		statefulSet.SetAnnotations(annotations)
	}

	// Remove volumeClaimTemplates status
	volumeClaimTemplates, found, _ := unstructured.NestedSlice(statefulSet.Object, "spec", "volumeClaimTemplates")
	if found {
		for i := range volumeClaimTemplates {
			template, ok := volumeClaimTemplates[i].(map[string]interface{})
			if ok {
				delete(template, "status")
			}
		}
		unstructured.SetNestedSlice(statefulSet.Object, volumeClaimTemplates, "spec", "volumeClaimTemplates")
	}

	// Remove owner references
	statefulSet.SetOwnerReferences(nil)
}

// handleIngressTransform handles Ingress-specific transformations
func handleIngressTransform(ingress *unstructured.Unstructured) {
	// Remove ingress class if it's set via annotation for older K8s versions
	annotations := ingress.GetAnnotations()
	if annotations != nil {
		if _, exists := annotations["kubernetes.io/ingress.class"]; exists {
			delete(annotations, "kubernetes.io/ingress.class")
			ingress.SetAnnotations(annotations)
		}
	}

	// Remove owner references
	ingress.SetOwnerReferences(nil)
}

// scaleDeployments scales all deployments in the namespace to the specified replica count
func scaleDeployments(ctx context.Context, client kubernetes.Interface, namespace string, replicas int32) error {
	log := logging.SetupLogging()

	// Scale deployments
	log.Infof("Scaling deployments in namespace %s to %d replicas", namespace, replicas)
	deployments, err := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list deployments: %v", err)
	}

	for _, deployment := range deployments.Items {
		log.Infof("Scaling deployment %s to %d replicas", deployment.Name, replicas)
		deployment.Spec.Replicas = &replicas
		_, err := client.AppsV1().Deployments(namespace).Update(ctx, &deployment, metav1.UpdateOptions{})
		if err != nil {
			log.Warnf("Failed to scale deployment %s: %v", deployment.Name, err)
			continue
		}
	}

	// Scale statefulsets
	log.Infof("Scaling statefulsets in namespace %s to %d replicas", namespace, replicas)
	statefulsets, err := client.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list statefulsets: %v", err)
	}

	for _, statefulset := range statefulsets.Items {
		log.Infof("Scaling statefulset %s to %d replicas", statefulset.Name, replicas)
		statefulset.Spec.Replicas = &replicas
		_, err := client.AppsV1().StatefulSets(namespace).Update(ctx, &statefulset, metav1.UpdateOptions{})
		if err != nil {
			log.Warnf("Failed to scale statefulset %s: %v", statefulset.Name, err)
			continue
		}
	}

	return nil
}

// annotateOriginalReplicas annotates deployments with their original replica counts
func annotateOriginalReplicas(ctx context.Context, client kubernetes.Interface, namespace string) error {
	log := logging.SetupLogging()

	// Annotate deployments
	log.Infof("Annotating deployments in namespace %s with original replica counts", namespace)
	deployments, err := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list deployments: %v", err)
	}

	for _, deployment := range deployments.Items {
		// Skip if already annotated
		if _, ok := deployment.Annotations[OriginalReplicasAnnotation]; ok {
			continue
		}

		// Add annotation with original replica count
		if deployment.Annotations == nil {
			deployment.Annotations = make(map[string]string)
		}

		replicas := int64(0)
		if deployment.Spec.Replicas != nil {
			replicas = int64(*deployment.Spec.Replicas)
		}

		deployment.Annotations[OriginalReplicasAnnotation] = fmt.Sprintf("%d", replicas)

		// Update deployment
		_, err := client.AppsV1().Deployments(namespace).Update(ctx, &deployment, metav1.UpdateOptions{})
		if err != nil {
			log.Warnf("Failed to annotate deployment %s: %v", deployment.Name, err)
			continue
		}

		log.Infof("Annotated deployment %s with original replicas: %d", deployment.Name, replicas)
	}

	// Annotate statefulsets
	log.Infof("Annotating statefulsets in namespace %s with original replica counts", namespace)
	statefulsets, err := client.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list statefulsets: %v", err)
	}

	for _, statefulset := range statefulsets.Items {
		// Skip if already annotated
		if _, ok := statefulset.Annotations[OriginalReplicasAnnotation]; ok {
			continue
		}

		// Add annotation with original replica count
		if statefulset.Annotations == nil {
			statefulset.Annotations = make(map[string]string)
		}

		replicas := int64(0)
		if statefulset.Spec.Replicas != nil {
			replicas = int64(*statefulset.Spec.Replicas)
		}

		statefulset.Annotations[OriginalReplicasAnnotation] = fmt.Sprintf("%d", replicas)

		// Update statefulset
		_, err := client.AppsV1().StatefulSets(namespace).Update(ctx, &statefulset, metav1.UpdateOptions{})
		if err != nil {
			log.Warnf("Failed to annotate statefulset %s: %v", statefulset.Name, err)
			continue
		}

		log.Infof("Annotated statefulset %s with original replicas: %d", statefulset.Name, replicas)
	}

	return nil
}

// restoreDeploymentScales restores deployment replica counts to destination based on source
func restoreDeploymentScales(
	ctx context.Context,
	sourceClient kubernetes.Interface,
	destClient kubernetes.Interface,
	sourceNamespace string,
	destNamespace string,
) error {
	log := logging.SetupLogging()

	// Restore deployments
	log.Infof("Restoring deployment scales from namespace %s to %s", sourceNamespace, destNamespace)
	deployments, err := sourceClient.AppsV1().Deployments(sourceNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list deployments in source: %v", err)
	}

	for _, deployment := range deployments.Items {
		// Get annotation with original replica count
		replicaStr, ok := deployment.Annotations[OriginalReplicasAnnotation]
		if !ok {
			log.Warnf("Deployment %s has no original replicas annotation, skipping", deployment.Name)
			continue
		}

		var replicas int64
		_, err := fmt.Sscanf(replicaStr, "%d", &replicas)
		if err != nil {
			log.Warnf("Failed to parse original replicas annotation for deployment %s: %v", deployment.Name, err)
			continue
		}

		// Update destination deployment
		destDeployment, err := destClient.AppsV1().Deployments(destNamespace).Get(ctx, deployment.Name, metav1.GetOptions{})
		if err != nil {
			log.Warnf("Failed to get destination deployment %s: %v", deployment.Name, err)
			continue
		}

		replicasInt32 := int32(replicas)
		destDeployment.Spec.Replicas = &replicasInt32

		_, err = destClient.AppsV1().Deployments(destNamespace).Update(ctx, destDeployment, metav1.UpdateOptions{})
		if err != nil {
			log.Warnf("Failed to update destination deployment %s: %v", deployment.Name, err)
			continue
		}

		log.Infof("Restored deployment %s to %d replicas", deployment.Name, replicas)
	}

	// Restore statefulsets
	log.Infof("Restoring statefulset scales from namespace %s to %s", sourceNamespace, destNamespace)
	statefulsets, err := sourceClient.AppsV1().StatefulSets(sourceNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list statefulsets in source: %v", err)
	}

	for _, statefulset := range statefulsets.Items {
		// Get annotation with original replica count
		replicaStr, ok := statefulset.Annotations[OriginalReplicasAnnotation]
		if !ok {
			log.Warnf("StatefulSet %s has no original replicas annotation, skipping", statefulset.Name)
			continue
		}

		var replicas int64
		_, err := fmt.Sscanf(replicaStr, "%d", &replicas)
		if err != nil {
			log.Warnf("Failed to parse original replicas annotation for statefulset %s: %v", statefulset.Name, err)
			continue
		}

		// Update destination statefulset
		destStatefulset, err := destClient.AppsV1().StatefulSets(destNamespace).Get(ctx, statefulset.Name, metav1.GetOptions{})
		if err != nil {
			log.Warnf("Failed to get destination statefulset %s: %v", statefulset.Name, err)
			continue
		}

		replicasInt32 := int32(replicas)
		destStatefulset.Spec.Replicas = &replicasInt32

		_, err = destClient.AppsV1().StatefulSets(destNamespace).Update(ctx, destStatefulset, metav1.UpdateOptions{})
		if err != nil {
			log.Warnf("Failed to update destination statefulset %s: %v", statefulset.Name, err)
			continue
		}

		log.Infof("Restored statefulset %s to %d replicas", statefulset.Name, replicas)
	}

	return nil
}

// restoreOriginalReplicas restores deployments to their original replica counts
func restoreOriginalReplicas(ctx context.Context, client kubernetes.Interface, namespace string) error {
	log := logging.SetupLogging()

	// Restore deployments
	log.Infof("Restoring original deployment replicas in namespace %s", namespace)
	deployments, err := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list deployments: %v", err)
	}

	for _, deployment := range deployments.Items {
		// Get annotation with original replica count
		replicaStr, ok := deployment.Annotations[OriginalReplicasAnnotation]
		if !ok {
			log.Warnf("Deployment %s has no original replicas annotation, skipping", deployment.Name)
			continue
		}

		var replicas int64
		_, err := fmt.Sscanf(replicaStr, "%d", &replicas)
		if err != nil {
			log.Warnf("Failed to parse original replicas annotation for deployment %s: %v", deployment.Name, err)
			continue
		}

		// Update deployment
		replicasInt32 := int32(replicas)
		deployment.Spec.Replicas = &replicasInt32

		_, err = client.AppsV1().Deployments(namespace).Update(ctx, &deployment, metav1.UpdateOptions{})
		if err != nil {
			log.Warnf("Failed to restore deployment %s: %v", deployment.Name, err)
			continue
		}

		log.Infof("Restored deployment %s to original %d replicas", deployment.Name, replicas)
	}

	// Restore statefulsets
	log.Infof("Restoring original statefulset replicas in namespace %s", namespace)
	statefulsets, err := client.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list statefulsets: %v", err)
	}

	for _, statefulset := range statefulsets.Items {
		// Get annotation with original replica count
		replicaStr, ok := statefulset.Annotations[OriginalReplicasAnnotation]
		if !ok {
			log.Warnf("StatefulSet %s has no original replicas annotation, skipping", statefulset.Name)
			continue
		}

		var replicas int64
		_, err := fmt.Sscanf(replicaStr, "%d", &replicas)
		if err != nil {
			log.Warnf("Failed to parse original replicas annotation for statefulset %s: %v", statefulset.Name, err)
			continue
		}

		// Update statefulset
		replicasInt32 := int32(replicas)
		statefulset.Spec.Replicas = &replicasInt32

		_, err = client.AppsV1().StatefulSets(namespace).Update(ctx, &statefulset, metav1.UpdateOptions{})
		if err != nil {
			log.Warnf("Failed to restore statefulset %s: %v", statefulset.Name, err)
			continue
		}

		log.Infof("Restored statefulset %s to original %d replicas", statefulset.Name, replicas)
	}

	return nil
}

// migratePVCData migrates PVC data using pv-migrate
func migratePVCData(ctx context.Context, sourceClient kubernetes.Interface, destClient kubernetes.Interface, config *Config) error {
	log := logging.SetupLogging()

	// Check if pv-migrate is installed
	if !isPvMigrateAvailable() {
		return fmt.Errorf("pv-migrate not found in PATH, PVC data migration requires pv-migrate to be installed")
	}

	// Get PVCs from source namespace
	pvcs, err := sourceClient.CoreV1().PersistentVolumeClaims(config.SourceNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list PVCs in source namespace: %v", err)
	}

	// Check if we have any PVCs to migrate
	if len(pvcs.Items) == 0 {
		log.Info("No PVCs found in source namespace for migration")
		return nil
	}

	log.Infof("Found %d PVCs in source namespace for potential migration", len(pvcs.Items))

	// Migrate each PVC's data
	for _, pvc := range pvcs.Items {
		// Check if destination PVC exists
		_, err := destClient.CoreV1().PersistentVolumeClaims(config.DestNamespace).Get(ctx, pvc.Name, metav1.GetOptions{})
		if err != nil {
			log.Warnf("Destination PVC %s not found, skipping data migration for this PVC", pvc.Name)
			continue
		}

		log.Infof("Migrating data for PVC %s from %s to %s", pvc.Name, config.SourceNamespace, config.DestNamespace)

		// Use pv-migrate to transfer data
		err = pvMigrate(config.SourceKubeconfig, config.DestKubeconfig, config.SourceNamespace, config.DestNamespace, pvc.Name, pvc.Name, config.PVMigrateFlags)
		if err != nil {
			log.Warnf("Failed to migrate data for PVC %s: %v", pvc.Name, err)
			continue
		}

		log.Infof("Successfully migrated data for PVC %s", pvc.Name)
	}

	return nil
}

// isPvMigrateAvailable checks if pv-migrate is available in the PATH
func isPvMigrateAvailable() bool {
	// First try the most common flag format
	_, err := executeCommand("pv-migrate", "--version")
	if err == nil {
		return true
	}

	// Fall back to try the command directly (which should print help)
	_, err = executeCommand("pv-migrate")
	// Even if it returns an error (due to missing required args),
	// if the binary exists it will have exit code 1 or 2, not "command not found"
	if err != nil {
		// Check if it's a "command not found" error
		if exitErr, ok := err.(*exec.ExitError); ok {
			// If it's just an exit status error (like 1 or 2), the command exists
			return exitErr.ExitCode() > 0 && exitErr.ExitCode() < 127
		}
	}
	return err == nil
}

// pvMigrate uses pv-migrate to transfer data between PVCs
func pvMigrate(sourceKubeconfig, destKubeconfig, sourceNamespace, destNamespace, sourcePVC, destPVC, additionalFlags string) error {
	log := logging.SetupLogging()
	args := []string{
		"--source", sourcePVC,
		"--dest", destPVC,
		"--source-namespace", sourceNamespace,
		"--dest-namespace", destNamespace,
		"-k", sourceKubeconfig,
		"-K", destKubeconfig,
	}

	// If additional flags are provided, parse and add them
	if additionalFlags != "" {
		// Split string by spaces, but respect quoted arguments
		additionalArgs, err := parseCommandLineArgs(additionalFlags)
		if err != nil {
			return fmt.Errorf("failed to parse additional pv-migrate flags: %v", err)
		}
		args = append(args, additionalArgs...)
	}

	// Print PV migrate command being executed
	log.Infof("============ EXECUTING PV-MIGRATE ============")
	log.Infof("Command: pv-migrate %s", strings.Join(args, " "))

	// Execute command and get output
	output, err := executeCommand("pv-migrate", args...)

	// Display full output regardless of success/failure
	log.Infof("============ PV-MIGRATE OUTPUT ============")
	log.Infof("%s", output)
	log.Infof("============ PV-MIGRATE END ============")

	return err
}

// parseCommandLineArgs parses a command line string into separate arguments
// respecting quotes (both single and double)
func parseCommandLineArgs(cmd string) ([]string, error) {
	var args []string
	var currentArg string
	var inQuote bool
	var quoteChar rune

	for _, r := range cmd {
		switch {
		case r == '"' || r == '\'':
			if inQuote && r == quoteChar {
				// End of quoted section
				inQuote = false
			} else if !inQuote {
				// Start of quoted section
				inQuote = true
				quoteChar = r
			} else {
				// Quote character inside different type of quote - treat as normal char
				currentArg += string(r)
			}
		case r == ' ' && !inQuote:
			// Space outside quotes - arg delimiter
			if currentArg != "" {
				args = append(args, currentArg)
				currentArg = ""
			}
		default:
			currentArg += string(r)
		}
	}

	// Add the last argument if there is one
	if currentArg != "" {
		args = append(args, currentArg)
	}

	if inQuote {
		return nil, fmt.Errorf("unterminated quote in command line: %s", cmd)
	}

	return args, nil
}

// executeCommand executes a command with the given arguments
func executeCommand(command string, args ...string) (string, error) {
	log := logging.SetupLogging()

	log.Debugf("Executing command: %s %v", command, args)

	cmd := exec.Command(command, args...)
	output, err := cmd.CombinedOutput()

	if err != nil {
		log.Warnf("Command failed: %s %v: %v", command, args, err)
		if len(output) > 0 {
			log.Warnf("Command output: %s", string(output))
		}
		return "", fmt.Errorf("command failed: %v: %s", err, string(output))
	}

	return string(output), nil
}
