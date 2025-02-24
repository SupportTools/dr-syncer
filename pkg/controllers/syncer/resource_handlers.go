package syncer

import (
	"context"
	"fmt"

	drv1alpha1 "github.com/supporttools/dr-syncer/pkg/api/v1alpha1"
	"github.com/supporttools/dr-syncer/pkg/controllers/syncer/internal/logging"
	"github.com/supporttools/dr-syncer/pkg/controllers/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// syncConfigMaps synchronizes ConfigMaps between namespaces
func syncConfigMaps(ctx context.Context, syncer *ResourceSyncer, sourceClient kubernetes.Interface, srcNamespace, dstNamespace string, config *drv1alpha1.ImmutableResourceConfig) error {
	logging.Logger.Info(fmt.Sprintf("syncing configmaps from %s to %s", srcNamespace, dstNamespace))

	configMaps, err := sourceClient.CoreV1().ConfigMaps(srcNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list ConfigMaps: %w", err)
	}

	for _, cm := range configMaps.Items {
		if cm.Name == "kube-root-ca.crt" || utils.ShouldIgnoreResource(&cm) {
			continue
		}
		cm.Namespace = dstNamespace
		logging.Logger.Info(fmt.Sprintf("syncing configmap %s from %s to %s", cm.Name, srcNamespace, dstNamespace))
		if err := syncer.SyncResource(ctx, &cm, config); err != nil {
			return fmt.Errorf("failed to sync ConfigMap %s: %w", cm.Name, err)
		}
	}
	return nil
}

// syncSecrets synchronizes Secrets between namespaces
func syncSecrets(ctx context.Context, syncer *ResourceSyncer, sourceClient kubernetes.Interface, srcNamespace, dstNamespace string, config *drv1alpha1.ImmutableResourceConfig) error {
	logging.Logger.Info(fmt.Sprintf("syncing secrets from %s to %s", srcNamespace, dstNamespace))

	secrets, err := sourceClient.CoreV1().Secrets(srcNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list Secrets: %w", err)
	}

	for _, secret := range secrets.Items {
		if utils.ShouldIgnoreResource(&secret) {
			continue
		}
		secret.Namespace = dstNamespace
		logging.Logger.Info(fmt.Sprintf("syncing secret %s from %s to %s", secret.Name, srcNamespace, dstNamespace))
		if err := syncer.SyncResource(ctx, &secret, config); err != nil {
			return fmt.Errorf("failed to sync Secret %s: %w", secret.Name, err)
		}
	}
	return nil
}

// syncDeployments synchronizes Deployments between namespaces
func syncDeployments(ctx context.Context, syncer *ResourceSyncer, sourceClient kubernetes.Interface, srcNamespace, dstNamespace string, scaleToZero bool, config *drv1alpha1.ImmutableResourceConfig) ([]DeploymentScale, error) {
	var scales []DeploymentScale
	logging.Logger.Info(fmt.Sprintf("syncing deployments from %s to %s (scale to zero: %v)", srcNamespace, dstNamespace, scaleToZero))

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
		logging.Logger.Info(fmt.Sprintf("syncing deployment %s from %s to %s (replicas: %d)", deploy.Name, srcNamespace, dstNamespace, *deploy.Spec.Replicas))
		if err := syncer.SyncResource(ctx, &deploy, config); err != nil {
			return nil, fmt.Errorf("failed to sync Deployment %s: %w", deploy.Name, err)
		}
	}
	return scales, nil
}

// syncServices synchronizes Services between namespaces
func syncServices(ctx context.Context, syncer *ResourceSyncer, sourceClient kubernetes.Interface, srcNamespace, dstNamespace string, config *drv1alpha1.ImmutableResourceConfig) error {
	logging.Logger.Info(fmt.Sprintf("syncing services from %s to %s", srcNamespace, dstNamespace))

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
		logging.Logger.Info(fmt.Sprintf("syncing service %s from %s to %s (type: %s)", svc.Name, srcNamespace, dstNamespace, svc.Spec.Type))
		if err := syncer.SyncResource(ctx, &svc, config); err != nil {
			return fmt.Errorf("failed to sync Service %s: %w", svc.Name, err)
		}
	}
	return nil
}

// syncIngresses synchronizes Ingresses between namespaces
func syncIngresses(ctx context.Context, syncer *ResourceSyncer, sourceClient kubernetes.Interface, srcNamespace, dstNamespace string, config *drv1alpha1.ImmutableResourceConfig) error {
	logging.Logger.Info(fmt.Sprintf("syncing ingresses from %s to %s", srcNamespace, dstNamespace))

	ingresses, err := sourceClient.NetworkingV1().Ingresses(srcNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list Ingresses: %w", err)
	}

	for _, ing := range ingresses.Items {
		if utils.ShouldIgnoreResource(&ing) {
			continue
		}
		ing.Namespace = dstNamespace
		logging.Logger.Info(fmt.Sprintf("syncing ingress %s from %s to %s", ing.Name, srcNamespace, dstNamespace))
		if err := syncer.SyncResource(ctx, &ing, config); err != nil {
			return fmt.Errorf("failed to sync Ingress %s: %w", ing.Name, err)
		}
	}
	return nil
}

// syncPersistentVolumeClaims synchronizes PVCs between namespaces
func syncPersistentVolumeClaims(ctx context.Context, syncer *ResourceSyncer, sourceClient kubernetes.Interface, srcNamespace, dstNamespace string, pvcConfig *drv1alpha1.PVCConfig, config *drv1alpha1.ImmutableResourceConfig) error {
	logging.Logger.Info(fmt.Sprintf("syncing persistent volume claims from %s to %s", srcNamespace, dstNamespace))

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

		logging.Logger.Info(fmt.Sprintf("syncing pvc %s from %s to %s (storage class: %s)", pvc.Name, srcNamespace, dstNamespace, *pvc.Spec.StorageClassName))
		if err := syncer.SyncResource(ctx, &pvc, config); err != nil {
			return fmt.Errorf("failed to sync PVC %s: %w", pvc.Name, err)
		}
	}
	return nil
}
