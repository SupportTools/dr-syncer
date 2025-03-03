package tempod

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	// DefaultRsyncPort is the default port for the rsync server
	DefaultRsyncPort = 8873

	// DefaultPodNamePrefix is the prefix for temporary pod names
	DefaultPodNamePrefix = "dr-syncer-temp-"

	// DefaultPodCleanupTimeout is the default timeout for pod cleanup
	DefaultPodCleanupTimeout = 5 * time.Minute

	// DefaultPodReadyTimeout is the default timeout for pod readiness
	DefaultPodReadyTimeout = 2 * time.Minute
)

// TempPod represents a temporary pod for PVC access
type TempPod struct {
	// Name is the name of the pod
	Name string

	// Namespace is the namespace of the pod
	Namespace string

	// PVCName is the name of the PVC to mount
	PVCName string

	// NodeName is the name of the node to schedule the pod on
	NodeName string

	// RsyncPort is the port for the rsync server
	RsyncPort int

	// Pod is the Kubernetes pod object
	Pod *corev1.Pod

	// Client is the Kubernetes client
	Client kubernetes.Interface
}

// TempPodOptions contains options for creating a temporary pod
type TempPodOptions struct {
	// Name is the name of the pod (optional, will be generated if not provided)
	Name string

	// Namespace is the namespace of the pod
	Namespace string

	// PVCName is the name of the PVC to mount
	PVCName string

	// NodeName is the name of the node to schedule the pod on
	NodeName string

	// RsyncPort is the port for the rsync server (optional, defaults to DefaultRsyncPort)
	RsyncPort int

	// Labels are additional labels to add to the pod
	Labels map[string]string

	// Annotations are additional annotations to add to the pod
	Annotations map[string]string

	// KeySecretName is the name of the secret containing the SSH keys
	KeySecretName string
}

// Manager manages temporary pods
type Manager struct {
	// Client is the Kubernetes client
	Client kubernetes.Interface

	// Config is the Kubernetes client config
	Config *rest.Config

	// Pods is a map of pod names to TempPod objects
	Pods map[string]*TempPod
}

// NewManager creates a new temporary pod manager
func NewManager(config *rest.Config) (*Manager, error) {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %v", err)
	}

	return &Manager{
		Client: clientset,
		Config: config,
		Pods:   make(map[string]*TempPod),
	}, nil
}

// CreateTempPod creates a new temporary pod for PVC access
func (m *Manager) CreateTempPod(ctx context.Context, opts TempPodOptions) (*TempPod, error) {
	// Generate pod name if not provided
	if opts.Name == "" {
		opts.Name = fmt.Sprintf("%s%s", DefaultPodNamePrefix, randomString(8))
	}

	// Set default rsync port if not provided
	if opts.RsyncPort <= 0 {
		opts.RsyncPort = DefaultRsyncPort
	}

	log.WithFields(map[string]interface{}{
		"name":      opts.Name,
		"namespace": opts.Namespace,
		"pvc":       opts.PVCName,
		"node":      opts.NodeName,
		"port":      opts.RsyncPort,
	}).Info("Creating temporary pod")

	// Create pod object
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      opts.Name,
			Namespace: opts.Namespace,
			Labels: map[string]string{
				"app":                   "dr-syncer",
				"component":             "temp-pod",
				"dr-syncer.io/temp-pod": "true",
				"dr-syncer.io/pvc":      opts.PVCName,
			},
			Annotations: map[string]string{
				"dr-syncer.io/created-at": time.Now().Format(time.RFC3339),
			},
		},
		Spec: corev1.PodSpec{
			NodeName: opts.NodeName,
			Containers: []corev1.Container{
				{
					Name:  "rsync",
					Image: "alpine:latest",
					Command: []string{
						"/bin/sh",
						"-c",
						fmt.Sprintf("apk add --no-cache rsync openssh && mkdir -p /data && mkdir -p /home/syncer/.ssh && chmod 700 /home/syncer/.ssh && cp /etc/ssh/keys/id_rsa /home/syncer/.ssh/ && cp /etc/ssh/keys/id_rsa.pub /home/syncer/.ssh/ && chmod 600 /home/syncer/.ssh/id_rsa && chmod 644 /home/syncer/.ssh/id_rsa.pub && rsync --daemon --no-detach --port=%d --config=/etc/rsyncd.conf", opts.RsyncPort),
					},
					Ports: []corev1.ContainerPort{
						{
							Name:          "rsync",
							ContainerPort: int32(opts.RsyncPort),
							Protocol:      corev1.ProtocolTCP,
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "pvc-data",
							MountPath: "/data",
						},
						{
							Name:      "rsync-config",
							MountPath: "/etc/rsyncd.conf",
							SubPath:   "rsyncd.conf",
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "pvc-data",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: opts.PVCName,
						},
					},
				},
				{
					Name: "rsync-config",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "dr-syncer-rsync-config",
							},
							Items: []corev1.KeyToPath{
								{
									Key:  "rsyncd.conf",
									Path: "rsyncd.conf",
								},
							},
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	// Add SSH key secret volume if provided
	if opts.KeySecretName != "" {
		log.WithFields(map[string]interface{}{
			"name":       opts.Name,
			"namespace":  opts.Namespace,
			"key_secret": opts.KeySecretName,
		}).Info("Adding SSH key secret to temporary pod")

		// Add volume for SSH keys
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
			Name: "ssh-keys",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: opts.KeySecretName,
					Optional:   &[]bool{false}[0],
				},
			},
		})

		// Add volume mount for SSH keys
		pod.Spec.Containers[0].VolumeMounts = append(pod.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
			Name:      "ssh-keys",
			MountPath: "/etc/ssh/keys",
			ReadOnly:  true,
		})
	}

	// Add additional labels if provided
	if opts.Labels != nil {
		for k, v := range opts.Labels {
			pod.Labels[k] = v
		}
	}

	// Add additional annotations if provided
	if opts.Annotations != nil {
		for k, v := range opts.Annotations {
			pod.Annotations[k] = v
		}
	}

	// Create the pod
	createdPod, err := m.Client.CoreV1().Pods(opts.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create pod: %v", err)
	}

	tempPod := &TempPod{
		Name:      opts.Name,
		Namespace: opts.Namespace,
		PVCName:   opts.PVCName,
		NodeName:  opts.NodeName,
		RsyncPort: opts.RsyncPort,
		Pod:       createdPod,
		Client:    m.Client,
	}

	// Add to pods map
	m.Pods[opts.Name] = tempPod

	log.WithFields(map[string]interface{}{
		"name":      opts.Name,
		"namespace": opts.Namespace,
	}).Info("Temporary pod created")

	return tempPod, nil
}

