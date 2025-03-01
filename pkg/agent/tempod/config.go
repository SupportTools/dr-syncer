package tempod

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// DefaultRsyncConfigMapName is the default name for the rsync ConfigMap
	DefaultRsyncConfigMapName = "dr-syncer-rsync-config"

	// DefaultRsyncConfigKey is the default key for the rsync config in the ConfigMap
	DefaultRsyncConfigKey = "rsyncd.conf"

	// DefaultRsyncConfig is the default rsync configuration
	DefaultRsyncConfig = `
# rsyncd.conf - rsync daemon configuration

# Global settings
uid = root
gid = root
use chroot = no
max connections = 4
timeout = 300
read only = no

# Module for PVC data
[data]
path = /data
comment = PVC Data
read only = no
list = yes
auth users = *
secrets file = /dev/null
`
)

// EnsureRsyncConfigMap ensures that the rsync ConfigMap exists
func EnsureRsyncConfigMap(ctx context.Context, client kubernetes.Interface, namespace string) error {
	// Check if ConfigMap already exists
	_, err := client.CoreV1().ConfigMaps(namespace).Get(ctx, DefaultRsyncConfigMapName, metav1.GetOptions{})
	if err == nil {
		// ConfigMap already exists
		log.WithFields(map[string]interface{}{
			"namespace": namespace,
			"name":      DefaultRsyncConfigMapName,
		}).Debug("Rsync ConfigMap already exists")
		return nil
	}

	// If error is not "not found", return it
	if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to check if ConfigMap exists: %v", err)
	}

	// Create ConfigMap
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DefaultRsyncConfigMapName,
			Namespace: namespace,
			Labels: map[string]string{
				"app":       "dr-syncer",
				"component": "rsync-config",
			},
		},
		Data: map[string]string{
			DefaultRsyncConfigKey: DefaultRsyncConfig,
		},
	}

	_, err = client.CoreV1().ConfigMaps(namespace).Create(ctx, configMap, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create ConfigMap: %v", err)
	}

	log.WithFields(map[string]interface{}{
		"namespace": namespace,
		"name":      DefaultRsyncConfigMapName,
	}).Info("Created rsync ConfigMap")

	return nil
}

// UpdateRsyncConfigMap updates the rsync ConfigMap with custom configuration
func UpdateRsyncConfigMap(ctx context.Context, client kubernetes.Interface, namespace string, config string) error {
	// Get existing ConfigMap
	configMap, err := client.CoreV1().ConfigMaps(namespace).Get(ctx, DefaultRsyncConfigMapName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new ConfigMap with custom config
			configMap = &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      DefaultRsyncConfigMapName,
					Namespace: namespace,
					Labels: map[string]string{
						"app":       "dr-syncer",
						"component": "rsync-config",
					},
				},
				Data: map[string]string{
					DefaultRsyncConfigKey: config,
				},
			}

			_, err = client.CoreV1().ConfigMaps(namespace).Create(ctx, configMap, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create ConfigMap: %v", err)
			}

			log.WithFields(map[string]interface{}{
				"namespace": namespace,
				"name":      DefaultRsyncConfigMapName,
			}).Info("Created rsync ConfigMap with custom config")

			return nil
		}

		return fmt.Errorf("failed to get ConfigMap: %v", err)
	}

	// Update ConfigMap
	if configMap.Data == nil {
		configMap.Data = make(map[string]string)
	}
	configMap.Data[DefaultRsyncConfigKey] = config

	_, err = client.CoreV1().ConfigMaps(namespace).Update(ctx, configMap, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update ConfigMap: %v", err)
	}

	log.WithFields(map[string]interface{}{
		"namespace": namespace,
		"name":      DefaultRsyncConfigMapName,
	}).Info("Updated rsync ConfigMap")

	return nil
}
