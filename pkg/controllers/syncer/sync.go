package syncer

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	drv1alpha1 "github.com/supporttools/dr-syncer/pkg/api/v1alpha1"
	"github.com/supporttools/dr-syncer/pkg/controllers/syncer/internal/logging"
	"github.com/supporttools/dr-syncer/pkg/controllers/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DeploymentScale represents a deployment's scale information
type DeploymentScale struct {
	Name     string
	Replicas int32
	SyncTime metav1.Time
}

// ResourceSyncer handles syncing resources between clusters
type ResourceSyncer struct {
	ctrlClient    client.Client
	sourceDynamic dynamic.Interface
	destDynamic   dynamic.Interface
	sourceClient  kubernetes.Interface
	destClient    kubernetes.Interface
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
	logging.Logger.Info(fmt.Sprintf("ensuring namespace %s exists", dstNamespace))

	maxRetries := 3
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		// Try to get the namespace
		_, err := client.CoreV1().Namespaces().Get(ctx, dstNamespace, metav1.GetOptions{})
		if err == nil {
			logging.Logger.Info(fmt.Sprintf("namespace %s already exists", dstNamespace))
			return nil
		}

		if !apierrors.IsNotFound(err) {
			lastErr = fmt.Errorf("failed to get namespace: %w", err)
			continue
		}

		// Create namespace if it doesn't exist
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: dstNamespace,
				Labels: map[string]string{
					"dr-syncer.io/source-namespace": srcNamespace,
					"dr-syncer.io/managed-by":       "dr-syncer",
				},
			},
		}

		_, err = client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		if err == nil {
			logging.Logger.Info(fmt.Sprintf("created namespace %s", dstNamespace))
			return nil
		}

		if !apierrors.IsAlreadyExists(err) {
			lastErr = fmt.Errorf("failed to create namespace %s: %w", dstNamespace, err)
			continue
		}

		// If we get here, another process created the namespace between our Get and Create
		logging.Logger.Info(fmt.Sprintf("namespace %s was created concurrently", dstNamespace))
		return nil
	}

	return lastErr
}

// verifyClusterAccess checks if the cluster has access to required resources
func verifyClusterAccess(ctx context.Context, client kubernetes.Interface, resourceTypes []string) error {
	logging.Logger.Info("verifying cluster resource permissions")

	// First verify API groups exist
	groups, err := client.Discovery().ServerGroups()
	if err != nil {
		return fmt.Errorf("failed to get API groups: %w", err)
	}

	// Build map of available API groups
	availableGroups := make(map[string]bool)
	for _, group := range groups.Groups {
		availableGroups[group.Name] = true
	}

	// Check if networking.k8s.io API group exists (needed for Ingress)
	if !availableGroups["networking.k8s.io"] {
		logging.Logger.Info("networking.k8s.io API group not found in cluster")
	}

	// Check if apps API group exists (needed for Deployments)
	if !availableGroups["apps"] {
		logging.Logger.Info("apps API group not found in cluster")
	}

	// Try to list each resource type to verify permissions
	for _, resourceType := range resourceTypes {
		logging.Logger.Info(fmt.Sprintf("checking access permissions for %s", resourceType))

		var err error
		switch strings.ToLower(resourceType) {
		case "configmaps", "configmap":
			_, err = client.CoreV1().ConfigMaps("").List(ctx, metav1.ListOptions{Limit: 1})
		case "secrets", "secret":
			_, err = client.CoreV1().Secrets("").List(ctx, metav1.ListOptions{Limit: 1})
		case "deployments", "deployment":
			if !availableGroups["apps"] {
				return fmt.Errorf("apps API group not available in cluster")
			}
			_, err = client.AppsV1().Deployments("").List(ctx, metav1.ListOptions{Limit: 1})
		case "services", "service":
			_, err = client.CoreV1().Services("").List(ctx, metav1.ListOptions{Limit: 1})
		case "ingresses", "ingress":
			if !availableGroups["networking.k8s.io"] {
				return fmt.Errorf("networking.k8s.io API group not available in cluster")
			}
			_, err = client.NetworkingV1().Ingresses("").List(ctx, metav1.ListOptions{Limit: 1})
		case "persistentvolumeclaims", "persistentvolumeclaim", "pvc":
			_, err = client.CoreV1().PersistentVolumeClaims("").List(ctx, metav1.ListOptions{Limit: 1})
		}

		if err != nil {
			if apierrors.IsNotFound(err) {
				return fmt.Errorf("resource type %s not found in cluster", resourceType)
			}
			return fmt.Errorf("failed to verify %s access: %w", resourceType, err)
		}
	}
	return nil
}