// WaitForPodReady waits for the pod to be ready
func (p *TempPod) WaitForPodReady(ctx context.Context, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = DefaultPodReadyTimeout
	}

	log.WithFields(map[string]interface{}{
		"name":      p.Name,
		"namespace": p.Namespace,
		"timeout":   timeout,
	}).Info("Waiting for pod to be ready")

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Poll for pod readiness
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("timeout waiting for pod to be ready")
		case <-ticker.C:
			pod, err := p.Client.CoreV1().Pods(p.Namespace).Get(ctx, p.Name, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("failed to get pod: %v", err)
			}

			p.Pod = pod

			// Check if pod is running
			if pod.Status.Phase == corev1.PodRunning {
				// Check if all containers are ready
				allReady := true
				for _, containerStatus := range pod.Status.ContainerStatuses {
					if !containerStatus.Ready {
						allReady = false
						break
					}
				}

				if allReady {
					log.WithFields(map[string]interface{}{
						"name":      p.Name,
						"namespace": p.Namespace,
					}).Info("Pod is ready")
					return nil
				}
			} else if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
				return fmt.Errorf("pod is in terminal state: %s", pod.Status.Phase)
			}
		}
	}
}

// GetRsyncEndpoint returns the rsync endpoint for the pod
func (p *TempPod) GetRsyncEndpoint() string {
	return fmt.Sprintf("%s:%d", p.Pod.Status.PodIP, p.RsyncPort)
}

// Cleanup deletes the temporary pod
func (p *TempPod) Cleanup(ctx context.Context, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = DefaultPodCleanupTimeout
	}

	log.WithFields(map[string]interface{}{
		"name":      p.Name,
		"namespace": p.Namespace,
		"timeout":   timeout,
	}).Info("Cleaning up temporary pod")

	// Delete the pod
	err := p.Client.CoreV1().Pods(p.Namespace).Delete(ctx, p.Name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete pod: %v", err)
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Wait for pod to be deleted
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("timeout waiting for pod to be deleted")
		case <-ticker.C:
			_, err := p.Client.CoreV1().Pods(p.Namespace).Get(ctx, p.Name, metav1.GetOptions{})
			if err != nil {
				// Pod is deleted
				log.WithFields(map[string]interface{}{
					"name":      p.Name,
					"namespace": p.Namespace,
				}).Info("Temporary pod deleted")
				return nil
			}
		}
	}
}

// CleanupAll deletes all temporary pods
func (m *Manager) CleanupAll(ctx context.Context, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = DefaultPodCleanupTimeout
	}

	log.Info("Cleaning up all temporary pods")

	for name, pod := range m.Pods {
		if err := pod.Cleanup(ctx, timeout); err != nil {
			log.WithFields(map[string]interface{}{
				"name":      pod.Name,
				"namespace": pod.Namespace,
				"error":     err,
			}).Error("Failed to cleanup pod")
		}
		delete(m.Pods, name)
	}

	return nil
}

// GetPod returns a temporary pod by name
func (m *Manager) GetPod(name string) *TempPod {
	return m.Pods[name]
}

// ListPods returns all temporary pods
func (m *Manager) ListPods() []*TempPod {
	pods := make([]*TempPod, 0, len(m.Pods))
	for _, pod := range m.Pods {
		pods = append(pods, pod)
	}
	return pods
}

// randomString generates a random string of the specified length
func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, length)
	for i := range result {
		result[i] = charset[time.Now().UnixNano()%int64(len(charset))]
		time.Sleep(1 * time.Nanosecond)
	}
	return string(result)
}
