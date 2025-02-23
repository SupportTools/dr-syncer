package syncer

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	drv1alpha1 "github.com/supporttools/dr-syncer/pkg/api/v1alpha1"
	"github.com/supporttools/dr-syncer/pkg/controllers/utils"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// DeploymentScale represents a deployment's scale information
type DeploymentScale struct {
	Name      string
	Replicas  int32
	SyncTime  metav1.Time
}

// ResourceSyncer handles syncing resources between clusters
type ResourceSyncer struct {
	ctrlClient       client.Client
	sourceDynamic    dynamic.Interface
	destDynamic      dynamic.Interface
	sourceClient     kubernetes.Interface
	destClient       kubernetes.Interface
}

// NewResourceSyncer creates a new resource syncer
func NewResourceSyncer(ctrlClient client.Client, sourceDynamic, destDynamic dynamic.Interface, sourceClient, destClient kubernetes.Interface) *ResourceSyncer {
	return &ResourceSyncer{
		ctrlClient:    ctrlClient,
		sourceDynamic: sourceDynamic,
		destDynamic:   destDynamic,
		sourceClient:  sourceClient,
		destClient:    destClient,
	}
}

// EnsureNamespaceExists ensures the destination namespace exists
func EnsureNamespaceExists(ctx context.Context, client kubernetes.Interface, dstNamespace, srcNamespace string) error {
	log := log.FromContext(ctx)
	log.V(1).Info("ensuring namespace exists",
		"namespace", dstNamespace,
		"sourceNamespace", srcNamespace)

	_, err := client.CoreV1().Namespaces().Get(ctx, dstNamespace, metav1.GetOptions{})
	if err == nil {
		log.V(1).Info("namespace already exists", "namespace", dstNamespace)
		return nil
	}

	// Create namespace if it doesn't exist
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: dstNamespace,
			Labels: map[string]string{
				"dr-syncer.io/source-namespace": srcNamespace,
			},
		},
	}

	_, err = client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		log.Error(err, "failed to create namespace", "namespace", dstNamespace)
		return fmt.Errorf("failed to create namespace %s: %w", dstNamespace, err)
	}

	log.V(1).Info("created namespace", "namespace", dstNamespace)
	return nil
}

