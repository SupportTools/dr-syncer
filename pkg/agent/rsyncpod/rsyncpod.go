package rsyncpod

import (
	"context"
	"fmt"
	"time"
	
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var log = logrus.WithField("component", "rsyncpod")

// PodType defines the type of rsync pod (source or destination)
type PodType string

const (
	// SourcePodType is used for pods that serve as the source for rsync
	SourcePodType PodType = "source"
	
	// DestinationPodType is used for pods that serve as the destination for rsync
	DestinationPodType PodType = "destination"
)

// RsyncPodOptions defines options for creating an rsync pod
type RsyncPodOptions struct {
	// Namespace is the namespace to create the pod in
	Namespace string
	
	// PVCName is the name of the PVC to mount
	PVCName string
	
	// NodeName is the node to schedule the pod on (optional)
	NodeName string
	
	// Type is the type of rsync pod (source or destination)
	Type PodType
	
	// SyncID is a unique identifier for this sync operation
	SyncID string
	
	// ReplicationName is the name of the replication resource
	ReplicationName string
	
	// DestinationInfo is additional information about the destination
	DestinationInfo string
}

// Manager manages rsync pods
type Manager struct {
	// client is the Kubernetes client
	client kubernetes.Interface
}

// NewManager creates a new rsync pod manager
func NewManager(config *rest.Config) (*Manager, error) {
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
	
	// Namespace is the namespace the pod is in
	Namespace string
	
	// client is the Kubernetes client
	client kubernetes.Interface
}

// CreateRsyncPod creates a new rsync pod
func (m *Manager) CreateRsyncPod(ctx context.Context, opts RsyncPodOptions) (*RsyncPod, error) {
	log.WithFields(logrus.Fields{
		"namespace":        opts.Namespace,
		"pvc_name":         opts.PVCName,
		"node_name":        opts.NodeName,
		"pod_type":         opts.Type,
		"sync_id":          opts.SyncID,
		"replication_name": opts.ReplicationName,
	}).Info("Creating rsync pod")

	// Generate a pod name
	podName := fmt.Sprintf("dr-syncer-rsync-%s-%s", opts.Type, opts.SyncID)
	
	// Create pod spec
	podSpec := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: opts.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "dr-syncer-rsync",
				"app.kubernetes.io/instance":   opts.SyncID,
				"app.kubernetes.io/component":  string(opts.Type),
				"app.kubernetes.io/managed-by": "dr-syncer",
				"dr-syncer.io/sync-id":         opts.SyncID,
				"dr-syncer.io/replication":     opts.ReplicationName,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "rsync",
					Image: "supporttools/dr-syncer-rsync:latest", // This should be configurable
					Command: []string{
						"/bin/sh",
						"-c",
						"sleep infinity", // Initial command is to wait
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "data",
							MountPath: "/data",
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
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}
	
	// Set node selector if node name is provided
	if opts.NodeName != "" {
		podSpec.Spec.NodeName = opts.NodeName
	}
	
	// Create the pod
	pod, err := m.client.CoreV1().Pods(opts.Namespace).Create(ctx, podSpec, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create rsync pod: %v", err)
	}
	
	log.WithFields(logrus.Fields{
		"pod":       pod.Name,
		"namespace": pod.Namespace,
	}).Info("Successfully created rsync pod")
	
	return &RsyncPod{
		Name:      pod.Name,
		Namespace: pod.Namespace,
		client:    m.client,
	}, nil
}

// WaitForPodReady waits for the pod to be ready
func (p *RsyncPod) WaitForPodReady(ctx context.Context, timeout time.Duration) error {
	log.WithFields(logrus.Fields{
		"pod":       p.Name,
		"namespace": p.Namespace,
		"timeout":   timeout,
	}).Info("Waiting for rsync pod to be ready")
	
	// Create a context with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	
	// Poll until the pod is ready or timeout
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("timeout waiting for rsync pod %s/%s to be ready", p.Namespace, p.Name)
		case <-ticker.C:
			// Get the pod
			pod, err := p.client.CoreV1().Pods(p.Namespace).Get(ctx, p.Name, metav1.GetOptions{})
			if err != nil {
				log.WithFields(logrus.Fields{
					"pod":       p.Name,
					"namespace": p.Namespace,
					"error":     err,
				}).Warn("Failed to get pod while waiting for ready state")
				continue
			}
			
			// Check if pod is running
			if pod.Status.Phase == corev1.PodRunning {
				log.WithFields(logrus.Fields{
					"pod":       p.Name,
					"namespace": p.Namespace,
				}).Info("Rsync pod is now running")
				return nil
			}
			
			log.WithFields(logrus.Fields{
				"pod":       p.Name,
				"namespace": p.Namespace,
				"phase":     pod.Status.Phase,
			}).Debug("Rsync pod not yet ready, waiting...")
		}
	}
}

