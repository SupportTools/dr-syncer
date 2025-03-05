package leader

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/supporttools/dr-syncer/pkg/agent/ssh"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

const (
	// DefaultLeaseDuration is the default lease duration for leader election
	DefaultLeaseDuration = 15 * time.Second
	// DefaultRenewDeadline is the default renew deadline for leader election
	DefaultRenewDeadline = 10 * time.Second
	// DefaultRetryPeriod is the default retry period for leader election
	DefaultRetryPeriod = 2 * time.Second
	// DefaultKeySecretName is the name of the secret containing SSH keys
	DefaultKeySecretName = "pvc-syncer-agent-keys"
	// DefaultKeyBits is the default number of bits for the SSH key
	DefaultKeyBits = 2048
)

// Manager handles leader election and key management
type Manager struct {
	client    kubernetes.Interface
	namespace string
	podName   string
}

// NewManager creates a new leader election manager
func NewManager(client kubernetes.Interface, namespace string) (*Manager, error) {
	podName := os.Getenv("HOSTNAME")
	if podName == "" {
		return nil, fmt.Errorf("HOSTNAME environment variable not set")
	}

	return &Manager{
		client:    client,
		namespace: namespace,
		podName:   podName,
	}, nil
}

// Run starts the leader election process
func (m *Manager) Run(ctx context.Context) error {
	// Check if the key secret already exists
	_, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, DefaultKeySecretName, metav1.GetOptions{})
	if err == nil {
		// Secret already exists, no need for leader election
		fmt.Printf("SSH key secret %s already exists in namespace %s\n", DefaultKeySecretName, m.namespace)
		return nil
	}

	// Create a new resource lock
	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      "pvc-syncer-agent-leader",
			Namespace: m.namespace,
		},
		Client: m.client.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: m.podName,
		},
	}

	// Start leader election
	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock:            lock,
		ReleaseOnCancel: true,
		LeaseDuration:   DefaultLeaseDuration,
		RenewDeadline:   DefaultRenewDeadline,
		RetryPeriod:     DefaultRetryPeriod,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				// This pod is the leader, generate SSH keys
				fmt.Printf("Pod %s became leader, generating SSH keys\n", m.podName)
				if err := m.generateSSHKeys(); err != nil {
					fmt.Printf("Error generating SSH keys: %v\n", err)
				}
			},
			OnStoppedLeading: func() {
				fmt.Printf("Pod %s stopped leading\n", m.podName)
			},
			OnNewLeader: func(identity string) {
				if identity == m.podName {
					return
				}
				fmt.Printf("New leader elected: %s\n", identity)
			},
		},
	})

	return nil
}

// generateSSHKeys generates SSH keys and stores them in a secret
func (m *Manager) generateSSHKeys() error {
	// Generate a new key pair
	privateKey, publicKey, fingerprint, err := ssh.GenerateKeyPair(DefaultKeyBits)
	if err != nil {
		return fmt.Errorf("failed to generate key pair: %v", err)
	}

	// Create the secret
	if err := ssh.CreateKeySecret(m.client, m.namespace, DefaultKeySecretName, privateKey, publicKey); err != nil {
		return fmt.Errorf("failed to create key secret: %v", err)
	}

	fmt.Printf("Successfully created SSH key with fingerprint %s\n", fingerprint)

	fmt.Printf("Successfully created SSH key secret %s in namespace %s\n", DefaultKeySecretName, m.namespace)
	return nil
}
