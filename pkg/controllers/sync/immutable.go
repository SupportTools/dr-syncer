package sync

import (
	"context"
	"fmt"
	"time"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// Label to override immutable resource handling at resource level
	ImmutableHandlingLabel = "dr-syncer.io/immutable-handling"
)

// ImmutableResourceHandler handles operations on immutable resources
type ImmutableResourceHandler struct {
	sourceClient kubernetes.Interface
	destClient   kubernetes.Interface
	ctrlClient   client.Client
}

// NewImmutableResourceHandler creates a new handler
func NewImmutableResourceHandler(sourceClient kubernetes.Interface, destClient kubernetes.Interface, ctrlClient client.Client) *ImmutableResourceHandler {
	return &ImmutableResourceHandler{
		sourceClient: sourceClient,
		destClient:   destClient,
		ctrlClient:   ctrlClient,
	}
}

// DetermineHandling determines how to handle an immutable resource
func (h *ImmutableResourceHandler) DetermineHandling(obj metav1.Object, config *drv1alpha1.ImmutableResourceConfig) drv1alpha1.ImmutableResourceHandling {
	// Check resource label override
	if handling, ok := obj.GetLabels()[ImmutableHandlingLabel]; ok {
		switch handling {
		case "no-change":
			return drv1alpha1.NoChange
		case "recreate":
			return drv1alpha1.Recreate
		case "recreate-with-drain":
			return drv1alpha1.RecreateWithPodDrain
		case "partial-update":
			return drv1alpha1.PartialUpdate
		case "force-update":
			return drv1alpha1.ForceUpdate
		}
	}

	// Check resource type override if config exists
	if config != nil && config.ResourceOverrides != nil {
		gvk := obj.(runtime.Object).GetObjectKind().GroupVersionKind()
		resourceKey := fmt.Sprintf("%s.%s", gvk.Kind, gvk.Group)
		if handling, ok := config.ResourceOverrides[resourceKey]; ok {
			return handling
		}
	}

	// Use default handling if config exists
	if config != nil {
		return config.DefaultHandling
	}

	// System default
	return drv1alpha1.NoChange
}

// HandleImmutableResource handles an immutable resource based on the specified handling strategy
func (h *ImmutableResourceHandler) HandleImmutableResource(ctx context.Context, obj runtime.Object, handling drv1alpha1.ImmutableResourceHandling, config *drv1alpha1.ImmutableResourceConfig) error {
	switch handling {
	case drv1alpha1.NoChange:
		return h.handleNoChange(ctx, obj)
	case drv1alpha1.Recreate:
		return h.handleRecreate(ctx, obj)
	case drv1alpha1.RecreateWithPodDrain:
		timeout := 5 * time.Minute
		if config != nil && config.DrainTimeout != nil {
			timeout = config.DrainTimeout.Duration
		}
		return h.handleRecreateWithDrain(ctx, obj, timeout)
	case drv1alpha1.PartialUpdate:
		return h.handlePartialUpdate(ctx, obj)
	case drv1alpha1.ForceUpdate:
		timeout := 2 * time.Minute
		if config != nil && config.ForceDeleteTimeout != nil {
			timeout = config.ForceDeleteTimeout.Duration
		}
		return h.handleForceUpdate(ctx, obj, timeout)
	default:
		return h.handleNoChange(ctx, obj)
	}
}

// handleNoChange skips updating the resource and logs a warning
func (h *ImmutableResourceHandler) handleNoChange(ctx context.Context, obj runtime.Object) error {
	clientObj, ok := obj.(client.Object)
	if !ok {
		return fmt.Errorf("object does not implement client.Object")
	}

	log.Info(fmt.Sprintf("skipping update of immutable resource %s/%s of type %s",
		clientObj.GetNamespace(),
		clientObj.GetName(),
		obj.GetObjectKind().GroupVersionKind().Kind))

	// TODO: Record warning event
	return nil
}

