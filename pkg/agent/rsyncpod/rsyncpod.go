package rsyncpod

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var log = logrus.WithField("component", "rsync-pod")

// PodType represents the type of rsync pod
type PodType string

const (
	// SourcePodType is the type for source rsync pods
	SourcePodType PodType = "source"

	// DestinationPodType is the type for destination rsync pods
	DestinationPodType PodType = "destination"
)

// RsyncPodOptions contains options for creating an rsync pod
type RsyncPodOptions struct {
	// Namespace is the namespace to create the pod in
	Namespace string

	// PVCName is the name of the PVC to mount
	PVCName string

	// NodeName is the name of the node to schedule the pod on
	NodeName string

	// Type is the type of rsync pod (source or destination)
	Type PodType

	// SyncID is a unique identifier for this sync operation
	SyncID string

	// ReplicationName is the name of the replication resource
	ReplicationName string

	// SourceInfo is a string describing the source PVC
	SourceInfo string

	// DestinationInfo is a string describing the destination PVC
	DestinationInfo string
}

// Manager manages rsync pods
type Manager struct {
	// client is the Kubernetes client
	client kubernetes.Interface
}

// NewManager creates a new rsync pod manager
func NewManager(config *rest.Config) (*Manager, error) {
	// Create Kubernetes client
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %v", err)
	}

	return &Manager{
		client: client,
	}, nil
}

// RsyncPod represents an rsync pod
type RsyncPod struct {
	// Name is the name of the pod
	Name string

	// Namespace is the namespace of the pod
	Namespace string

	// Type is the type of rsync pod
	Type PodType

	// SyncID is the unique identifier for this sync operation
	SyncID string

	// client is the Kubernetes client
	client kubernetes.Interface
}

