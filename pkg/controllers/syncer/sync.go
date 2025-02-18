package syncer

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	drv1alpha1 "github.com/supporttools/dr-syncer/pkg/api/v1alpha1"
	"github.com/supporttools/dr-syncer/pkg/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// EnsureNamespaceExists ensures that a namespace exists in the cluster
func EnsureNamespaceExists(ctx context.Context, client *kubernetes.Clientset, namespace, srcNamespace string) error {
	_, err := client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
					Labels: map[string]string{
						"dr-syncer.io":      "true",
						"dr-syncer.io/type": "destination",
					},
				},
			}
			_, err = client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create namespace %s: %v", namespace, err)
			}
			log.FromContext(ctx).Info("Created namespace", "namespace", namespace)
		} else {
			return fmt.Errorf("failed to get namespace %s: %v", namespace, err)
		}
	}
	return nil
}

// DeploymentScale represents the scale of a deployment
type DeploymentScale struct {
	Name     string
	Replicas int32
	SyncTime metav1.Time
}

// ResourceSyncer handles syncing resources between clusters
type ResourceSyncer struct {
	sourceClient kubernetes.Interface
	destClient   kubernetes.Interface
	ctrlClient   client.Client
	logger       logr.Logger
}

// NewResourceSyncer creates a new resource syncer
func NewResourceSyncer(sourceClient kubernetes.Interface, destClient kubernetes.Interface, ctrlClient client.Client) *ResourceSyncer {
	return &ResourceSyncer{
		sourceClient: sourceClient,
		destClient:   destClient,
		ctrlClient:   ctrlClient,
		logger:       log.Log.WithName("resource-syncer"),
	}
}