// handleRecreate deletes and recreates the resource
func (h *ImmutableResourceHandler) handleRecreate(ctx context.Context, obj runtime.Object) error {
	clientObj, ok := obj.(client.Object)
	if !ok {
		return fmt.Errorf("object does not implement client.Object")
	}

	// Delete resource
	if err := h.ctrlClient.Delete(ctx, clientObj); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete resource: %w", err)
	}

	// Wait for deletion
	key := types.NamespacedName{
		Name:      clientObj.GetName(),
		Namespace: clientObj.GetNamespace(),
	}
	if err := retry.OnError(retry.DefaultRetry, func(err error) bool {
		return !apierrors.IsNotFound(err)
	}, func() error {
		return h.ctrlClient.Get(ctx, key, clientObj)
	}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed waiting for resource deletion: %w", err)
	}

	// Create new resource
	if err := h.ctrlClient.Create(ctx, clientObj); err != nil {
		return fmt.Errorf("failed to create resource: %w", err)
	}

	return nil
}

// handleRecreateWithDrain safely drains pods before recreating the resource
func (h *ImmutableResourceHandler) handleRecreateWithDrain(ctx context.Context, obj runtime.Object, timeout time.Duration) error {
	// Get pods for the resource
	pods, err := h.getPodsForResource(ctx, obj)
	if err != nil {
		return fmt.Errorf("failed to get pods: %w", err)
	}

	// Create eviction for each pod
	for _, pod := range pods.Items {
		eviction := &policyv1beta1.Eviction{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pod.Name,
				Namespace: pod.Namespace,
			},
		}
		if err := h.destClient.CoreV1().Pods(pod.Namespace).Evict(ctx, eviction); err != nil {
			return fmt.Errorf("failed to evict pod %s: %w", pod.Name, err)
		}
	}

	// Wait for pods to be evicted
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pods, err := h.getPodsForResource(ctx, obj)
		if err != nil {
			return fmt.Errorf("failed to get pods: %w", err)
		}
		if len(pods.Items) == 0 {
			break
		}
		time.Sleep(time.Second)
	}

	// Proceed with recreation
	return h.handleRecreate(ctx, obj)
}

// handlePartialUpdate applies only mutable field changes
func (h *ImmutableResourceHandler) handlePartialUpdate(ctx context.Context, obj runtime.Object) error {
	// We need both metav1.Object and client.Object interfaces
	clientObj, ok := obj.(client.Object)
	if !ok {
		return fmt.Errorf("object does not implement client.Object")
	}

	// Get current resource from destination
	current := obj.DeepCopyObject().(client.Object)
	key := types.NamespacedName{
		Name:      clientObj.GetName(),
		Namespace: clientObj.GetNamespace(),
	}
	if err := h.ctrlClient.Get(ctx, key, current); err != nil {
		return fmt.Errorf("failed to get current resource: %w", err)
	}

	// Apply mutable field updates based on resource type
	updated, err := h.applyMutableFieldUpdates(current, obj)
	if err != nil {
		return fmt.Errorf("failed to apply mutable updates: %w", err)
	}

	// Update the resource
	if err := h.ctrlClient.Update(ctx, updated); err != nil {
		return fmt.Errorf("failed to update resource: %w", err)
	}

	log.Info(fmt.Sprintf("successfully applied partial update to %s/%s",
		clientObj.GetNamespace(), clientObj.GetName()))

	return nil
}

// handleForceUpdate force deletes and recreates the resource
func (h *ImmutableResourceHandler) handleForceUpdate(ctx context.Context, obj runtime.Object, timeout time.Duration) error {
	meta, ok := obj.(metav1.Object)
	if !ok {
		return fmt.Errorf("object does not implement metav1.Object")
	}

	clientObj, ok := obj.(client.Object)
	if !ok {
		return fmt.Errorf("object does not implement client.Object")
	}

	// Delete with foreground cascading
	foreground := metav1.DeletePropagationForeground
	if err := h.ctrlClient.Delete(ctx, clientObj, &client.DeleteOptions{
		PropagationPolicy: &foreground,
	}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete resource: %w", err)
	}

	// Wait for cascading deletion
	key := types.NamespacedName{
		Name:      meta.GetName(),
		Namespace: meta.GetNamespace(),
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := h.ctrlClient.Get(ctx, key, clientObj); err != nil {
			if apierrors.IsNotFound(err) {
				break
			}
			return fmt.Errorf("failed to check resource deletion: %w", err)
		}
		time.Sleep(time.Second)
	}

	// Create new resource
	if err := h.ctrlClient.Create(ctx, clientObj); err != nil {
		return fmt.Errorf("failed to create resource: %w", err)
	}

	return nil
}