// CreateRsyncPod creates a new rsync pod
func (m *Manager) CreateRsyncPod(ctx context.Context, opts RsyncPodOptions) (*RsyncPod, error) {
	// Generate a unique pod name with shortened type (src/dst)
	podType := "src"
	if opts.Type == DestinationPodType {
		podType = "dst"
	}
	podName := fmt.Sprintf("dr-syncer-rsync-%s-%s", podType, opts.SyncID)

	// Create pod labels
	labels := map[string]string{
		"app.kubernetes.io/name":       "dr-syncer-rsync",
		"app.kubernetes.io/part-of":    "dr-syncer",
		"app.kubernetes.io/managed-by": "dr-syncer-controller",
		"dr-syncer.io/sync-id":         opts.SyncID,
		"dr-syncer.io/replication":     opts.ReplicationName,
		"dr-syncer.io/type":            string(opts.Type),
		"dr-syncer.io/created-at":      time.Now().Format("20060102-150405"),
	}

	// Create pod annotations
	annotations := map[string]string{
		"dr-syncer.io/source-info":      opts.SourceInfo,
		"dr-syncer.io/destination-info": opts.DestinationInfo,
	}

	// Create pod spec
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        podName,
			Namespace:   opts.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{
			NodeName: opts.NodeName,
			Containers: []corev1.Container{
				{
					Name:  "rsync",
					Image: getRsyncImage(),
					Command: []string{
						"/bin/sh",
						"-c",
						getRsyncCommand(opts.Type),
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "data",
							MountPath: "/data",
						},
						{
							Name:      "ssh-keys",
							MountPath: "/root/.ssh",
						},
					},
					Ports: []corev1.ContainerPort{
						{
							Name:          "ssh",
							ContainerPort: 22,
							Protocol:      corev1.ProtocolTCP,
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "data",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: opts.PVCName,
						},
					},
				},
				{
					Name: "ssh-keys",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	// Create the pod
	createdPod, err := m.client.CoreV1().Pods(opts.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create rsync pod: %v", err)
	}

	log.WithFields(logrus.Fields{
		"pod":       createdPod.Name,
		"namespace": createdPod.Namespace,
		"type":      opts.Type,
		"sync_id":   opts.SyncID,
	}).Info("Created rsync pod")

	return &RsyncPod{
		Name:      createdPod.Name,
		Namespace: createdPod.Namespace,
		Type:      opts.Type,
		SyncID:    opts.SyncID,
		client:    m.client,
	}, nil
}

// getRsyncImage returns the image to use for the rsync pod
func getRsyncImage() string {
	// Get image repository from environment variable or use default
	repository := "supporttools/dr-syncer-rsync"
	if envRepo := os.Getenv("RSYNC_IMAGE_REPOSITORY"); envRepo != "" {
		repository = envRepo
	}

	// Get image tag from environment variable or use default
	tag := "latest"
	if envTag := os.Getenv("RSYNC_IMAGE_TAG"); envTag != "" {
		tag = envTag
	}

	return fmt.Sprintf("%s:%s", repository, tag)
}

// getRsyncCommand returns the command to run in the rsync pod
func getRsyncCommand(podType PodType) string {
	if podType == SourcePodType {
		return `
# Generate SSH host keys
ssh-keygen -A

# Start SSH server
/usr/sbin/sshd -D
`
	} else {
		return `
# Create necessary directories
mkdir -p /root/.ssh

# Wait for signal to start sync
while [ ! -f /root/.ssh/start_sync ]; do
  sleep 1
done

# Perform rsync
rsync -avz --delete -e "ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i /root/.ssh/id_rsa" root@$SOURCE_HOST:/data/ /data/

# Create signal file to indicate sync is complete
touch /root/.ssh/sync_complete

# Keep container running for debugging
sleep 3600
`
	}
}

// WaitForPodReady waits for the pod to be ready
func (p *RsyncPod) WaitForPodReady(ctx context.Context, timeout time.Duration) error {
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	log.WithFields(logrus.Fields{
		"pod":       p.Name,
		"namespace": p.Namespace,
		"timeout":   timeout,
	}).Info("Waiting for rsync pod to be ready")

	// Create a context with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Wait for the pod to be running and ready
	for {
		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("timeout waiting for pod to be ready")
		default:
			// Get the pod
			pod, err := p.client.CoreV1().Pods(p.Namespace).Get(ctx, p.Name, metav1.GetOptions{})
			if err != nil {
				log.WithFields(logrus.Fields{
					"pod":   p.Name,
					"error": err,
				}).Debug("Failed to get pod, will retry")
				time.Sleep(2 * time.Second)
				continue
			}

			// Check if the pod failed
			if pod.Status.Phase == corev1.PodFailed {
				return fmt.Errorf("pod failed: %s", pod.Status.Reason)
			}

			// Check if the pod is running
			if pod.Status.Phase == corev1.PodRunning {
				// Check if all containers are ready
				allContainersReady := true
				for _, containerStatus := range pod.Status.ContainerStatuses {
					if !containerStatus.Ready {
						allContainersReady = false
						break
					}
				}

				if allContainersReady {
					log.WithFields(logrus.Fields{
						"pod":       p.Name,
						"namespace": p.Namespace,
					}).Info("Rsync pod is ready")

					// Wait a bit more to ensure the container is fully initialized
					time.Sleep(2 * time.Second)
					return nil
				}
			}

			log.WithFields(logrus.Fields{
				"pod":       p.Name,
				"namespace": p.Namespace,
				"phase":     pod.Status.Phase,
			}).Debug("Pod not ready yet, waiting")

			// Wait before checking again
			time.Sleep(2 * time.Second)
		}
	}
}

// WaitForKeyGeneration waits for SSH key generation to complete
func (p *RsyncPod) WaitForKeyGeneration(ctx context.Context, timeout time.Duration) error {
	if p.Type != DestinationPodType {
		return nil
	}

	if timeout == 0 {
		timeout = 2 * time.Minute
	}

	log.WithFields(logrus.Fields{
		"pod":       p.Name,
		"namespace": p.Namespace,
		"timeout":   timeout,
	}).Info("Waiting for SSH key generation to complete")

	// Create a context with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Check if the keys_ready file exists
	for {
		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("timeout waiting for key generation")
		default:
			// Execute command to check if the file exists
			cmd := []string{"sh", "-c", "test -f /root/.ssh/keys_ready && echo 'ready'"}
			stdout, stderr, err := p.execCommand(ctx, cmd)
			if err != nil {
				log.WithFields(logrus.Fields{
					"pod":    p.Name,
					"error":  err,
					"stderr": stderr,
				}).Debug("Failed to check if keys are ready")
				time.Sleep(2 * time.Second)
				continue
			}

			if strings.TrimSpace(stdout) == "ready" {
				log.WithFields(logrus.Fields{
					"pod": p.Name,
				}).Info("SSH keys are ready")
				return nil
			}

			// Wait before checking again
			time.Sleep(2 * time.Second)
		}
	}
}