// SyncNamespaceResources synchronizes resources between source and destination namespaces
func SyncNamespaceResources(ctx context.Context, sourceClient, destClient kubernetes.Interface, ctrlClient client.Client, srcNamespace, dstNamespace string, resourceTypes []string, scaleToZero bool, namespaceScopedResources []string, pvcConfig *drv1alpha1.PVCConfig, immutableConfig *drv1alpha1.ImmutableResourceConfig) ([]DeploymentScale, error) {
	var deploymentScales []DeploymentScale
	log := log.FromContext(ctx)

	// If no resource types specified, use defaults
	if len(resourceTypes) == 0 {
		resourceTypes = []string{"configmaps", "secrets", "deployments", "services", "ingresses", "persistentvolumeclaims"}
	}

	// Create resource syncer
	syncer := NewResourceSyncer(sourceClient, destClient, ctrlClient)

	// Sync standard resource types
	for _, resourceType := range resourceTypes {
		switch resourceType {
		case "configmaps":
			if err := syncConfigMaps(ctx, syncer, sourceClient, srcNamespace, dstNamespace, immutableConfig); err != nil {
				log.Error(err, "Failed to sync ConfigMaps")
			}
		case "secrets":
			if err := syncSecrets(ctx, syncer, sourceClient, srcNamespace, dstNamespace, immutableConfig); err != nil {
				log.Error(err, "Failed to sync Secrets")
			}
		case "deployments":
			scales, err := syncDeployments(ctx, syncer, sourceClient, srcNamespace, dstNamespace, scaleToZero, immutableConfig)
			if err != nil {
				log.Error(err, "Failed to sync Deployments")
			} else {
				deploymentScales = scales
			}
		case "services":
			if err := syncServices(ctx, syncer, sourceClient, srcNamespace, dstNamespace, immutableConfig); err != nil {
				log.Error(err, "Failed to sync Services")
			}
		case "ingresses":
			if err := syncIngresses(ctx, syncer, sourceClient, srcNamespace, dstNamespace, immutableConfig); err != nil {
				log.Error(err, "Failed to sync Ingresses")
			}
		case "persistentvolumeclaims":
			if err := syncPersistentVolumeClaims(ctx, syncer, sourceClient, srcNamespace, dstNamespace, pvcConfig, immutableConfig); err != nil {
				log.Error(err, "Failed to sync PVCs")
			}
		}
	}

	// Sync namespace scoped resources
	if len(namespaceScopedResources) == 1 && namespaceScopedResources[0] == "*" {
		// Get all API resources from the source cluster
		groups, err := sourceClient.Discovery().ServerGroups()
		if err != nil {
			log.Error(err, "Failed to get API groups")
		} else {
			for _, group := range groups.Groups {
				for _, version := range group.Versions {
					groupVersion := version.GroupVersion
					resources, err := sourceClient.Discovery().ServerResourcesForGroupVersion(groupVersion)
					if err != nil {
						log.Error(err, "Failed to get resources", "groupVersion", groupVersion)
						continue
					}

					for _, r := range resources.APIResources {
						// Only sync namespaced resources that are not built-in types
						if r.Namespaced && !isBuiltInResource(r.Name) {
							if err := syncNamespaceScopedResource(ctx, sourceClient, destClient, srcNamespace, dstNamespace, r.Name, group.Name); err != nil {
								log.Error(err, "Failed to sync namespace scoped resource", "resource", r.Name, "group", group.Name)
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
				log.Error(fmt.Errorf("invalid resource reference format"), "Resource reference must be in format 'resource.group'", "reference", resourceRef)
				continue
			}

			resource := parts[0]
			group := strings.Join(parts[1:], ".")

			if err := syncNamespaceScopedResource(ctx, sourceClient, destClient, srcNamespace, dstNamespace, resource, group); err != nil {
				log.Error(err, "Failed to sync namespace scoped resource", "resource", resource, "group", group)
			}
		}
	}

	return deploymentScales, nil
}

// isBuiltInResource checks if a resource is a built-in Kubernetes resource
func isBuiltInResource(name string) bool {
	builtInResources := map[string]bool{
		"configmaps":             true,
		"secrets":               true,
		"deployments":           true,
		"services":              true,
		"ingresses":             true,
		"pods":                  true,
		"events":                true,
		"endpoints":             true,
		"persistentvolumeclaims": true,
		"persistentvolumes":      true,
	}
	return builtInResources[name]
}


// syncNamespaceScopedResource synchronizes a specific namespace scoped resource
func syncNamespaceScopedResource(ctx context.Context, sourceClient, destClient kubernetes.Interface, srcNamespace, dstNamespace, resource, group string) error {
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

	// Get dynamic client for the source cluster
	cfg, err := config.GetConfig()
	if err != nil {
		return fmt.Errorf("failed to get config: %v", err)
	}
	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %v", err)
	}

	// Get dynamic client for destination cluster
	destDynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create destination dynamic client: %v", err)
	}
	gvr := schema.GroupVersionResource{
		Group:    group,
		Version:  "v1",
		Resource: resource,
	}

	// List resources in source namespace
	sourceList, err := dynamicClient.Resource(gvr).Namespace(srcNamespace).List(ctx, metav1.ListOptions{})
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
		existing, err := destDynamicClient.Resource(gvr).Namespace(dstNamespace).Get(ctx, item.GetName(), metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				// Create resource
				_, err = destDynamicClient.Resource(gvr).Namespace(dstNamespace).Create(ctx, &item, metav1.CreateOptions{})
				if err != nil {
					log.Error(err, "Failed to create resource", "resource", resource, "name", item.GetName())
					continue
				}
				log.Info("Created resource", "resource", resource, "name", item.GetName())
			} else {
				log.Error(err, "Failed to get resource", "resource", resource, "name", item.GetName())
				continue
			}
		} else {
			// Update resource if needed
			if !reflect.DeepEqual(item.Object, existing.Object) {
				item.SetResourceVersion(existing.GetResourceVersion())
				_, err = destDynamicClient.Resource(gvr).Namespace(dstNamespace).Update(ctx, &item, metav1.UpdateOptions{})
				if err != nil {
					log.Error(err, "Failed to update resource", "resource", resource, "name", item.GetName())
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