// getPodsForResource returns pods controlled by the given resource
func (h *ImmutableResourceHandler) getPodsForResource(ctx context.Context, obj runtime.Object) (*corev1.PodList, error) {
	meta, ok := obj.(metav1.Object)
	if !ok {
		return nil, fmt.Errorf("object does not implement metav1.Object")
	}

	// List pods in the same namespace
	pods, err := h.destClient.CoreV1().Pods(meta.GetNamespace()).List(ctx, metav1.ListOptions{
		// TODO: Add label selectors based on resource type
		// This requires implementing resource-specific pod selection logic
	})
	if err != nil {
		return nil, err
	}

	return pods, nil
}

// applyMutableFieldUpdates copies mutable fields from source to destination based on resource type
func (h *ImmutableResourceHandler) applyMutableFieldUpdates(current, source runtime.Object) (client.Object, error) {
	switch src := source.(type) {
	case *corev1.ConfigMap:
		curr, ok := current.(*corev1.ConfigMap)
		if !ok {
			return nil, fmt.Errorf("current object is not a ConfigMap")
		}
		return h.updateConfigMapFields(curr, src)
	case *corev1.Secret:
		curr, ok := current.(*corev1.Secret)
		if !ok {
			return nil, fmt.Errorf("current object is not a Secret")
		}
		return h.updateSecretFields(curr, src)
	case *corev1.Service:
		curr, ok := current.(*corev1.Service)
		if !ok {
			return nil, fmt.Errorf("current object is not a Service")
		}
		return h.updateServiceFields(curr, src)
	case *corev1.PersistentVolumeClaim:
		curr, ok := current.(*corev1.PersistentVolumeClaim)
		if !ok {
			return nil, fmt.Errorf("current object is not a PersistentVolumeClaim")
		}
		return h.updatePVCFields(curr, src)
	case *appsv1.Deployment:
		curr, ok := current.(*appsv1.Deployment)
		if !ok {
			return nil, fmt.Errorf("current object is not a Deployment")
		}
		return h.updateDeploymentFields(curr, src)
	case *networkingv1.Ingress:
		curr, ok := current.(*networkingv1.Ingress)
		if !ok {
			return nil, fmt.Errorf("current object is not an Ingress")
		}
		return h.updateIngressFields(curr, src)
	default:
		// For unknown types, update labels and annotations only
		currClient, ok := current.(client.Object)
		if !ok {
			return nil, fmt.Errorf("current object does not implement client.Object")
		}
		srcClient, ok := source.(client.Object)
		if !ok {
			return nil, fmt.Errorf("source object does not implement client.Object")
		}
		return h.updateMetadataOnly(currClient, srcClient)
	}
}

// updateConfigMapFields updates mutable fields for ConfigMap
func (h *ImmutableResourceHandler) updateConfigMapFields(current, source *corev1.ConfigMap) (client.Object, error) {
	updated := current.DeepCopy()

	// Update mutable fields
	updated.Data = source.Data
	updated.BinaryData = source.BinaryData

	// Update metadata (labels and annotations)
	updated.Labels = source.Labels
	updated.Annotations = source.Annotations

	return updated, nil
}

// updateSecretFields updates mutable fields for Secret
// Note: Secret.Type is immutable, so we only update data fields
func (h *ImmutableResourceHandler) updateSecretFields(current, source *corev1.Secret) (client.Object, error) {
	updated := current.DeepCopy()

	// Update mutable fields (data only, type is immutable)
	updated.Data = source.Data
	updated.StringData = source.StringData

	// Update metadata (labels and annotations)
	updated.Labels = source.Labels
	updated.Annotations = source.Annotations

	return updated, nil
}

