package cli

import (
	"context"
	"fmt"

	"github.com/supporttools/dr-syncer/pkg/logging"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Run executes the CLI operation with the given configuration
func Run(config *Config) error {
	log := logging.SetupLogging()
	log.Info("Starting DR Syncer CLI operation")

	// Create Kubernetes clients
	sourceClient, destClient, sourceDynamicClient, destDynamicClient, err := setupClients(config)
	if err != nil {
		return fmt.Errorf("failed to setup Kubernetes clients: %v", err)
	}

	// Create context
	ctx := context.Background()

	// Ensure destination namespace exists
	if err := ensureNamespace(ctx, destClient, config.DestNamespace); err != nil {
		return fmt.Errorf("failed to ensure destination namespace exists: %v", err)
	}

	// Execute the appropriate mode
	switch config.Mode {
	case "Stage":
		log.Info("Executing Stage mode")
		if err := executeStageModeSync(ctx, sourceClient, destClient, sourceDynamicClient, destDynamicClient, config); err != nil {
			return fmt.Errorf("stage mode failed: %v", err)
		}

	case "Cutover":
		log.Info("Executing Cutover mode")
		if err := executeCutoverModeSync(ctx, sourceClient, destClient, sourceDynamicClient, destDynamicClient, config); err != nil {
			return fmt.Errorf("cutover mode failed: %v", err)
		}

	case "Failback":
		log.Info("Executing Failback mode")
		if err := executeFailbackModeSync(ctx, sourceClient, destClient, sourceDynamicClient, destDynamicClient, config); err != nil {
			return fmt.Errorf("failback mode failed: %v", err)
		}

	default:
		return fmt.Errorf("unknown mode: %s", config.Mode)
	}

	log.Info("DR Syncer CLI operation completed successfully")
	return nil
}

// ensureNamespace checks if the namespace exists and creates it if it doesn't
func ensureNamespace(ctx context.Context, client kubernetes.Interface, namespace string) error {
	log := logging.SetupLogging()

	log.Infof("Checking if namespace %s exists", namespace)
	// Use the proper Kubernetes client API with concrete types
	_, err := client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err == nil {
		// Namespace exists
		log.Infof("Namespace %s already exists", namespace)
		return nil
	}

	// Check if error is "not found"
	if !errors.IsNotFound(err) {
		// Unexpected error
		return fmt.Errorf("error checking namespace %s: %v", namespace, err)
	}

	// Create namespace
	log.Infof("Creating namespace %s", namespace)
	nsObj := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}
	_, err = client.CoreV1().Namespaces().Create(ctx, nsObj, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("error creating namespace %s: %v", namespace, err)
	}

	log.Infof("Namespace %s created successfully", namespace)
	return nil
}