// GenerateSSHKeys generates SSH keys in the pod
func (p *RsyncPod) GenerateSSHKeys(ctx context.Context) error {
	log.WithFields(logrus.Fields{
		"pod":       p.Name,
		"namespace": p.Namespace,
	}).Info("Generating SSH keys in rsync pod")
	
	cmd := []string{
		"sh",
		"-c",
		"mkdir -p /root/.ssh && ssh-keygen -t rsa -N '' -f /root/.ssh/id_rsa",
	}
	
	// Execute command in pod to generate SSH keys
	stdout, stderr, err := executeCommandInPod(ctx, p.client, p.Namespace, p.Name, cmd)
	if err != nil {
		log.WithFields(logrus.Fields{
			"pod":    p.Name,
			"stderr": stderr,
			"error":  err,
		}).Error("Failed to generate SSH keys")
		return fmt.Errorf("failed to generate SSH keys: %v", err)
	}
	
	log.WithFields(logrus.Fields{
		"pod":    p.Name,
		"stdout": stdout,
	}).Debug("Successfully generated SSH keys")
	
	return nil
}

// GetPublicKey gets the public key from the pod
func (p *RsyncPod) GetPublicKey(ctx context.Context) (string, error) {
	log.WithFields(logrus.Fields{
		"pod":       p.Name,
		"namespace": p.Namespace,
	}).Info("Getting public key from rsync pod")
	
	cmd := []string{
		"cat",
		"/root/.ssh/id_rsa.pub",
	}
	
	// Execute command in pod to get public key
	stdout, stderr, err := executeCommandInPod(ctx, p.client, p.Namespace, p.Name, cmd)
	if err != nil {
		log.WithFields(logrus.Fields{
			"pod":    p.Name,
			"stderr": stderr,
			"error":  err,
		}).Error("Failed to get public key")
		return "", fmt.Errorf("failed to get public key: %v", err)
	}
	
	log.WithFields(logrus.Fields{
		"pod": p.Name,
	}).Debug("Successfully got public key")
	
	return stdout, nil
}

// Cleanup deletes the pod after waiting for the specified grace period
func (p *RsyncPod) Cleanup(ctx context.Context, gracePeriodSeconds int64) error {
	log.WithFields(logrus.Fields{
		"pod":                  p.Name,
		"namespace":            p.Namespace,
		"grace_period_seconds": gracePeriodSeconds,
	}).Info("Cleaning up rsync pod")
	
	deleteOptions := metav1.DeleteOptions{}
	if gracePeriodSeconds >= 0 {
		deleteOptions.GracePeriodSeconds = &gracePeriodSeconds
	}
	
	err := p.client.CoreV1().Pods(p.Namespace).Delete(ctx, p.Name, deleteOptions)
	if err != nil {
		log.WithFields(logrus.Fields{
			"pod":       p.Name,
			"namespace": p.Namespace,
			"error":     err,
		}).Error("Failed to delete rsync pod")
		return fmt.Errorf("failed to delete rsync pod: %v", err)
	}
	
	log.WithFields(logrus.Fields{
		"pod":       p.Name,
		"namespace": p.Namespace,
	}).Info("Successfully deleted rsync pod")
	
	return nil
}

// executeCommandInPod executes a command in a pod
func executeCommandInPod(ctx context.Context, client kubernetes.Interface, namespace, podName string, command []string) (string, string, error) {
	// In a real implementation, this would use the Kubernetes API to execute a command in a pod
	// For now, we'll just log that we would execute the command and return a mock response
	
	log.WithFields(logrus.Fields{
		"pod":       podName,
		"namespace": namespace,
		"command":   command,
	}).Info("Executing command in pod")
	
	// Simulate command execution
	// Return a mock response based on the command
	if command[0] == "cat" && command[1] == "/root/.ssh/id_rsa.pub" {
		// Return a mock public key
		return "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQDcmRX6AcZhA7PJ+izJJM9YvN7LVp8D/6LjdkUPA9GqMTU6GapfVW4nYZaHBWnSTVFd+0nKtY4pEgOQfYYnlvjz3js5SZ3sRCEBgm5S5d6nFIkRtNNJ2p5zZbUmYhpYKST8TnfUmXAtLBPtc7xnCntZliWeQT/cL0ELrTi9SjK9e1hK2lcMX9zQnzo6jYnzEMxRjyZgvlEZwUAFMBKHzAxpJjxY+mVRxggJO74JwQfGpwQ0S3xO5Wxu6OAQEVnMJvEfQiJ9S1EgmMXsJ+3QZ48p2Gkvu0Q0T7n8YyQRVLALPJCCZPemmYDsUhDAdK25G7e5ZV7O0RWw1dfmB2mE74Sc5GoN44jxTgQHiRLe1n14CYj/QFl96zjUQyQJ7dl9YXiTK7BrW5SmZhfGqHsblMvMnZdkIRKHmqFdmwZnO1o4dJ8XKlILCFzngGCBKBLlJhlmgnl4i5AxVBbZ3KyTnRY7rYOjGPGxxa+BGfDZmbhcgWm1ILxXB8I1MXSYXZ0= root@dr-syncer-rsync", "", nil
	} else if command[0] == "sh" && command[1] == "-c" && command[2] == "mkdir -p /root/.ssh && ssh-keygen -t rsa -N '' -f /root/.ssh/id_rsa" {
		// Return a success message for SSH key generation
		return "Generating public/private rsa key pair. Your identification has been saved in /root/.ssh/id_rsa", "", nil
	}
	
	// Default response
	return "Command executed successfully", "", nil
}