// SyncNamespaceResources synchronizes resources between source and destination namespaces
func SyncNamespaceResources(ctx context.Context, sourceClient, destClient kubernetes.Interface, sourceDynamic, destDynamic dynamic.Interface, ctrlClient client.Client, srcNamespace, dstNamespace string, resourceTypes []string, scaleToZero bool, namespaceScopedResources []string, pvcConfig *drv1alpha1.PVCConfig, immutableConfig *drv1alpha1.ImmutableResourceConfig) ([]DeploymentScale, error) {
	var deploymentScales []DeploymentScale

	// If no resource types specified, use defaults
	if len(resourceTypes) == 0 {
		resourceTypes = []string{"configmaps", "secrets", "deployments", "services", "ingresses", "persistentvolumeclaims"}
	}

	// Verify cluster access and permissions first
	logging.Logger.Info("verifying source cluster access")
	if err := verifyClusterAccess(ctx, sourceClient, resourceTypes); err != nil {
		return nil, fmt.Errorf("source cluster verification failed: %w", err)
	}

	logging.Logger.Info("verifying destination cluster access")
	if err := verifyClusterAccess(ctx, destClient, resourceTypes); err != nil {
		return nil, fmt.Errorf("destination cluster verification failed: %w", err)
	}

	logging.Logger.Info(fmt.Sprintf("initializing resource syncer for %s to %s", srcNamespace, dstNamespace))

	// Create resource syncer using the passed-in clients
	syncer := NewResourceSyncer(ctrlClient, sourceDynamic, destDynamic, sourceClient, destClient)

	// Ensure destination namespace exists first
	if err := EnsureNamespaceExists(ctx, destClient, dstNamespace, srcNamespace); err != nil {
		return nil, fmt.Errorf("failed to ensure destination namespace exists: %w", err)
	}

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
			logging.Logger.Info(fmt.Sprintf("created source namespace %s", srcNamespace))
		} else {
			return nil, fmt.Errorf("failed to get source namespace: %w", err)
		}
	}

	// Create namespace in destination cluster if it doesn't exist
	maxRetries := 3
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		_, err = destClient.CoreV1().Namespaces().Get(ctx, dstNamespace, metav1.GetOptions{})
		if err == nil {
			// Namespace exists, proceed with sync
			break
		}

		if !apierrors.IsNotFound(err) {
			lastErr = fmt.Errorf("failed to get destination namespace: %w", err)
			continue
		}

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
		newNS.Labels["dr-syncer.io/managed-by"] = "dr-syncer"

		_, err = destClient.CoreV1().Namespaces().Create(ctx, newNS, metav1.CreateOptions{})
		if err == nil {
			logging.Logger.Info(fmt.Sprintf("created destination namespace %s", dstNamespace))
			break
		}

		if !apierrors.IsAlreadyExists(err) {
			lastErr = fmt.Errorf("failed to create destination namespace: %w", err)
			continue
		}

		// If we get here, another process created the namespace between our Get and Create
		logging.Logger.Info(fmt.Sprintf("namespace %s was created concurrently", dstNamespace))
		break
	}

	if lastErr != nil {
		return nil, lastErr
	}

	logging.Logger.Info(fmt.Sprintf("starting resource synchronization from %s to %s", srcNamespace, dstNamespace))

	// Sync standard resource types
	for _, resourceType := range resourceTypes {
		// Normalize resource type to lowercase
		rtLower := strings.ToLower(resourceType)
		logging.Logger.Info(fmt.Sprintf("processing resource type: %s", resourceType))

		switch rtLower {
		case "configmaps", "configmap":
			if err := syncConfigMaps(ctx, syncer, sourceClient, srcNamespace, dstNamespace, immutableConfig); err != nil {
				return nil, fmt.Errorf("failed to sync ConfigMaps: %w", err)
			}
		case "secrets", "secret":
			if err := syncSecrets(ctx, syncer, sourceClient, srcNamespace, dstNamespace, immutableConfig); err != nil {
				return nil, fmt.Errorf("failed to sync Secrets: %w", err)
			}
		case "deployments", "deployment":
			scales, err := syncDeployments(ctx, syncer, sourceClient, srcNamespace, dstNamespace, scaleToZero, immutableConfig)
			if err != nil {
				return nil, fmt.Errorf("failed to sync Deployments: %w", err)
			}
			deploymentScales = append(deploymentScales, scales...)
		case "services", "service":
			if err := syncServices(ctx, syncer, sourceClient, srcNamespace, dstNamespace, immutableConfig); err != nil {
				return nil, fmt.Errorf("failed to sync Services: %w", err)
			}
		case "ingresses", "ingress":
			if err := syncIngresses(ctx, syncer, sourceClient, srcNamespace, dstNamespace, immutableConfig); err != nil {
				return nil, fmt.Errorf("failed to sync Ingresses: %w", err)
			}
		case "persistentvolumeclaims", "persistentvolumeclaim", "pvc":
			if err := syncPersistentVolumeClaims(ctx, syncer, sourceClient, srcNamespace, dstNamespace, pvcConfig, immutableConfig); err != nil {
				return nil, fmt.Errorf("failed to sync PVCs: %w", err)
			}
		}
	}

	// Sync namespace scoped resources
	if len(namespaceScopedResources) == 1 && namespaceScopedResources[0] == "*" {
		// Get all API resources from the source cluster
		groups, err := sourceClient.Discovery().ServerGroups()
		if err != nil {
			logging.Logger.WithError(err).Error("failed to get API groups")
		} else {
			for _, group := range groups.Groups {
				for _, version := range group.Versions {
					groupVersion := version.GroupVersion
					resources, err := sourceClient.Discovery().ServerResourcesForGroupVersion(groupVersion)
					if err != nil {
						logging.Logger.WithError(err).Error(fmt.Sprintf("failed to get resources for group version %s", groupVersion))
						continue
					}

					for _, r := range resources.APIResources {
						// Only sync namespaced resources that are not built-in types
						if r.Namespaced && !isBuiltInResource(r.Name) {
							if err := syncer.syncNamespaceScopedResource(ctx, sourceClient, destClient, srcNamespace, dstNamespace, r.Name, group.Name); err != nil {
								logging.Logger.WithError(err).Error(fmt.Sprintf("failed to sync resource %s in group %s", r.Name, group.Name))
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
				logging.Logger.Error(fmt.Sprintf("invalid resource reference format: %s", resourceRef))
				continue
			}

			resource := parts[0]
			group := strings.Join(parts[1:], ".")

			if err := syncer.syncNamespaceScopedResource(ctx, sourceClient, destClient, srcNamespace, dstNamespace, resource, group); err != nil {
				logging.Logger.WithError(err).Error(fmt.Sprintf("failed to sync resource %s in group %s", resource, group))
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
		"configmap":              true,
		"secrets":                true,
		"secret":                 true,
		"deployments":            true,
		"deployment":             true,
		"services":               true,
		"service":                true,
		"ingresses":              true,
		"ingress":                true,
		"pods":                   true,
		"pod":                    true,
		"events":                 true,
		"event":                  true,
		"endpoints":              true,
		"endpoint":               true,
		"persistentvolumeclaims": true,
		"persistentvolumeclaim":  true,
		"pvc":                    true,
		"persistentvolumes":      true,
		"persistentvolume":       true,
		"pv":                     true,
	}
	return builtInResources[nameLower]
}

// syncNamespaceScopedResource synchronizes a specific namespace scoped resource
func (r *ResourceSyncer) syncNamespaceScopedResource(ctx context.Context, sourceClient, destClient kubernetes.Interface, srcNamespace, dstNamespace, resource, group string) error {
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
					logging.Logger.WithError(err).Error(fmt.Sprintf("failed to create resource %s/%s", resource, item.GetName()))
					continue
				}
				logging.Logger.Info(fmt.Sprintf("created resource %s/%s", resource, item.GetName()))
			} else {
				logging.Logger.WithError(err).Error(fmt.Sprintf("failed to get resource %s/%s", resource, item.GetName()))
				continue
			}
		} else {
			// Update resource if needed
			if !reflect.DeepEqual(item.Object, existing.Object) {
				// Preserve UID and ResourceVersion
				item.SetUID(existing.GetUID())
				item.SetResourceVersion(existing.GetResourceVersion())
				_, err = r.destDynamic.Resource(gvr).Namespace(dstNamespace).Update(ctx, &item, metav1.UpdateOptions{})
				if err != nil {
					logging.Logger.WithError(err).Error(fmt.Sprintf("failed to update resource %s/%s", resource, item.GetName()))
					continue
				}
				logging.Logger.Info(fmt.Sprintf("updated resource %s/%s", resource, item.GetName()))
			}
		}
	}

	return nil
}

// SyncResource syncs a single resource between clusters
func (r *ResourceSyncer) SyncResource(ctx context.Context, obj runtime.Object, config *drv1alpha1.ImmutableResourceConfig) error {
	// Get GVK from the object
	gvk := obj.GetObjectKind().GroupVersionKind()
	if gvk.Empty() {
		// Try to determine GVK from the object type
		switch obj.(type) {
		case *corev1.ConfigMap:
			gvk = schema.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "ConfigMap",
			}
		case *corev1.Secret:
			gvk = schema.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Secret",
			}
		case *corev1.Service:
			gvk = schema.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Service",
			}
		case *appsv1.Deployment:
			gvk = schema.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			}
		case *networkingv1.Ingress:
			gvk = schema.GroupVersionKind{
				Group:   "networking.k8s.io",
				Version: "v1",
				Kind:    "Ingress",
			}
		default:
			return fmt.Errorf("unknown object type: %T", obj)
		}
	}

	// Convert to unstructured
	unstructuredObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return fmt.Errorf("failed to convert object to unstructured: %w", err)
	}

	u := &unstructured.Unstructured{Object: unstructuredObj}
	u.SetGroupVersionKind(gvk)

	// Ensure GVK is set for Deployments
	if _, ok := obj.(*appsv1.Deployment); ok && u.GroupVersionKind().Group != "apps" {
		u.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "apps",
			Version: "v1",
			Kind:    "Deployment",
		})
	}

	// Create GroupVersionResource from GroupVersionKind
	var gvr schema.GroupVersionResource
	switch gvk.Kind {
	case "ConfigMap":
		gvr = schema.GroupVersionResource{
			Group:    "",
			Version:  "v1",
			Resource: "configmaps",
		}
	case "Secret":
		gvr = schema.GroupVersionResource{
			Group:    "",
			Version:  "v1",
			Resource: "secrets",
		}
	case "Deployment":
		gvr = schema.GroupVersionResource{
			Group:    "apps",
			Version:  "v1",
			Resource: "deployments",
		}
	case "Service":
		gvr = schema.GroupVersionResource{
			Group:    "",
			Version:  "v1",
			Resource: "services",
		}
	case "Ingress":
		gvr = schema.GroupVersionResource{
			Group:    "networking.k8s.io",
			Version:  "v1",
			Resource: "ingresses",
		}
	default:
		// For other types, use the standard conversion
		gvr = schema.GroupVersionResource{
			Group:    gvk.Group,
			Version:  gvk.Version,
			Resource: strings.ToLower(gvk.Kind) + "s", // Pluralize the kind
		}
	}

	logging.Logger.Info(fmt.Sprintf("syncing %s %s/%s", gvk.Kind, u.GetNamespace(), u.GetName()))

	// Get current resource in destination cluster
	existing, err := r.destDynamic.Resource(gvr).Namespace(u.GetNamespace()).Get(ctx, u.GetName(), metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to get current resource: %w", err)
		}
		// Resource doesn't exist, create it
		logging.Logger.Info(fmt.Sprintf("creating %s %s/%s", gvk.Kind, u.GetNamespace(), u.GetName()))

		// Sanitize metadata before creation
		utils.SanitizeMetadata(u)
		_, err = r.destDynamic.Resource(gvr).Namespace(u.GetNamespace()).Create(ctx, u, metav1.CreateOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return fmt.Errorf("resource type %s not found in destination cluster", gvk.Kind)
			}
			return fmt.Errorf("failed to create resource: %w", err)
		}
		return nil
	}

	// Create copies for comparison
	existingCopy := existing.DeepCopy()
	sourceCopy := u.DeepCopy()

	// Store UID for update
	existingUID := existingCopy.GetUID()

	// Sanitize both copies
	utils.SanitizeMetadata(existingCopy)
	utils.SanitizeMetadata(sourceCopy)

	// Compare sanitized versions
	if !reflect.DeepEqual(existingCopy.Object, sourceCopy.Object) {
		// Real change detected - update with proper resourceVersion and UID
		logging.Logger.Info(fmt.Sprintf("updating %s %s/%s", gvk.Kind, u.GetNamespace(), u.GetName()))

		u.SetUID(existingUID)
		u.SetResourceVersion(existing.GetResourceVersion())
		_, err = r.destDynamic.Resource(gvr).Namespace(u.GetNamespace()).Update(ctx, u, metav1.UpdateOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return fmt.Errorf("resource type %s not found in destination cluster", gvk.Kind)
			}
			return fmt.Errorf("failed to update resource: %w", err)
		}
	} else {
		logging.Logger.Info(fmt.Sprintf("no changes needed for %s %s/%s", gvk.Kind, u.GetNamespace(), u.GetName()))
	}
	return nil
}
