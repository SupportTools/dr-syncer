package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/supporttools/dr-syncer/pkg/agent/daemon"
	"github.com/supporttools/dr-syncer/pkg/agent/leader"
	"github.com/supporttools/dr-syncer/pkg/agent/ssh"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

var (
	sshPort    = flag.Int("ssh-port", 2222, "SSH server port")
	kubeconfig = flag.String("kubeconfig", "", "Path to kubeconfig file")
)

func main() {
	flag.Parse()

	// Initialize SSH server
	sshServer, err := ssh.NewServer(*sshPort)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize SSH server: %v\n", err)
		os.Exit(1)
	}

	// Initialize daemon
	d := daemon.NewDaemon(sshServer)

	// Initialize Kubernetes client
	config, err := getKubeConfig(*kubeconfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get Kubernetes config: %v\n", err)
		os.Exit(1)
	}

	// Create Kubernetes clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create Kubernetes client: %v\n", err)
		os.Exit(1)
	}

	// Get namespace
	namespace, err := getNamespace()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get namespace: %v\n", err)
		os.Exit(1)
	}
	d.SetNamespace(namespace)

	// Initialize temporary pod manager
	if err := d.InitTempManager(config); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize temporary pod manager: %v\n", err)
		os.Exit(1)
	}

	// Initialize key system
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := d.InitKeySystem(ctx, clientset); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize key system: %v\n", err)
		os.Exit(1)
	}

	// Initialize leader election manager
	leaderMgr, err := leader.NewManager(clientset, namespace)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize leader election manager: %v\n", err)
		os.Exit(1)
	}

	// Start leader election in background
	leaderCtx, leaderCancel := context.WithCancel(context.Background())
	defer leaderCancel()
	go func() {
		if err := leaderMgr.Run(leaderCtx); err != nil {
			fmt.Fprintf(os.Stderr, "Leader election failed: %v\n", err)
		}
	}()

	// Start the daemon
	if err := d.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start daemon: %v\n", err)
		os.Exit(1)
	}

	// Handle shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), daemon.DefaultShutdownTimeout)
	defer shutdownCancel()

	// Clean up temporary pods
	if err := d.CleanupTempPods(shutdownCtx); err != nil {
		fmt.Fprintf(os.Stderr, "Error cleaning up temporary pods: %v\n", err)
	}

	// Stop the daemon
	if err := d.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "Error during shutdown: %v\n", err)
		os.Exit(1)
	}
}

// getNamespace returns the namespace to use for the daemon
func getNamespace() (string, error) {
	// Try to get namespace from environment variable
	namespace := os.Getenv("POD_NAMESPACE")
	if namespace != "" {
		return namespace, nil
	}

	// Try to get namespace from service account
	data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err == nil && len(data) > 0 {
		return string(data), nil
	}

	// Default to "default" namespace
	return "default", nil
}

// getKubeConfig returns the Kubernetes client configuration
func getKubeConfig(kubeconfigPath string) (*rest.Config, error) {
	// If kubeconfig is provided, use it
	if kubeconfigPath != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	}

	// Try in-cluster config
	config, err := rest.InClusterConfig()
	if err == nil {
		return config, nil
	}

	// Try default kubeconfig location
	home := homedir.HomeDir()
	if home != "" {
		kubeconfigPath = filepath.Join(home, ".kube", "config")
		if _, err := os.Stat(kubeconfigPath); err == nil {
			return clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		}
	}

	return nil, fmt.Errorf("could not find kubeconfig")
}