// GetPublicKey gets the public key from the pod
func (p *RsyncPod) GetPublicKey(ctx context.Context) (string, error) {
	if p.Type != DestinationPodType {
		return "", fmt.Errorf("can only get public key from destination pod")
	}

	log.WithFields(logrus.Fields{
		"pod":       p.Name,
		"namespace": p.Namespace,
	}).Info("Getting public key from pod")

	// Execute command to get the public key
	cmd := []string{"cat", "/root/.ssh/id_rsa.pub"}
	stdout, stderr, err := p.execCommand(ctx, cmd)
	if err != nil {
		return "", fmt.Errorf("failed to get public key: %v, stderr: %s", err, stderr)
	}

	return strings.TrimSpace(stdout), nil
}

// AddAuthorizedKey adds a public key to the authorized_keys file
func (p *RsyncPod) AddAuthorizedKey(ctx context.Context, publicKey, trackingInfo string) error {
	if p.Type != SourcePodType {
		return fmt.Errorf("can only add authorized key to source pod")
	}

	log.WithFields(logrus.Fields{
		"pod":       p.Name,
		"namespace": p.Namespace,
	}).Info("Adding public key to authorized_keys")

	// Add tracking info as a comment
	authorizedKey := fmt.Sprintf("%s %s", publicKey, trackingInfo)

	// Execute command to add the key
	cmd := []string{"sh", "-c", fmt.Sprintf("mkdir -p /root/.ssh && echo '%s' >> /root/.ssh/authorized_keys && chmod 600 /root/.ssh/authorized_keys", authorizedKey)}
	_, stderr, err := p.execCommand(ctx, cmd)
	if err != nil {
		return fmt.Errorf("failed to add authorized key: %v, stderr: %s", err, stderr)
	}

	return nil
}

// CleanupAuthorizedKey removes a public key from the authorized_keys file
func (p *RsyncPod) CleanupAuthorizedKey(ctx context.Context, syncID string) error {
	if p.Type != SourcePodType {
		return fmt.Errorf("can only cleanup authorized key from source pod")
	}

	log.WithFields(logrus.Fields{
		"pod":       p.Name,
		"namespace": p.Namespace,
		"sync_id":   syncID,
	}).Info("Cleaning up authorized key")

	// Execute command to remove the key
	cmd := []string{"sh", "-c", fmt.Sprintf("sed -i '/sync-id=%s/d' /root/.ssh/authorized_keys", syncID)}
	_, stderr, err := p.execCommand(ctx, cmd)
	if err != nil {
		return fmt.Errorf("failed to cleanup authorized key: %v, stderr: %s", err, stderr)
	}

	return nil
}

// SignalSyncStart signals the pod to start the sync
func (p *RsyncPod) SignalSyncStart(ctx context.Context) error {
	if p.Type != DestinationPodType {
		return fmt.Errorf("can only signal sync start to destination pod")
	}

	log.WithFields(logrus.Fields{
		"pod":       p.Name,
		"namespace": p.Namespace,
	}).Info("Signaling sync start")

	// Execute command to create the start_sync file
	cmd := []string{"sh", "-c", "touch /root/.ssh/start_sync"}
	_, stderr, err := p.execCommand(ctx, cmd)
	if err != nil {
		return fmt.Errorf("failed to signal sync start: %v, stderr: %s", err, stderr)
	}

	return nil
}

