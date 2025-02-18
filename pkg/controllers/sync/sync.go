package sync

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

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
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/log"

	drv1alpha1 "github.com/supporttools/dr-syncer/pkg/api/v1alpha1"
)

const (
	// ScaleOverrideLabel is used to override the scale of a deployment in the destination cluster
	// Format: "dr-syncer.io/scale-override: <number>"
	ScaleOverrideLabel = "dr-syncer.io/scale-override"

	// IgnoreLabel is used to mark resources that should be ignored during replication
	// Format: "dr-syncer.io/ignore: true"
	IgnoreLabel = "dr-syncer.io/ignore"
)

// parseInt32 converts a string to int32
func parseInt32(s string) (int32, error) {
	i, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return 0, err
	}
	return int32(i), nil
}

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
	Name string
	Replicas int32
	SyncTime time.Time
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
	if err != nil {
		return fmt.Errorf("failed to list %s in source namespace: %v", resource, err)
	}

	// Process each resource
	for _, item := range items {
		if shouldIgnoreResource(&item) {
			continue
		}

		// Prepare resource for destination
		item.SetNamespace(dstNamespace)
		sanitizeMetadata(&item)

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

// SyncNamespaceResources synchronizes resources between source and destination namespaces
func SyncNamespaceResources(ctx context.Context, sourceClient, destClient kubernetes.Interface, srcNamespace, dstNamespace string, resourceTypes []string, scaleToZero bool, namespaceScopedResources []string, pvcConfig *drv1alpha1.PVCConfig) ([]DeploymentScale, error) {
	var deploymentScales []DeploymentScale
	log := log.FromContext(ctx)

	// If no resource types specified, use defaults
	if len(resourceTypes) == 0 {
		resourceTypes = []string{"configmaps", "secrets", "deployments", "services", "ingresses", "persistentvolumeclaims"}
	}

	// Sync standard resource types
	for _, resourceType := range resourceTypes {
		switch resourceType {
		case "configmaps":
			if err := syncConfigMaps(ctx, sourceClient, destClient, srcNamespace, dstNamespace); err != nil {
				log.Error(err, "Failed to sync ConfigMaps")
			}
		case "secrets":
			if err := syncSecrets(ctx, sourceClient, destClient, srcNamespace, dstNamespace); err != nil {
				log.Error(err, "Failed to sync Secrets")
			}
		case "deployments":
			scales, err := syncDeployments(ctx, sourceClient, destClient, srcNamespace, dstNamespace, scaleToZero)
			if err != nil {
				log.Error(err, "Failed to sync Deployments")
			} else {
				deploymentScales = scales
			}
		case "services":
			if err := syncServices(ctx, sourceClient, destClient, srcNamespace, dstNamespace); err != nil {
				log.Error(err, "Failed to sync Services")
			}
		case "ingresses":
			if err := syncIngresses(ctx, sourceClient, destClient, srcNamespace, dstNamespace); err != nil {
				log.Error(err, "Failed to sync Ingresses")
			}
		case "persistentvolumeclaims":
			if err := syncPersistentVolumeClaims(ctx, sourceClient, destClient, srcNamespace, dstNamespace, pvcConfig); err != nil {
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

// Helper functions from the original controller
// shouldIgnoreResource checks if a resource should be ignored based on labels
func shouldIgnoreResource(obj metav1.Object) bool {
	if val, exists := obj.GetLabels()[IgnoreLabel]; exists {
		return val == "true"
	}
	return false
}

func syncConfigMaps(ctx context.Context, sourceClient, destClient kubernetes.Interface, srcNamespace, dstNamespace string) error {
	log := log.FromContext(ctx)
	configMaps, err := sourceClient.CoreV1().ConfigMaps(srcNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list ConfigMaps: %v", err)
	}

	for _, cm := range configMaps.Items {
		if cm.Name == "kube-root-ca.crt" || shouldIgnoreResource(&cm) {
			continue
		}

		cm.Namespace = dstNamespace
		sanitizeMetadata(&cm)

		existing, err := destClient.CoreV1().ConfigMaps(dstNamespace).Get(ctx, cm.Name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				_, err = destClient.CoreV1().ConfigMaps(dstNamespace).Create(ctx, &cm, metav1.CreateOptions{})
				if err != nil {
					log.Error(err, "Failed to create ConfigMap", "name", cm.Name)
					continue
				}
				log.Info("Created ConfigMap", "name", cm.Name)
			} else {
				log.Error(err, "Failed to get ConfigMap", "name", cm.Name)
				continue
			}
		} else {
			if !reflect.DeepEqual(cm.Data, existing.Data) || !reflect.DeepEqual(cm.BinaryData, existing.BinaryData) {
				cm.ResourceVersion = existing.ResourceVersion
				_, err = destClient.CoreV1().ConfigMaps(dstNamespace).Update(ctx, &cm, metav1.UpdateOptions{})
				if err != nil {
					log.Error(err, "Failed to update ConfigMap", "name", cm.Name)
					continue
				}
				log.Info("Updated ConfigMap", "name", cm.Name)
			}
		}
	}
	return nil
}

func syncSecrets(ctx context.Context, sourceClient, destClient kubernetes.Interface, srcNamespace, dstNamespace string) error {
	log := log.FromContext(ctx)
	secrets, err := sourceClient.CoreV1().Secrets(srcNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list Secrets: %v", err)
	}

	for _, secret := range secrets.Items {
		if shouldIgnoreResource(&secret) {
			continue
		}
		secret.Namespace = dstNamespace
		sanitizeMetadata(&secret)

		existing, err := destClient.CoreV1().Secrets(dstNamespace).Get(ctx, secret.Name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				_, err = destClient.CoreV1().Secrets(dstNamespace).Create(ctx, &secret, metav1.CreateOptions{})
				if err != nil {
					log.Error(err, "Failed to create Secret", "name", secret.Name)
					continue
				}
				log.Info("Created Secret", "name", secret.Name)
			} else {
				log.Error(err, "Failed to get Secret", "name", secret.Name)
				continue
			}
		} else {
			if !reflect.DeepEqual(secret.Data, existing.Data) || !reflect.DeepEqual(secret.StringData, existing.StringData) {
				secret.ResourceVersion = existing.ResourceVersion
				_, err = destClient.CoreV1().Secrets(dstNamespace).Update(ctx, &secret, metav1.UpdateOptions{})
				if err != nil {
					log.Error(err, "Failed to update Secret", "name", secret.Name)
					continue
				}
				log.Info("Updated Secret", "name", secret.Name)
			}
		}
	}
	return nil
}

func syncDeployments(ctx context.Context, sourceClient, destClient kubernetes.Interface, srcNamespace, dstNamespace string, scaleToZero bool) ([]DeploymentScale, error) {
	var scales []DeploymentScale
	log := log.FromContext(ctx)
	deployments, err := sourceClient.AppsV1().Deployments(srcNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list Deployments: %v", err)
	}

	for _, deployment := range deployments.Items {
		if shouldIgnoreResource(&deployment) {
			continue
		}
		deployment.Namespace = dstNamespace
		sanitizeMetadata(&deployment)

		// Get and store the original number of replicas
		originalReplicas := int32(0)
		if deployment.Spec.Replicas != nil {
			originalReplicas = *deployment.Spec.Replicas
		}

		// Add to scales list
		scales = append(scales, DeploymentScale{
			Name:     deployment.Name,
			Replicas: originalReplicas,
			SyncTime: time.Now(),
		})

		// Store information in annotations
		if deployment.Annotations == nil {
			deployment.Annotations = make(map[string]string)
		}
		deployment.Annotations["dr-syncer.io/original-replicas"] = fmt.Sprintf("%d", originalReplicas)
		deployment.Annotations["dr-syncer.io/source-namespace"] = srcNamespace
		deployment.Annotations["dr-syncer.io/synced-at"] = metav1.Now().Format(time.RFC3339)

		// Check for scale override label
		if scaleOverride, exists := deployment.Labels[ScaleOverrideLabel]; exists {
			if override, err := parseInt32(scaleOverride); err == nil {
				deployment.Spec.Replicas = &override
			} else {
				log.Error(err, "Invalid scale override value", "deployment", deployment.Name, "value", scaleOverride)
			}
		} else if scaleToZero {
			// Scale down the deployment to zero replicas
			zero := int32(0)
			deployment.Spec.Replicas = &zero
		} else {
			// Keep the original number of replicas
			deployment.Spec.Replicas = &originalReplicas
		}

		existing, err := destClient.AppsV1().Deployments(dstNamespace).Get(ctx, deployment.Name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				_, err = destClient.AppsV1().Deployments(dstNamespace).Create(ctx, &deployment, metav1.CreateOptions{})
				if err != nil {
					log.Error(err, "Failed to create Deployment", "name", deployment.Name)
					continue
				}
				log.Info("Created Deployment", "name", deployment.Name)
			} else {
				log.Error(err, "Failed to get Deployment", "name", deployment.Name)
				continue
			}
		} else {
			// Type assertion to use appsv1
			var _ appsv1.DeploymentSpec = deployment.Spec

			// Compare deployment specs
			specChanged := false
			
			// Compare relevant fields that should trigger an update
			if deployment.Spec.Replicas != nil && existing.Spec.Replicas != nil && *deployment.Spec.Replicas != *existing.Spec.Replicas {
				specChanged = true
			}
			if !reflect.DeepEqual(deployment.Spec.Template.Spec.Containers, existing.Spec.Template.Spec.Containers) {
				specChanged = true
			}
			if !reflect.DeepEqual(deployment.Spec.Template.Spec.Volumes, existing.Spec.Template.Spec.Volumes) {
				specChanged = true
			}
			
			if specChanged || !reflect.DeepEqual(deployment.Annotations, existing.Annotations) {
				deployment.ResourceVersion = existing.ResourceVersion
				_, err = destClient.AppsV1().Deployments(dstNamespace).Update(ctx, &deployment, metav1.UpdateOptions{})
				if err != nil {
					log.Error(err, "Failed to update Deployment", "name", deployment.Name)
					continue
				}
				log.Info("Updated Deployment", "name", deployment.Name)
			}
		}
	}
	return scales, nil
}

func syncServices(ctx context.Context, sourceClient, destClient kubernetes.Interface, srcNamespace, dstNamespace string) error {
	log := log.FromContext(ctx)
	services, err := sourceClient.CoreV1().Services(srcNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list Services: %v", err)
	}

	for _, service := range services.Items {
		if shouldIgnoreResource(&service) {
			continue
		}
		service.Namespace = dstNamespace
		sanitizeMetadata(&service)
		service.Spec.ClusterIP = ""
		service.Spec.ClusterIPs = nil
		service.Spec.HealthCheckNodePort = 0

		existing, err := destClient.CoreV1().Services(dstNamespace).Get(ctx, service.Name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				_, err = destClient.CoreV1().Services(dstNamespace).Create(ctx, &service, metav1.CreateOptions{})
				if err != nil {
					log.Error(err, "Failed to create Service", "name", service.Name)
					continue
				}
				log.Info("Created Service", "name", service.Name)
			} else {
				log.Error(err, "Failed to get Service", "name", service.Name)
				continue
			}
		} else {
			// Compare service specs
			specChanged := false
			
			// Compare relevant fields that should trigger an update
			if !reflect.DeepEqual(service.Spec.Ports, existing.Spec.Ports) {
				specChanged = true
			}
			if !reflect.DeepEqual(service.Spec.Selector, existing.Spec.Selector) {
				specChanged = true
			}
			if service.Spec.Type != existing.Spec.Type {
				specChanged = true
			}
			
			if specChanged {
				service.ResourceVersion = existing.ResourceVersion
				service.Spec.ClusterIP = existing.Spec.ClusterIP
				service.Spec.ClusterIPs = existing.Spec.ClusterIPs
				_, err = destClient.CoreV1().Services(dstNamespace).Update(ctx, &service, metav1.UpdateOptions{})
				if err != nil {
					log.Error(err, "Failed to update Service", "name", service.Name)
					continue
				}
				log.Info("Updated Service", "name", service.Name)
			}
		}
	}
	return nil
}

func syncIngresses(ctx context.Context, sourceClient, destClient kubernetes.Interface, srcNamespace, dstNamespace string) error {
	log := log.FromContext(ctx)
	ingresses, err := sourceClient.NetworkingV1().Ingresses(srcNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list Ingresses: %v", err)
	}

	for _, ingress := range ingresses.Items {
		if shouldIgnoreResource(&ingress) {
			continue
		}
		ingress.Namespace = dstNamespace
		sanitizeMetadata(&ingress)

		existing, err := destClient.NetworkingV1().Ingresses(dstNamespace).Get(ctx, ingress.Name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				_, err = destClient.NetworkingV1().Ingresses(dstNamespace).Create(ctx, &ingress, metav1.CreateOptions{})
				if err != nil {
					log.Error(err, "Failed to create Ingress", "name", ingress.Name)
					continue
				}
				log.Info("Created Ingress", "name", ingress.Name)
			} else {
				log.Error(err, "Failed to get Ingress", "name", ingress.Name)
				continue
			}
		} else {
			// Type assertion to use networkingv1
			var _ networkingv1.IngressSpec = ingress.Spec

			// Compare ingress specs
			specChanged := false
			
			// Compare relevant fields that should trigger an update
			if !reflect.DeepEqual(ingress.Spec.Rules, existing.Spec.Rules) {
				specChanged = true
			}
			if !reflect.DeepEqual(ingress.Spec.TLS, existing.Spec.TLS) {
				specChanged = true
			}
			
			if specChanged {
				ingress.ResourceVersion = existing.ResourceVersion
				_, err = destClient.NetworkingV1().Ingresses(dstNamespace).Update(ctx, &ingress, metav1.UpdateOptions{})
				if err != nil {
					log.Error(err, "Failed to update Ingress", "name", ingress.Name)
					continue
				}
				log.Info("Updated Ingress", "name", ingress.Name)
			}
		}
	}
	return nil
}

func sanitizeMetadata(obj metav1.Object) {
	obj.SetUID("")
	obj.SetResourceVersion("")
	obj.SetSelfLink("")
	obj.SetCreationTimestamp(metav1.Time{})
	obj.SetManagedFields(nil)
	obj.SetOwnerReferences(nil)
	obj.SetGeneration(0)
	obj.SetFinalizers(nil)
	annotations := obj.GetAnnotations()
	if annotations != nil {
		delete(annotations, "kubectl.kubernetes.io/last-applied-configuration")
		obj.SetAnnotations(annotations)
	}
}
