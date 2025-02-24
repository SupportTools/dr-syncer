package sync

import (
	"context"
	"fmt"
	"time"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
	"github.com/supporttools/dr-syncer/pkg/controllers/sync/internal/logging"
	corev1 "k8s.io/api/core/v1"
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

	logging.Logger.Info(fmt.Sprintf("skipping update of immutable resource %s/%s of type %s",
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

	// Get current resource
	current := obj.DeepCopyObject().(client.Object)
	key := types.NamespacedName{
		Name:      clientObj.GetName(),
		Namespace: clientObj.GetNamespace(),
	}
	if err := h.ctrlClient.Get(ctx, key, current); err != nil {
		return fmt.Errorf("failed to get current resource: %w", err)
	}

	// TODO: Implement field-specific update logic based on resource type
	// This requires knowledge of which fields are mutable for each resource type

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