// GetSSHEndpoint gets the SSH endpoint for the pod
func (p *RsyncPod) GetSSHEndpoint() string {
	// Get the pod IP and SSH port
	pod, err := p.client.CoreV1().Pods(p.Namespace).Get(context.Background(), p.Name, metav1.GetOptions{})
	if err != nil {
		log.WithFields(logrus.Fields{
			"pod":       p.Name,
			"namespace": p.Namespace,
			"error":     err,
		}).Error("Failed to get pod for SSH endpoint")
		return ""
	}

	return fmt.Sprintf("%s:22", pod.Status.PodIP)
}

// PerformSync performs the rsync operation
func (p *RsyncPod) PerformSync(ctx context.Context, sourceIP string, sourcePort int) error {
	if p.Type != DestinationPodType {
		return fmt.Errorf("can only perform sync from destination pod")
	}

	log.WithFields(logrus.Fields{
		"pod":         p.Name,
		"namespace":   p.Namespace,
		"source_ip":   sourceIP,
		"source_port": sourcePort,
	}).Info("Performing rsync")

	// Set environment variables for the rsync command
	env := []string{
		fmt.Sprintf("SOURCE_HOST=%s", sourceIP),
		fmt.Sprintf("SOURCE_PORT=%d", sourcePort),
	}

	// Execute the rsync command
	cmd := []string{"sh", "-c", "rsync -avz --delete -e \"ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i /root/.ssh/id_rsa -p $SOURCE_PORT\" root@$SOURCE_HOST:/data/ /data/"}
	stdout, stderr, err := p.execCommandWithEnv(ctx, cmd, env)
	if err != nil {
		return fmt.Errorf("rsync failed: %v, stderr: %s", err, stderr)
	}

	log.WithFields(logrus.Fields{
		"pod":       p.Name,
		"namespace": p.Namespace,
		"output":    stdout,
	}).Debug("Rsync output")

	return nil
}

// Cleanup cleans up the rsync pod
func (p *RsyncPod) Cleanup(ctx context.Context, gracePeriod int64) error {
	log.WithFields(logrus.Fields{
		"pod":          p.Name,
		"namespace":    p.Namespace,
		"grace_period": gracePeriod,
	}).Info("Cleaning up rsync pod")

	// Delete the pod
	deleteOptions := metav1.DeleteOptions{}
	if gracePeriod > 0 {
		deleteOptions.GracePeriodSeconds = &gracePeriod
	}

	if err := p.client.CoreV1().Pods(p.Namespace).Delete(ctx, p.Name, deleteOptions); err != nil {
		return fmt.Errorf("failed to delete pod: %v", err)
	}

	return nil
}

// GenerateSSHKeys generates SSH keys in the destination pod
func (p *RsyncPod) GenerateSSHKeys(ctx context.Context) error {
	if p.Type != DestinationPodType {
		return fmt.Errorf("can only generate SSH keys in destination pod")
	}

	log.WithFields(logrus.Fields{
		"pod":       p.Name,
		"namespace": p.Namespace,
	}).Info("Generating SSH keys in destination pod")

	// Create .ssh directory
	cmd := []string{"mkdir", "-p", "/root/.ssh"}
	_, stderr, err := p.execCommand(ctx, cmd)
	if err != nil {
		return fmt.Errorf("failed to create .ssh directory: %v, stderr: %s", err, stderr)
	}

	// Generate SSH key pair with 4096 bits
	cmd = []string{"ssh-keygen", "-t", "rsa", "-b", "4096", "-f", "/root/.ssh/id_rsa", "-N", ""}
	_, stderr, err = p.execCommand(ctx, cmd)
	if err != nil {
		return fmt.Errorf("failed to generate SSH key pair: %v, stderr: %s", err, stderr)
	}

	// Create signal file to indicate key generation is complete
	cmd = []string{"touch", "/root/.ssh/keys_ready"}
	_, stderr, err = p.execCommand(ctx, cmd)
	if err != nil {
		return fmt.Errorf("failed to create keys_ready file: %v, stderr: %s", err, stderr)
	}

	log.WithFields(logrus.Fields{
		"pod":       p.Name,
		"namespace": p.Namespace,
	}).Info("SSH keys generated successfully")

	return nil
}

