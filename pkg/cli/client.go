package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/supporttools/dr-syncer/pkg/logging"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// setupClients creates Kubernetes clients for source and destination clusters
func setupClients(config *Config) (kubernetes.Interface, kubernetes.Interface, dynamic.Interface, dynamic.Interface, error) {
	log := logging.SetupLogging()

	// Create source client
	log.Info("Creating source cluster client")
	sourceConfig, err := loadKubeconfig(config.SourceKubeconfig)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to load source kubeconfig: %v", err)
	}

	sourceClient, err := kubernetes.NewForConfig(sourceConfig)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to create source Kubernetes client: %v", err)
	}

	sourceDynamicClient, err := dynamic.NewForConfig(sourceConfig)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to create source dynamic client: %v", err)
	}

	// Create destination client
	log.Info("Creating destination cluster client")
	destConfig, err := loadKubeconfig(config.DestKubeconfig)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to load destination kubeconfig: %v", err)
	}

	destClient, err := kubernetes.NewForConfig(destConfig)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to create destination Kubernetes client: %v", err)
	}

	destDynamicClient, err := dynamic.NewForConfig(destConfig)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to create destination dynamic client: %v", err)
	}

	// Test connectivity to source cluster
	log.Info("Testing connectivity to source cluster")
	_, err = sourceClient.Discovery().ServerVersion()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to connect to source cluster: %v", err)
	}

	// Test connectivity to destination cluster
	log.Info("Testing connectivity to destination cluster")
	_, err = destClient.Discovery().ServerVersion()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to connect to destination cluster: %v", err)
	}

	return sourceClient, destClient, sourceDynamicClient, destDynamicClient, nil
}

// loadKubeconfig loads a kubeconfig file from the given path
func loadKubeconfig(kubeconfigPath string) (*rest.Config, error) {
	// If path starts with ~, expand it
	if kubeconfigPath[:1] == "~" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %v", err)
		}
		kubeconfigPath = filepath.Join(homeDir, kubeconfigPath[1:])
	}

	// Check if file exists
	if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("kubeconfig file not found: %s", kubeconfigPath)
	}

	// Load the kubeconfig file
	return clientcmd.BuildConfigFromFlags("", kubeconfigPath)
}