// SyncNamespaceResources synchronizes resources between source and destination namespaces
func SyncNamespaceResources(ctx context.Context, sourceClient, destClient kubernetes.Interface, sourceDynamic, destDynamic dynamic.Interface, ctrlClient client.Client, srcNamespace, dstNamespace string, resourceTypes []string, scaleToZero bool, namespaceScopedResources []string, pvcConfig *drv1alpha1.PVCConfig, immutableConfig *drv1alpha1.ImmutableResourceConfig) ([]DeploymentScale, error) {
	var deploymentScales []DeploymentScale
	log := log.FromContext(ctx)

	// If no resource types specified, use defaults
	if len(resourceTypes) == 0 {
		resourceTypes = []string{"configmaps", "secrets", "deployments", "services", "ingresses", "persistentvolumeclaims"}
	}

	log.V(1).Info("creating resource syncer",
		"sourceNS", srcNamespace,
		"destNS", dstNamespace)

	// Create resource syncer using the passed-in clients
	syncer := NewResourceSyncer(ctrlClient, sourceDynamic, destDynamic, sourceClient, destClient)

	// Get or create namespace in source cluster
	sourceNS, err := sourceClient.CoreV1().Namespaces().Get(ctx, srcNamespace, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Create source namespace
			newSourceNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: srcNamespace,
					Labels: map[string]string{
						"dr-syncer.io/managed-by": "dr-syncer",
					},
				},
			}
			sourceNS, err = sourceClient.CoreV1().Namespaces().Create(ctx, newSourceNS, metav1.CreateOptions{})
			if err != nil {
				return nil, fmt.Errorf("failed to create source namespace: %w", err)
			}
			log.V(1).Info("created source namespace", "namespace", srcNamespace)
		} else {
			return nil, fmt.Errorf("failed to get source namespace: %w", err)
		}
	}

	// Create namespace in destination cluster if it doesn't exist
	_, err = destClient.CoreV1().Namespaces().Get(ctx, dstNamespace, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Create new namespace with same labels and annotations
			newNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:        dstNamespace,
					Labels:      sourceNS.Labels,
					Annotations: sourceNS.Annotations,
				},
			}
			// Add source namespace label
			if newNS.Labels == nil {
				newNS.Labels = make(map[string]string)
			}
			newNS.Labels["dr-syncer.io/source-namespace"] = srcNamespace

			_, err = destClient.CoreV1().Namespaces().Create(ctx, newNS, metav1.CreateOptions{})
			if err != nil {
				return nil, fmt.Errorf("failed to create destination namespace: %w", err)
			}
			log.V(1).Info("created destination namespace", "namespace", dstNamespace)
		} else {
			return nil, fmt.Errorf("failed to get destination namespace: %w", err)
		}
	}

	log.V(1).Info("starting resource sync",
		"types", resourceTypes,
		"scaleToZero", scaleToZero,
		"namespaceScopedResources", namespaceScopedResources,
		"sourceNamespace", srcNamespace,
		"destinationNamespace", dstNamespace,
		"sourceCluster", sourceClient.Discovery().RESTClient().Get().URL().Host,
		"destinationCluster", destClient.Discovery().RESTClient().Get().URL().Host)

	// Sync standard resource types
	for _, resourceType := range resourceTypes {
		// Normalize resource type to lowercase
		rtLower := strings.ToLower(resourceType)
		log.V(1).Info("processing resource type",
			"type", resourceType,
			"normalized", rtLower,
			"sourceNamespace", srcNamespace,
			"destinationNamespace", dstNamespace,
			"sourceCluster", sourceClient.Discovery().RESTClient().Get().URL().Host,
			"destinationCluster", destClient.Discovery().RESTClient().Get().URL().Host)

		switch rtLower {
		case "configmaps", "configmap":
			if err := syncConfigMaps(ctx, syncer, sourceClient, srcNamespace, dstNamespace, immutableConfig); err != nil {
				log.Error(err, "failed to sync ConfigMaps",
					"sourceNamespace", srcNamespace,
					"destinationNamespace", dstNamespace,
					"sourceCluster", sourceClient.Discovery().RESTClient().Get().URL().Host,
					"destinationCluster", destClient.Discovery().RESTClient().Get().URL().Host)
			}
		case "secrets", "secret":
			if err := syncSecrets(ctx, syncer, sourceClient, srcNamespace, dstNamespace, immutableConfig); err != nil {
				log.Error(err, "failed to sync Secrets",
					"sourceNamespace", srcNamespace,
					"destinationNamespace", dstNamespace,
					"sourceCluster", sourceClient.Discovery().RESTClient().Get().URL().Host,
					"destinationCluster", destClient.Discovery().RESTClient().Get().URL().Host)
			}
		case "deployments", "deployment":
			scales, err := syncDeployments(ctx, syncer, sourceClient, srcNamespace, dstNamespace, scaleToZero, immutableConfig)
			if err != nil {
				log.Error(err, "failed to sync Deployments",
					"sourceNamespace", srcNamespace,
					"destinationNamespace", dstNamespace,
					"sourceCluster", sourceClient.Discovery().RESTClient().Get().URL().Host,
					"destinationCluster", destClient.Discovery().RESTClient().Get().URL().Host)
			} else {
				deploymentScales = append(deploymentScales, scales...)
			}
		case "services", "service":
			if err := syncServices(ctx, syncer, sourceClient, srcNamespace, dstNamespace, immutableConfig); err != nil {
				log.Error(err, "failed to sync Services",
					"sourceNamespace", srcNamespace,
					"destinationNamespace", dstNamespace,
					"sourceCluster", sourceClient.Discovery().RESTClient().Get().URL().Host,
					"destinationCluster", destClient.Discovery().RESTClient().Get().URL().Host)
			}
		case "ingresses", "ingress":
			if err := syncIngresses(ctx, syncer, sourceClient, srcNamespace, dstNamespace, immutableConfig); err != nil {
				log.Error(err, "failed to sync Ingresses",
					"sourceNamespace", srcNamespace,
					"destinationNamespace", dstNamespace,
					"sourceCluster", sourceClient.Discovery().RESTClient().Get().URL().Host,
					"destinationCluster", destClient.Discovery().RESTClient().Get().URL().Host)
			}
		case "persistentvolumeclaims", "persistentvolumeclaim", "pvc":
			if err := syncPersistentVolumeClaims(ctx, syncer, sourceClient, srcNamespace, dstNamespace, pvcConfig, immutableConfig); err != nil {
				log.Error(err, "failed to sync PVCs",
					"sourceNamespace", srcNamespace,
					"destinationNamespace", dstNamespace,
					"sourceCluster", sourceClient.Discovery().RESTClient().Get().URL().Host,
					"destinationCluster", destClient.Discovery().RESTClient().Get().URL().Host)
			}
		}
	}

	// Sync namespace scoped resources
	if len(namespaceScopedResources) == 1 && namespaceScopedResources[0] == "*" {
		// Get all API resources from the source cluster
		groups, err := sourceClient.Discovery().ServerGroups()
		if err != nil {
			log.Error(err, "failed to get API groups",
				"sourceNS", srcNamespace,
				"destNS", dstNamespace)
		} else {
			for _, group := range groups.Groups {
				for _, version := range group.Versions {
					groupVersion := version.GroupVersion
					resources, err := sourceClient.Discovery().ServerResourcesForGroupVersion(groupVersion)
					if err != nil {
						log.Error(err, "failed to get resources",
							"groupVersion", groupVersion,
							"sourceNS", srcNamespace,
							"destNS", dstNamespace)
						continue
					}

					for _, r := range resources.APIResources {
						// Only sync namespaced resources that are not built-in types
						if r.Namespaced && !isBuiltInResource(r.Name) {
							if err := syncer.syncNamespaceScopedResource(ctx, sourceClient, destClient, srcNamespace, dstNamespace, r.Name, group.Name); err != nil {
								log.Error(err, "failed to sync namespace scoped resource",
									"resource", r.Name,
									"group", group.Name,
									"sourceNS", srcNamespace,
									"destNS", dstNamespace)
							}
						}
					}
				}
			}
		}
	} else {
		// Sync specific resources
		for _, resourceRef := range namespaceScopedResources {
			parts := strings.Split(resourceRef, ".")
			if len(parts) < 2 {
				log.Error(fmt.Errorf("invalid resource reference format"),
					"resource reference must be in format 'resource.group'",
					"reference", resourceRef,
					"sourceNS", srcNamespace,
					"destNS", dstNamespace)
				continue
			}

			resource := parts[0]
			group := strings.Join(parts[1:], ".")

			if err := syncer.syncNamespaceScopedResource(ctx, sourceClient, destClient, srcNamespace, dstNamespace, resource, group); err != nil {
				log.Error(err, "failed to sync namespace scoped resource",
					"resource", resource,
					"group", group,
					"sourceNS", srcNamespace,
					"destNS", dstNamespace)
			}
		}
	}

	return deploymentScales, nil
}