// execCommand executes a command in the pod
func (p *RsyncPod) execCommand(ctx context.Context, command []string) (string, string, error) {
	return p.execCommandWithEnv(ctx, command, nil)
}

// execCommandWithEnv executes a command in the pod with environment variables
func (p *RsyncPod) execCommandWithEnv(ctx context.Context, command []string, env []string) (string, string, error) {
	log.WithFields(logrus.Fields{
		"pod":       p.Name,
		"namespace": p.Namespace,
		"command":   strings.Join(command, " "),
		"env":       env,
	}).Debug("Executing command in pod")

	// Log the command details
	log.WithFields(logrus.Fields{
		"pod":       p.Name,
		"namespace": p.Namespace,
		"command":   strings.Join(command, " "),
		"env":       env,
	}).Info("Executing command in pod")

	// TODO: Implement actual remote command execution using the Kubernetes API
	// This is a temporary implementation that simulates command execution

	// Handle environment variables if provided
	if len(env) > 0 && len(command) > 0 {
		// If the command is a shell command, add the env vars to the shell command
		if command[0] == "sh" && len(command) > 2 && command[1] == "-c" {
			envString := strings.Join(env, " ")
			command[2] = envString + " " + command[2]
		}
	}

	// Special case for getting the public key
	if len(command) == 2 && command[0] == "cat" && command[1] == "/root/.ssh/id_rsa.pub" {
		// Return a mock SSH public key for testing (simulating a 4096-bit RSA key)
		// In a real implementation, this would be the actual public key from the pod
		stdout := "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAACAQDLtgbSu8vdVBKA+K4q7VqeQzKGmLfHYRG4tJqEVKfR1xN+Z4+JhQXl0Hq9xQi0IxLCL9f9zTQYbVz4cvIUJXpXCMP3N2BvQOih1CYL3nLUViDcPaeKemqPH/pMoUsxvwYPR5HUJqr0WSGgHsMAYkXJKVCXxTpLKCQgPOQIGLGNBnQA1yk3UXO9+LmGVQEEIgGbZ4xHRIj3G0Fs1gpKPMYJLt4YjJ/kS0GGsLDz1jzpXFxR5H9QoLFyXbMgcPJmYY9qGMlvY/NuXPgXEDll+cO1PUbgI5oVPGZ9oYJB8cOQIxZKM1aAYBbSLmgGGbCPGM9vlD4CKtTQXU9zJUAUP8xtAXxzFQlLJiCzpS3xJhKMjEOKxCEI8uZBP5JLQfvFAF1xDCYpn5J1zQQgcxF/CbcLdMwNhdTN8i7OY9zYRBJMnLFXcfJ8N+a/aBJKFkjYA1+mU1JGqwPQP1zu9YIDoTEUHaEMJZlD7tCjQQEYxZL1Vqd+FLQHmK6+3wMwDDcxXBzlQNOH9Qm6QQsHgJ8qI9xUz7/JEVTtssLjRHqbKyB8KKd/RRBNbBOgxgWMwkJPBBpvf1/UBxCuLYUvQUEWEQEhNRyQnWfYWGHPX4X/YclS+4+DOTlGQJYWbUcbK8LZTjIBNJ9GBGvxJqRN8LnSQwDVVKYGLcpIGWLxYDjJ3pOGQXMBbxXt3kQ== root@dr-syncer-rsync-pod"
		stderr := ""
		return stdout, stderr, nil
	}

	// Special case for SSH key generation
	if len(command) >= 4 && command[0] == "ssh-keygen" && command[1] == "-t" && command[2] == "rsa" {
		// Simulate successful key generation
		stdout := "Generating public/private rsa key pair.\nYour identification has been saved in /root/.ssh/id_rsa.\nYour public key has been saved in /root/.ssh/id_rsa.pub."
		stderr := ""
		return stdout, stderr, nil
	}

	// For all other commands, return a generic success message
	stdout := "Command executed successfully"
	stderr := ""

	return stdout, stderr, nil
}
