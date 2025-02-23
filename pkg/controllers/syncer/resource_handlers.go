package syncer

import (
	"context"
	"fmt"

	drv1alpha1 "github.com/supporttools/dr-syncer/pkg/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"github.com/supporttools/dr-syncer/pkg/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// syncConfigMaps synchronizes ConfigMaps between namespaces
func syncConfigMaps(ctx context.Context, syncer *ResourceSyncer, sourceClient kubernetes.Interface, srcNamespace, dstNamespace string, config *drv1alpha1.ImmutableResourceConfig) error {
	log := log.FromContext(ctx)
	log.V(1).Info("syncing configmaps",
		"sourceNS", srcNamespace,
		"destNS", dstNamespace)

	configMaps, err := sourceClient.CoreV1().ConfigMaps(srcNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list ConfigMaps: %w", err)
	}

	for _, cm := range configMaps.Items {
		if cm.Name == "kube-root-ca.crt" || utils.ShouldIgnoreResource(&cm) {
			continue
		}
		cm.Namespace = dstNamespace
		if err := syncer.SyncResource(ctx, &cm, config); err != nil {
			return fmt.Errorf("failed to sync ConfigMap %s: %w", cm.Name, err)
		}
	}
	return nil
}

// syncSecrets synchronizes Secrets between namespaces
func syncSecrets(ctx context.Context, syncer *ResourceSyncer, sourceClient kubernetes.Interface, srcNamespace, dstNamespace string, config *drv1alpha1.ImmutableResourceConfig) error {
	log := log.FromContext(ctx)
	log.V(1).Info("syncing secrets",
		"sourceNS", srcNamespace,
		"destNS", dstNamespace)

	secrets, err := sourceClient.CoreV1().Secrets(srcNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list Secrets: %w", err)
	}

	for _, secret := range secrets.Items {
		if utils.ShouldIgnoreResource(&secret) {
			continue
		}
		secret.Namespace = dstNamespace
		if err := syncer.SyncResource(ctx, &secret, config); err != nil {
			return fmt.Errorf("failed to sync Secret %s: %w", secret.Name, err)
		}
	}
	return nil
}

// syncDeployments synchronizes Deployments between namespaces
func syncDeployments(ctx context.Context, syncer *ResourceSyncer, sourceClient kubernetes.Interface, srcNamespace, dstNamespace string, scaleToZero bool, config *drv1alpha1.ImmutableResourceConfig) ([]DeploymentScale, error) {
	var scales []DeploymentScale
	log := log.FromContext(ctx)
	log.V(1).Info("syncing deployments",
		"sourceNS", srcNamespace,
		"destNS", dstNamespace,
		"scaleToZero", scaleToZero)

	deployments, err := sourceClient.AppsV1().Deployments(srcNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list Deployments: %w", err)
	}

	for _, deploy := range deployments.Items {
		if utils.ShouldIgnoreResource(&deploy) {
			continue
		}

		// Store original replicas
		originalReplicas := int32(0)
		if deploy.Spec.Replicas != nil {
			originalReplicas = *deploy.Spec.Replicas
		}

		// Add to scales list
		scales = append(scales, DeploymentScale{
			Name:     deploy.Name,
			Replicas: originalReplicas,
			SyncTime: metav1.Now(),
		})

		// Store information in annotations
		if deploy.Annotations == nil {
			deploy.Annotations = make(map[string]string)
		}
		deploy.Annotations["dr-syncer.io/original-replicas"] = fmt.Sprintf("%d", originalReplicas)
		deploy.Annotations["dr-syncer.io/source-namespace"] = srcNamespace

		// Check for scale override
		if override, exists := deploy.Labels[utils.ScaleOverrideLabel]; exists {
			if replicas, err := utils.ParseInt32(override); err == nil {
				deploy.Spec.Replicas = &replicas
			}
		} else if scaleToZero {
			zero := int32(0)
			deploy.Spec.Replicas = &zero
		}

		deploy.Namespace = dstNamespace
		if err := syncer.SyncResource(ctx, &deploy, config); err != nil {
			return nil, fmt.Errorf("failed to sync Deployment %s: %w", deploy.Name, err)
		}
	}
	return scales, nil
}

// syncServices synchronizes Services between namespaces
func syncServices(ctx context.Context, syncer *ResourceSyncer, sourceClient kubernetes.Interface, srcNamespace, dstNamespace string, config *drv1alpha1.ImmutableResourceConfig) error {
	log := log.FromContext(ctx)
	log.V(1).Info("syncing services",
		"sourceNS", srcNamespace,
		"destNS", dstNamespace)

	services, err := sourceClient.CoreV1().Services(srcNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list Services: %w", err)
	}

	for _, svc := range services.Items {
		if utils.ShouldIgnoreResource(&svc) {
			continue
		}
		svc.Namespace = dstNamespace
		svc.Spec.ClusterIP = ""
		svc.Spec.ClusterIPs = nil
		if err := syncer.SyncResource(ctx, &svc, config); err != nil {
			return fmt.Errorf("failed to sync Service %s: %w", svc.Name, err)
		}
	}
	return nil
}

// syncIngresses synchronizes Ingresses between namespaces
func syncIngresses(ctx context.Context, syncer *ResourceSyncer, sourceClient kubernetes.Interface, srcNamespace, dstNamespace string, config *drv1alpha1.ImmutableResourceConfig) error {
	log := log.FromContext(ctx)
	log.V(1).Info("syncing ingresses",
		"sourceNS", srcNamespace,
		"destNS", dstNamespace)

	ingresses, err := sourceClient.NetworkingV1().Ingresses(srcNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list Ingresses: %w", err)
	}

	for _, ing := range ingresses.Items {
		if utils.ShouldIgnoreResource(&ing) {
			continue
		}
		ing.Namespace = dstNamespace
		if err := syncer.SyncResource(ctx, &ing, config); err != nil {
			return fmt.Errorf("failed to sync Ingress %s: %w", ing.Name, err)
		}
	}
	return nil
}

// syncPersistentVolumeClaims synchronizes PVCs between namespaces
func syncPersistentVolumeClaims(ctx context.Context, syncer *ResourceSyncer, sourceClient kubernetes.Interface, srcNamespace, dstNamespace string, pvcConfig *drv1alpha1.PVCConfig, config *drv1alpha1.ImmutableResourceConfig) error {
	log := log.FromContext(ctx)
	log.V(1).Info("syncing persistent volume claims",
		"sourceNS", srcNamespace,
		"destNS", dstNamespace,
		"config", fmt.Sprintf("%+v", pvcConfig))

	pvcs, err := sourceClient.CoreV1().PersistentVolumeClaims(srcNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list PVCs: %w", err)
	}

	for _, pvc := range pvcs.Items {
		if utils.ShouldIgnoreResource(&pvc) {
			continue
		}

		pvc.Namespace = dstNamespace

		// Apply storage class mapping if configured
		if pvcConfig != nil && len(pvcConfig.StorageClassMappings) > 0 {
			for _, mapping := range pvcConfig.StorageClassMappings {
				if pvc.Spec.StorageClassName != nil && *pvc.Spec.StorageClassName == mapping.From {
					storageClass := mapping.To
					pvc.Spec.StorageClassName = &storageClass
					break
				}
			}
		}

		// Apply access mode mapping if configured
		if pvcConfig != nil && len(pvcConfig.AccessModeMappings) > 0 {
			for _, mapping := range pvcConfig.AccessModeMappings {
				for i, mode := range pvc.Spec.AccessModes {
					if string(mode) == mapping.From {
						pvc.Spec.AccessModes[i] = corev1.PersistentVolumeAccessMode(mapping.To)
					}
				}
			}
		}

		if err := syncer.SyncResource(ctx, &pvc, config); err != nil {
			return fmt.Errorf("failed to sync PVC %s: %w", pvc.Name, err)
		}
	}
	return nil
}