// isBuiltInResource checks if a resource is a built-in Kubernetes resource
func isBuiltInResource(name string) bool {
	// Normalize name to lowercase
	nameLower := strings.ToLower(name)

	// Map of built-in resources with common variations
	builtInResources := map[string]bool{
		"configmaps":             true,
		"configmap":             true,
		"secrets":               true,
		"secret":               true,
		"deployments":           true,
		"deployment":           true,
		"services":              true,
		"service":              true,
		"ingresses":             true,
		"ingress":             true,
		"pods":                  true,
		"pod":                  true,
		"events":                true,
		"event":                true,
		"endpoints":             true,
		"endpoint":             true,
		"persistentvolumeclaims": true,
		"persistentvolumeclaim": true,
		"pvc":                   true,
		"persistentvolumes":      true,
		"persistentvolume":      true,
		"pv":                    true,
	}
	return builtInResources[nameLower]
}

// syncNamespaceScopedResource synchronizes a specific namespace scoped resource
func (r *ResourceSyncer) syncNamespaceScopedResource(ctx context.Context, sourceClient, destClient kubernetes.Interface, srcNamespace, dstNamespace, resource, group string) error {
	log := log.FromContext(ctx)

	// Get the resource from the source cluster
	sourceResources, err := sourceClient.Discovery().ServerResourcesForGroupVersion(group + "/v1")
	if err != nil {
		return fmt.Errorf("failed to get resources for group %s: %v", group, err)
	}

	var resourceFound bool
	for _, r := range sourceResources.APIResources {
		if r.Name == resource && r.Namespaced {
			resourceFound = true
			break
		}
	}

	if !resourceFound {
		return fmt.Errorf("resource %s not found in group %s or not namespaced", resource, group)
	}

	// Create GVR for the resource
	gvr := schema.GroupVersionResource{
		Group:    group,
		Version:  "v1",
		Resource: resource,
	}

	// List resources in source namespace
	sourceList, err := r.sourceDynamic.Resource(gvr).Namespace(srcNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list %s in source namespace: %v", resource, err)
	}

	// Convert to unstructured list
	var items []unstructured.Unstructured
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(sourceList.UnstructuredContent(), &items); err != nil {
		return fmt.Errorf("failed to convert source list: %v", err)
	}

	// Process each resource
	for _, item := range items {
		if utils.ShouldIgnoreResource(&item) {
			continue
		}

		// Prepare resource for destination
		item.SetNamespace(dstNamespace)
		utils.SanitizeMetadata(&item)

		// Check if resource exists in destination
		existing, err := r.destDynamic.Resource(gvr).Namespace(dstNamespace).Get(ctx, item.GetName(), metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				// Create resource
				_, err = r.destDynamic.Resource(gvr).Namespace(dstNamespace).Create(ctx, &item, metav1.CreateOptions{})
				if err != nil {
					log.Error(err, "Failed to create resource",
					"resource", resource,
					"name", item.GetName(),
					"sourceNamespace", srcNamespace,
					"destinationNamespace", dstNamespace,
					"sourceCluster", sourceClient.Discovery().RESTClient().Get().URL().Host,
					"destinationCluster", destClient.Discovery().RESTClient().Get().URL().Host)
					continue
				}
				log.Info("Created resource", "resource", resource, "name", item.GetName())
			} else {
				log.Error(err, "Failed to get resource",
					"resource", resource,
					"name", item.GetName(),
					"sourceNamespace", srcNamespace,
					"destinationNamespace", dstNamespace,
					"sourceCluster", sourceClient.Discovery().RESTClient().Get().URL().Host,
					"destinationCluster", destClient.Discovery().RESTClient().Get().URL().Host)
				continue
			}
		} else {
			// Update resource if needed
			if !reflect.DeepEqual(item.Object, existing.Object) {
				item.SetResourceVersion(existing.GetResourceVersion())
				_, err = r.destDynamic.Resource(gvr).Namespace(dstNamespace).Update(ctx, &item, metav1.UpdateOptions{})
				if err != nil {
					log.Error(err, "Failed to update resource",
					"resource", resource,
					"name", item.GetName(),
					"sourceNamespace", srcNamespace,
					"destinationNamespace", dstNamespace,
					"sourceCluster", sourceClient.Discovery().RESTClient().Get().URL().Host,
					"destinationCluster", destClient.Discovery().RESTClient().Get().URL().Host)
					continue
				}
				log.Info("Updated resource", "resource", resource, "name", item.GetName())
			}
		}
	}

	return nil
}

// SyncResource syncs a single resource between clusters
func (r *ResourceSyncer) SyncResource(ctx context.Context, obj runtime.Object, config *drv1alpha1.ImmutableResourceConfig) error {
	clientObj, ok := obj.(client.Object)
	if !ok {
		return fmt.Errorf("object does not implement client.Object")
	}

	// Get current resource in destination cluster
	current := obj.DeepCopyObject().(client.Object)
	key := client.ObjectKeyFromObject(clientObj)
	err := r.ctrlClient.Get(ctx, key, current)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to get current resource: %w", err)
		}
		// Resource doesn't exist, create it
		return r.ctrlClient.Create(ctx, clientObj)
	}

	// Regular update for mutable resources or no immutable field changes
	return r.ctrlClient.Update(ctx, clientObj)
}