// updateServiceFields updates mutable fields for Service
// Note: clusterIP and clusterIPs are immutable and must be preserved
func (h *ImmutableResourceHandler) updateServiceFields(current, source *corev1.Service) (client.Object, error) {
	updated := current.DeepCopy()

	// Preserve immutable fields from current
	preservedClusterIP := current.Spec.ClusterIP
	preservedClusterIPs := current.Spec.ClusterIPs

	// Update mutable spec fields
	updated.Spec.Ports = source.Spec.Ports
	updated.Spec.Selector = source.Spec.Selector
	updated.Spec.ExternalIPs = source.Spec.ExternalIPs
	updated.Spec.LoadBalancerIP = source.Spec.LoadBalancerIP
	updated.Spec.LoadBalancerSourceRanges = source.Spec.LoadBalancerSourceRanges
	updated.Spec.ExternalName = source.Spec.ExternalName
	updated.Spec.ExternalTrafficPolicy = source.Spec.ExternalTrafficPolicy
	updated.Spec.SessionAffinity = source.Spec.SessionAffinity
	updated.Spec.SessionAffinityConfig = source.Spec.SessionAffinityConfig

	// Restore immutable fields
	updated.Spec.ClusterIP = preservedClusterIP
	updated.Spec.ClusterIPs = preservedClusterIPs

	// Update metadata (labels and annotations)
	updated.Labels = source.Labels
	updated.Annotations = source.Annotations

	return updated, nil
}

// updatePVCFields updates mutable fields for PersistentVolumeClaim
// Note: storageClassName, volumeName, and accessModes are immutable
// Only resources.requests.storage can be expanded (not contracted)
func (h *ImmutableResourceHandler) updatePVCFields(current, source *corev1.PersistentVolumeClaim) (client.Object, error) {
	updated := current.DeepCopy()

	// Only update resources.requests if source requests more storage
	// PVC can only be expanded, not contracted
	if source.Spec.Resources.Requests != nil {
		if sourceStorage, ok := source.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
			if currentStorage, ok := current.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
				// Only update if source requests more storage
				if sourceStorage.Cmp(currentStorage) > 0 {
					if updated.Spec.Resources.Requests == nil {
						updated.Spec.Resources.Requests = make(corev1.ResourceList)
					}
					updated.Spec.Resources.Requests[corev1.ResourceStorage] = sourceStorage
				}
			}
		}
	}

	// Update metadata (labels and annotations)
	updated.Labels = source.Labels
	updated.Annotations = source.Annotations

	return updated, nil
}

// updateDeploymentFields updates mutable fields for Deployment
// Note: spec.selector is immutable and must be preserved
func (h *ImmutableResourceHandler) updateDeploymentFields(current, source *appsv1.Deployment) (client.Object, error) {
	updated := current.DeepCopy()

	// Preserve immutable selector from current
	preservedSelector := current.Spec.Selector

	// Update mutable spec fields
	updated.Spec.Replicas = source.Spec.Replicas
	updated.Spec.Template = source.Spec.Template
	updated.Spec.Strategy = source.Spec.Strategy
	updated.Spec.MinReadySeconds = source.Spec.MinReadySeconds
	updated.Spec.RevisionHistoryLimit = source.Spec.RevisionHistoryLimit
	updated.Spec.Paused = source.Spec.Paused
	updated.Spec.ProgressDeadlineSeconds = source.Spec.ProgressDeadlineSeconds

	// Restore immutable selector
	updated.Spec.Selector = preservedSelector

	// Update metadata (labels and annotations)
	updated.Labels = source.Labels
	updated.Annotations = source.Annotations

	return updated, nil
}

// updateIngressFields updates mutable fields for Ingress
func (h *ImmutableResourceHandler) updateIngressFields(current, source *networkingv1.Ingress) (client.Object, error) {
	updated := current.DeepCopy()

	// Update mutable spec fields
	updated.Spec.IngressClassName = source.Spec.IngressClassName
	updated.Spec.DefaultBackend = source.Spec.DefaultBackend
	updated.Spec.TLS = source.Spec.TLS
	updated.Spec.Rules = source.Spec.Rules

	// Update metadata (labels and annotations)
	updated.Labels = source.Labels
	updated.Annotations = source.Annotations

	return updated, nil
}

// updateMetadataOnly updates only labels and annotations for unknown resource types
func (h *ImmutableResourceHandler) updateMetadataOnly(current, source client.Object) (client.Object, error) {
	// Get a deep copy to avoid modifying the original
	updated := current.DeepCopyObject().(client.Object)

	// Update metadata only
	updated.SetLabels(source.GetLabels())
	updated.SetAnnotations(source.GetAnnotations())

	log.Info(fmt.Sprintf("updating metadata only for unknown resource type %s/%s",
		current.GetNamespace(), current.GetName()))

	return updated, nil
}
