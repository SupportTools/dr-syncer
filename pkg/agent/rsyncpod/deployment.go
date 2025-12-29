package rsyncpod

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/supporttools/dr-syncer/pkg/contextkeys"
	"github.com/supporttools/dr-syncer/pkg/logging"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/utils/pointer"
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

	// CachedKeySecretName is the name of a secret containing pre-provisioned SSH keys
	// If set, the pod will mount the private key from this secret instead of generating new keys
	// The secret is expected to have an "id_rsa" key containing the private key
	CachedKeySecretName string
}

// Manager manages rsync operations
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

// RsyncDeployment represents an rsync deployment
type RsyncDeployment struct {
	// Name is the name of the deployment
	Name string

	// Namespace is the namespace the deployment is in
	Namespace string

	// client is the Kubernetes client
	client kubernetes.Interface

	// PodName is the name of the generated pod
	PodName string

	// PVCName is the name of the PVC being synced
	PVCName string

	// SyncID is a unique identifier for this sync operation
	SyncID string

	// HasCachedKeys indicates whether the deployment has pre-provisioned SSH keys mounted
	HasCachedKeys bool
}

// CreateRsyncDeployment creates a new rsync deployment
func (m *Manager) CreateRsyncDeployment(ctx context.Context, opts RsyncPodOptions) (*RsyncDeployment, error) {
	// Sanitize PVC name for use in deployment name
	safePVCName := sanitizeNameForLabel(opts.PVCName)

	// Generate a unique hash for this sync operation
	syncHash := rand.String(8)

	// Generate a deployment name
	deploymentName := fmt.Sprintf("dr-syncer-%s-%s", safePVCName, syncHash)

	log.WithFields(logrus.Fields{
		"namespace":        opts.Namespace,
		"pvc_name":         opts.PVCName,
		"node_name":        opts.NodeName,
		"deployment_name":  deploymentName,
		"sync_id":          opts.SyncID,
		"replication_name": opts.ReplicationName,
	}).Info(logging.LogTagDetail + " Creating rsync deployment")

	// Create deployment spec
	replicas := int32(1)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: opts.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "dr-syncer-rsync",
				"app.kubernetes.io/instance":   opts.SyncID,
				"app.kubernetes.io/component":  string(opts.Type),
				"app.kubernetes.io/managed-by": "dr-syncer",
				"dr-syncer.io/sync-id":         opts.SyncID,
				"dr-syncer.io/replication":     opts.ReplicationName,
				"dr-syncer.io/pvc-name":        safePVCName,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":     "dr-syncer-rsync",
					"app.kubernetes.io/instance": opts.SyncID,
					"dr-syncer.io/pvc-name":      safePVCName,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/name":       "dr-syncer-rsync",
						"app.kubernetes.io/instance":   opts.SyncID,
						"app.kubernetes.io/component":  string(opts.Type),
						"app.kubernetes.io/managed-by": "dr-syncer",
						"dr-syncer.io/sync-id":         opts.SyncID,
						"dr-syncer.io/replication":     opts.ReplicationName,
						"dr-syncer.io/pvc-name":        safePVCName,
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
							VolumeMounts: func() []corev1.VolumeMount {
								mounts := []corev1.VolumeMount{
									{
										Name:      "data",
										MountPath: "/data",
									},
								}
								// Add cached SSH key mount if specified
								if opts.CachedKeySecretName != "" {
									mounts = append(mounts, corev1.VolumeMount{
										Name:      "ssh-keys",
										MountPath: "/root/.ssh",
										ReadOnly:  true,
									})
								}
								return mounts
							}(),
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("2"),
									corev1.ResourceMemory: resource.MustParse("2Gi"),
								},
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("512Mi"),
								},
							},
							SecurityContext: &corev1.SecurityContext{
								Privileged: pointer.Bool(false),
								Capabilities: &corev1.Capabilities{
									Add: []corev1.Capability{"SYS_RESOURCE"},
								},
								RunAsUser: pointer.Int64(0), // Run as root to preserve file ownership
							},
							Env: []corev1.EnvVar{
								{
									Name:  "RSYNC_MAX_THREADS",
									Value: "8",
								},
								{
									Name:  "RSYNC_IO_PRIORITY",
									Value: "4", // Higher I/O priority (0-7, 0 is highest)
								},
							},
						},
					},
					Volumes: func() []corev1.Volume {
						vols := []corev1.Volume{
							{
								Name: "data",
								VolumeSource: corev1.VolumeSource{
									PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: opts.PVCName,
									},
								},
							},
						}
						// Add cached SSH key secret volume if specified
						if opts.CachedKeySecretName != "" {
							defaultMode := int32(0600) // Secure permissions for private key
							vols = append(vols, corev1.Volume{
								Name: "ssh-keys",
								VolumeSource: corev1.VolumeSource{
									Secret: &corev1.SecretVolumeSource{
										SecretName:  opts.CachedKeySecretName,
										DefaultMode: &defaultMode,
										Items: []corev1.KeyToPath{
											{
												Key:  "id_rsa",
												Path: "id_rsa",
											},
										},
									},
								},
							})
						}
						return vols
					}(),
					RestartPolicy: corev1.RestartPolicyAlways,
				},
			},
		},
	}

	// Set node selector if node name is provided
	if opts.NodeName != "" {
		deployment.Spec.Template.Spec.NodeName = opts.NodeName
	}

	// Check if a deployment with this name already exists and delete it if found
	existingDeployment, err := m.client.AppsV1().Deployments(opts.Namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err == nil {
		// Deployment exists, delete it
		log.WithFields(logrus.Fields{
			"deployment": existingDeployment.Name,
			"namespace":  existingDeployment.Namespace,
		}).Info(logging.LogTagDetail + " Found existing deployment, deleting it")

		deletePolicy := metav1.DeletePropagationForeground
		deleteOptions := metav1.DeleteOptions{
			PropagationPolicy: &deletePolicy,
		}

		if err := m.client.AppsV1().Deployments(opts.Namespace).Delete(ctx, deploymentName, deleteOptions); err != nil {
			if !errors.IsNotFound(err) {
				return nil, fmt.Errorf("failed to delete existing deployment: %v", err)
			}
		}

		// Wait for deletion to complete
		if err := waitForDeploymentDeletion(ctx, m.client, opts.Namespace, deploymentName); err != nil {
			return nil, fmt.Errorf("timeout waiting for deployment deletion: %v", err)
		}
	} else if !errors.IsNotFound(err) {
		// Some error other than "not found"
		return nil, fmt.Errorf("failed to check for existing deployment: %v", err)
	}

	// Create the deployment
	createdDeployment, err := m.client.AppsV1().Deployments(opts.Namespace).Create(ctx, deployment, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create rsync deployment: %v", err)
	}

	log.WithFields(logrus.Fields{
		"deployment": createdDeployment.Name,
		"namespace":  createdDeployment.Namespace,
	}).Info(logging.LogTagDetail + " Successfully created rsync deployment")

	// Create the RsyncDeployment object
	rsyncDeployment := &RsyncDeployment{
		Name:          createdDeployment.Name,
		Namespace:     createdDeployment.Namespace,
		client:        m.client,
		PVCName:       opts.PVCName,
		SyncID:        opts.SyncID,
		HasCachedKeys: opts.CachedKeySecretName != "",
	}

	return rsyncDeployment, nil
}

// WaitForPodReady waits for the deployment's pod to be ready - new signature that's compatible with rsync controller
func (d *RsyncDeployment) WaitForPodReady(ctx context.Context, timeout time.Duration) error {
	log.WithFields(logrus.Fields{
		"deployment": d.Name,
		"namespace":  d.Namespace,
		"timeout":    timeout,
	}).Info(logging.LogTagDetail + " Waiting for rsync deployment to be ready")

	// Create a context with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Poll until a pod is ready or timeout
	var podName string
	err := wait.PollUntilContextCancel(timeoutCtx, 5*time.Second, true, func(ctx context.Context) (bool, error) {
		// Get the deployment
		deployment, err := d.client.AppsV1().Deployments(d.Namespace).Get(ctx, d.Name, metav1.GetOptions{})
		if err != nil {
			log.WithFields(logrus.Fields{
				"deployment": d.Name,
				"namespace":  d.Namespace,
				"error":      err,
			}).Warn(logging.LogTagWarn + " Failed to get deployment")
			return false, nil
		}

		// Check if deployment is available
		if deployment.Status.AvailableReplicas == 0 {
			log.WithFields(logrus.Fields{
				"deployment":         d.Name,
				"namespace":          d.Namespace,
				"available_replicas": deployment.Status.AvailableReplicas,
				"ready_replicas":     deployment.Status.ReadyReplicas,
			}).Debug(logging.LogTagDetail + " Deployment not yet ready")
			return false, nil
		}

		// Get pods for this deployment
		labelSelector := metav1.FormatLabelSelector(deployment.Spec.Selector)
		pods, err := d.client.CoreV1().Pods(d.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})

		if err != nil {
			log.WithFields(logrus.Fields{
				"deployment": d.Name,
				"namespace":  d.Namespace,
				"error":      err,
			}).Warn("[DR-SYNC-DETAIL] Failed to list pods for deployment")
			return false, nil
		}

		// Find a running pod
		for _, pod := range pods.Items {
			if pod.Status.Phase == corev1.PodRunning {
				podName = pod.Name
				log.WithFields(logrus.Fields{
					"deployment": d.Name,
					"namespace":  d.Namespace,
					"pod":        podName,
				}).Info("[DR-SYNC-DETAIL] Found running pod for deployment")
				return true, nil
			}
		}

		log.WithFields(logrus.Fields{
			"deployment": d.Name,
			"namespace":  d.Namespace,
		}).Info("[DR-SYNC-DETAIL] Deployment is available but no running pods found yet")
		return false, nil
	})

	if err != nil {
		return fmt.Errorf("timeout waiting for rsync deployment %s/%s to be ready: %v", d.Namespace, d.Name, err)
	}

	// Store the pod name
	d.PodName = podName

	log.WithFields(logrus.Fields{
		"deployment": d.Name,
		"namespace":  d.Namespace,
		"pod":        d.PodName,
	}).Info("[DR-SYNC-DETAIL] Rsync deployment is ready with running pod")

	return nil
}

// RetryableError represents an error that can be retried
type RetryableError struct {
	Err error
}

func (e *RetryableError) Error() string {
	return e.Err.Error()
}

// withRetry executes a function with retries
func withRetry(ctx context.Context, maxRetries int, backoff time.Duration, operation func() error) error {
	var err error

	for attempt := 0; attempt < maxRetries; attempt++ {
		err = operation()
		if err == nil {
			return nil
		}

		// Check if error is retryable
		if _, ok := err.(*RetryableError); !ok {
			return err // Non-retryable error, return immediately
		}

		// Log retry attempt
		log.WithFields(logrus.Fields{
			"attempt":     attempt + 1,
			"max_retries": maxRetries,
			"error":       err,
		}).Info("[DR-SYNC-RETRY] Operation failed, retrying...")

		// Wait before retrying with exponential backoff
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff * time.Duration(1<<attempt)):
			// Continue to next attempt
		}
	}

	return fmt.Errorf("operation failed after %d attempts: %v", maxRetries, err)
}

// Enhanced loggers that ensure logs go to both log files and stdout/stderr
type OutputCapture struct {
	// The original buffer
	buffer *bytes.Buffer
	// The kind of output (stdout/stderr)
	kind string
	// Command info for logging
	podName   string
	namespace string
	command   string
}

func (o *OutputCapture) Write(p []byte) (n int, err error) {
	// First, write to the buffer
	n, err = o.buffer.Write(p)
	if err != nil {
		return n, err
	}

	// Then log to the logger with a prefix
	output := string(p)

	// Use different log levels for stdout vs stderr
	fields := logrus.Fields{
		"pod":       o.podName,
		"namespace": o.namespace,
		"command":   o.command,
		"output":    output,
		"type":      o.kind,
	}

	if o.kind == "stdout" {
		log.WithFields(fields).Info(fmt.Sprintf("[REMOTE-EXEC-OUT] %s", strings.TrimSpace(output)))
	} else {
		log.WithFields(fields).Warn(fmt.Sprintf("[REMOTE-EXEC-ERR] %s", strings.TrimSpace(output)))
	}

	return n, nil
}

// ExecuteCommandInPod executes a command in a pod using the Kubernetes API
// This is exported so it can be used by other packages
func ExecuteCommandInPod(ctx context.Context, client kubernetes.Interface, namespace, podName string, command []string, explicitConfig ...*rest.Config) (string, string, error) {
	if client == nil {
		return "", "", fmt.Errorf("kubernetes client is nil")
	}

	commandStr := strings.Join(command, " ")
	commandId := fmt.Sprintf("cmd-%s", rand.String(6))

	log.WithFields(logrus.Fields{
		"pod":        podName,
		"namespace":  namespace,
		"command":    commandStr,
		"command_id": commandId,
		"timestamp":  time.Now().Format(time.RFC3339),
	}).Info("[DR-SYNC-EXEC] Executing command in pod")

	// Set up the ExecOptions for the command
	execOpts := &corev1.PodExecOptions{
		Command: command,
		Stdin:   false,
		Stdout:  true,
		Stderr:  true,
		TTY:     false,
	}

	// Create the URL for the exec request
	req := client.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(execOpts, scheme.ParameterCodec)

	// We need to get the REST config
	var config *rest.Config

	// Debug log all context values being checked
	log.WithFields(logrus.Fields{
		"command_id": commandId,
		"pod":        podName,
		"namespace":  namespace,
	}).Info("[DR-SYNC-DEBUG] Checking context for config keys")

	//Dump all context values
	config, ok := ctx.Value(contextkeys.ConfigKey).(*rest.Config)
	if !ok {
		log.Warn("[DR-SYNC-DEBUG] Kubernetes REST config not found in context")
	} else {
		log.Debug("[DR-SYNC-DEBUG] Successfully retrieved Kubernetes REST config from context")
	}

	// First priority: explicit config in context
	if configFromCtx := ctx.Value(contextkeys.K8sConfigKey); configFromCtx != nil {
		config = configFromCtx.(*rest.Config)
		log.WithFields(logrus.Fields{
			"host":       config.Host,
			"command_id": commandId,
			"key_type":   fmt.Sprintf("%T", contextkeys.K8sConfigKey),
			"key_value":  string(contextkeys.K8sConfigKey),
		}).Info("[DR-SYNC-INFO] Using explicit config from context with contextkeys.K8sConfigKey")
	}

	// Also try with the internal package keys as fallback
	if config == nil {
		for _, key := range []interface{}{
			"k8s-config",             // string literal
			contextkeys.K8sConfigKey, // shared key
		} {
			if configFromCtx := ctx.Value(key); configFromCtx != nil {
				config = configFromCtx.(*rest.Config)
				log.WithFields(logrus.Fields{
					"host":       config.Host,
					"command_id": commandId,
					"key_type":   fmt.Sprintf("%T", key),
					"key_used":   fmt.Sprintf("%v", key),
				}).Info("[DR-SYNC-INFO] Found config with alternate key type")
				break
			}
		}
	}

	// No explicit config provided - check for PVCSyncer in context
	if config == nil {
		// Try all possible syncer keys
		var syncerValue interface{}
		var keyUsed string
		var keyType string

		// Try with different key types
		possibleKeys := []interface{}{
			contextkeys.SyncerKey,          // shared key (type contextkeys.ContextKey)
			contextkeys.SyncerKey.String(), // as string
			"pvcsync",                      // string literal
		}

		for _, key := range possibleKeys {
			if value := ctx.Value(key); value != nil {
				syncerValue = value
				keyUsed = fmt.Sprintf("%v", key)
				keyType = fmt.Sprintf("%T", key)
				log.WithFields(logrus.Fields{
					"key_type":   keyType,
					"key_used":   keyUsed,
					"value_type": fmt.Sprintf("%T", value),
					"command_id": commandId,
				}).Info("[DR-SYNC-DEBUG] Found PVCSyncer in context with key")
				break
			}
		}

		if syncerValue == nil {
			log.WithFields(logrus.Fields{
				"command_id": commandId,
				"keys_tried": fmt.Sprintf("%v", possibleKeys),
			}).Warn("[DR-SYNC-DEBUG] No PVCSyncer found in context with any key")
		} else {
			// Get config from PVCSyncer context
			type ConfigProvider interface {
				GetSourceConfig() *rest.Config
				GetDestinationConfig() *rest.Config
				GetSourceClient() kubernetes.Interface
				GetDestinationClient() kubernetes.Interface
			}

			if provider, ok := syncerValue.(ConfigProvider); ok {
				// Compare the client with source/destination clients to determine which to use
				srcClient := provider.GetSourceClient()
				destClient := provider.GetDestinationClient()

				// Compare client URLs to determine if we're using source or destination client
				clientHost := client.CoreV1().RESTClient().Get().URL().Host
				srcHost := srcClient.CoreV1().RESTClient().Get().URL().Host
				destHost := destClient.CoreV1().RESTClient().Get().URL().Host

				if clientHost == srcHost {
					config = provider.GetSourceConfig()
					log.WithFields(logrus.Fields{
						"host":        config.Host,
						"client_host": clientHost,
						"command_id":  commandId,
					}).Info("[DR-SYNC-INFO] Using source config from PVCSyncer (matched client)")
				} else if clientHost == destHost {
					config = provider.GetDestinationConfig()
					log.WithFields(logrus.Fields{
						"host":        config.Host,
						"client_host": clientHost,
						"command_id":  commandId,
					}).Info("[DR-SYNC-INFO] Using destination config from PVCSyncer (matched client)")
				} else {
					// If no direct match, use simple heuristic - dest for rsync operations
					if provider.GetDestinationConfig() != nil {
						config = provider.GetDestinationConfig()
						log.WithFields(logrus.Fields{
							"host":        config.Host,
							"client_host": clientHost,
							"src_host":    srcHost,
							"dest_host":   destHost,
							"command_id":  commandId,
						}).Info("[DR-SYNC-INFO] Using destination config from PVCSyncer (no direct match)")
					} else if provider.GetSourceConfig() != nil {
						config = provider.GetSourceConfig()
						log.WithFields(logrus.Fields{
							"host":        config.Host,
							"client_host": clientHost,
							"src_host":    srcHost,
							"dest_host":   destHost,
							"command_id":  commandId,
						}).Info("[DR-SYNC-INFO] Using source config from PVCSyncer (no direct match)")
					}
				}
			}
		}
	}

	// Check if explicit config was provided
	if len(explicitConfig) > 0 && explicitConfig[0] != nil {
		config = explicitConfig[0]
		log.WithFields(logrus.Fields{
			"host":       config.Host,
			"command_id": commandId,
		}).Info("[DR-SYNC-INFO] Using explicit config parameter")
	}

	// If no config is available, return an error
	if config == nil {
		return "", "", fmt.Errorf("no REST config found in context or provided explicitly for client %s", client.CoreV1().RESTClient().Get().URL().Host)
	}

	// Log the URL
	log.WithFields(logrus.Fields{
		"url":        req.URL().String(),
		"command_id": commandId,
	}).Debug("[DR-SYNC-EXEC] Preparing execution URL")

	// Create a SPDY executor
	executor, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		log.WithFields(logrus.Fields{
			"error":      err,
			"command_id": commandId,
		}).Error("[DR-SYNC-ERROR] Failed to create SPDY executor")
		return "", "", &RetryableError{Err: fmt.Errorf("failed to create SPDY executor: %v", err)}
	}

	// Create buffers for stdout and stderr with enhanced logging capability
	var stdoutBuffer, stderrBuffer bytes.Buffer
	stdout := &OutputCapture{
		buffer:    &stdoutBuffer,
		kind:      "stdout",
		podName:   podName,
		namespace: namespace,
		command:   commandStr,
	}
	stderr := &OutputCapture{
		buffer:    &stderrBuffer,
		kind:      "stderr",
		podName:   podName,
		namespace: namespace,
		command:   commandStr,
	}

	// Execute the command
	log.WithFields(logrus.Fields{
		"command_id": commandId,
		"timestamp":  time.Now().Format(time.RFC3339),
	}).Info("[DR-SYNC-EXEC] Starting command execution...")

	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: stdout,
		Stderr: stderr,
	})

	// Generate execution summary regardless of whether the command succeeded
	summary := fmt.Sprintf("Command execution summary (ID: %s):\n"+
		"Pod: %s/%s\n"+
		"Command: %s\n"+
		"Exit Code: %v\n"+
		"Stdout Size: %d bytes\n"+
		"Stderr Size: %d bytes\n"+
		"Execution Time: %s",
		commandId, namespace, podName, commandStr,
		err != nil, stdoutBuffer.Len(), stderrBuffer.Len(),
		time.Now().Format(time.RFC3339))

	log.Info("[DR-SYNC-EXEC-SUMMARY] " + summary)

	// Check for errors
	if err != nil {
		// Determine if the error is retryable
		if strings.Contains(err.Error(), "connection refused") ||
			strings.Contains(err.Error(), "connection reset") ||
			strings.Contains(err.Error(), "broken pipe") {
			return stdoutBuffer.String(), stderrBuffer.String(), &RetryableError{Err: fmt.Errorf("transient error: %v", err)}
		}

		log.WithFields(logrus.Fields{
			"error":      err,
			"stderr":     stderrBuffer.String(),
			"command_id": commandId,
			"timestamp":  time.Now().Format(time.RFC3339),
		}).Error("[DR-SYNC-ERROR] Failed to execute command")
		return stdoutBuffer.String(), stderrBuffer.String(), fmt.Errorf("failed to execute command: %v, stderr: %s", err, stderrBuffer.String())
	}

	// Log completion
	log.WithFields(logrus.Fields{
		"pod":        podName,
		"namespace":  namespace,
		"command":    commandStr,
		"stdout_len": stdoutBuffer.Len(),
		"stderr_len": stderrBuffer.Len(),
		"command_id": commandId,
		"timestamp":  time.Now().Format(time.RFC3339),
	}).Info("[DR-SYNC-EXEC] Command execution completed successfully")

	// If there's content in stderr but no error was returned, log it as a warning
	if stderrBuffer.Len() > 0 && err == nil {
		log.WithFields(logrus.Fields{
			"pod":        podName,
			"namespace":  namespace,
			"command":    commandStr,
			"stderr":     stderrBuffer.String(),
			"command_id": commandId,
		}).Warn("[DR-SYNC-EXEC] Command produced stderr output but no error")
	}

	return stdoutBuffer.String(), stderrBuffer.String(), nil
}

// GenerateSSHKeys generates SSH keys in the deployment's pod
// If the deployment has cached keys mounted (HasCachedKeys=true), this is a no-op
func (d *RsyncDeployment) GenerateSSHKeys(ctx context.Context, explicitConfig ...*rest.Config) error {
	if d.PodName == "" {
		return fmt.Errorf("no pod found for deployment, ensure WaitForPodReady was called")
	}

	// Skip key generation if cached keys are mounted
	if d.HasCachedKeys {
		log.WithFields(logrus.Fields{
			"deployment": d.Name,
			"namespace":  d.Namespace,
			"pod":        d.PodName,
		}).Info("[DR-SYNC-DETAIL] Skipping SSH key generation - using pre-provisioned cached keys")
		return nil
	}

	log.WithFields(logrus.Fields{
		"deployment": d.Name,
		"namespace":  d.Namespace,
		"pod":        d.PodName,
	}).Info("[DR-SYNC-DETAIL] Generating SSH keys in rsync pod")

	cmd := []string{
		"sh",
		"-c",
		"mkdir -p /root/.ssh && ssh-keygen -t rsa -N '' -f /root/.ssh/id_rsa",
	}

	// Execute command in pod to generate SSH keys
	stdout, stderr, err := ExecuteCommandInPod(ctx, d.client, d.Namespace, d.PodName, cmd, explicitConfig...)
	if err != nil {
		log.WithFields(logrus.Fields{
			"pod":    d.PodName,
			"stderr": stderr,
			"error":  err,
		}).Error("[DR-SYNC-ERROR] Failed to generate SSH keys")
		return fmt.Errorf("failed to generate SSH keys: %v", err)
	}

	log.WithFields(logrus.Fields{
		"pod":    d.PodName,
		"stdout": stdout,
	}).Debug("[DR-SYNC-DETAIL] Successfully generated SSH keys")

	return nil
}

// GetPublicKey gets the public key from the deployment's pod
func (d *RsyncDeployment) GetPublicKey(ctx context.Context, explicitConfig ...*rest.Config) (string, error) {
	if d.PodName == "" {
		return "", fmt.Errorf("no pod found for deployment, ensure WaitForPodReady was called")
	}

	log.WithFields(logrus.Fields{
		"deployment": d.Name,
		"namespace":  d.Namespace,
		"pod":        d.PodName,
	}).Info("[DR-SYNC-DETAIL] Getting public key from rsync pod")

	cmd := []string{
		"cat",
		"/root/.ssh/id_rsa.pub",
	}

	// Execute command in pod to get public key
	stdout, stderr, err := ExecuteCommandInPod(ctx, d.client, d.Namespace, d.PodName, cmd, explicitConfig...)
	if err != nil {
		log.WithFields(logrus.Fields{
			"pod":    d.PodName,
			"stderr": stderr,
			"error":  err,
		}).Error("[DR-SYNC-ERROR] Failed to get public key")
		return "", fmt.Errorf("failed to get public key: %v", err)
	}

	log.WithFields(logrus.Fields{
		"pod": d.PodName,
	}).Debug("[DR-SYNC-DETAIL] Successfully got public key")

	return stdout, nil
}

// Cleanup deletes the deployment - new signature with no grace period
func (d *RsyncDeployment) Cleanup(ctx context.Context) error {
	log.WithFields(logrus.Fields{
		"deployment": d.Name,
		"namespace":  d.Namespace,
	}).Info("[DR-SYNC] Cleaning up rsync deployment")

	// Set foreground deletion to ensure pods are deleted first
	deletePolicy := metav1.DeletePropagationForeground
	deleteOptions := metav1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	}

	err := d.client.AppsV1().Deployments(d.Namespace).Delete(ctx, d.Name, deleteOptions)
	if err != nil {
		if !errors.IsNotFound(err) {
			log.WithFields(logrus.Fields{
				"deployment": d.Name,
				"namespace":  d.Namespace,
				"error":      err,
			}).Error("[DR-SYNC-ERROR] Failed to delete rsync deployment")
			return fmt.Errorf("failed to delete rsync deployment: %v", err)
		}
		// Deployment not found, which is fine
		log.WithFields(logrus.Fields{
			"deployment": d.Name,
			"namespace":  d.Namespace,
		}).Info("[DR-SYNC-DETAIL] Deployment not found, already deleted")
		return nil
	}

	// Wait for deletion to complete
	if err := waitForDeploymentDeletion(ctx, d.client, d.Namespace, d.Name); err != nil {
		log.WithFields(logrus.Fields{
			"deployment": d.Name,
			"namespace":  d.Namespace,
			"error":      err,
		}).Warn("[DR-SYNC-DETAIL] Timeout waiting for deployment deletion, continuing anyway")
		// We'll continue anyway since we've initiated deletion
	}

	log.WithFields(logrus.Fields{
		"deployment": d.Name,
		"namespace":  d.Namespace,
	}).Info("[DR-SYNC-DETAIL] Successfully deleted rsync deployment")

	return nil
}

// CleanupExistingDeployments cleans up existing rsync deployments for a PVC
func (m *Manager) CleanupExistingDeployments(ctx context.Context, namespace, pvcName string) error {
	safePVCName := sanitizeNameForLabel(pvcName)
	labelSelector := fmt.Sprintf("app.kubernetes.io/name=dr-syncer-rsync,dr-syncer.io/pvc-name=%s", safePVCName)

	log.WithFields(logrus.Fields{
		"namespace":      namespace,
		"pvc_name":       pvcName,
		"label_selector": labelSelector,
	}).Info("[DR-SYNC-DETAIL] Cleaning up existing rsync deployments for PVC")

	// List deployments with matching labels
	deployments, err := m.client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})

	if err != nil {
		return fmt.Errorf("failed to list existing deployments: %v", err)
	}

	if len(deployments.Items) == 0 {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
		}).Info("[DR-SYNC-DETAIL] No existing deployments found for PVC")
		return nil
	}

	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc_name":  pvcName,
		"count":     len(deployments.Items),
	}).Info("[DR-SYNC-DETAIL] Found existing deployments to clean up")

	// Set deletion options with foreground propagation
	deletionPropagation := metav1.DeletePropagationForeground
	deleteOptions := metav1.DeleteOptions{
		PropagationPolicy: &deletionPropagation,
	}

	// Delete each deployment
	for _, deployment := range deployments.Items {
		log.WithFields(logrus.Fields{
			"deployment": deployment.Name,
			"namespace":  deployment.Namespace,
		}).Info("[DR-SYNC-DETAIL] Deleting existing deployment")

		if err := m.client.AppsV1().Deployments(namespace).Delete(ctx, deployment.Name, deleteOptions); err != nil {
			if !errors.IsNotFound(err) {
				log.WithFields(logrus.Fields{
					"deployment": deployment.Name,
					"namespace":  deployment.Namespace,
					"error":      err,
				}).Warn("[DR-SYNC-DETAIL] Failed to delete deployment, continuing with others")
				// Continue with other deployments
			}
		}

		// Wait for deletion
		if err := waitForDeploymentDeletion(ctx, m.client, namespace, deployment.Name); err != nil {
			log.WithFields(logrus.Fields{
				"deployment": deployment.Name,
				"namespace":  deployment.Namespace,
				"error":      err,
			}).Warn("[DR-SYNC-DETAIL] Timeout waiting for deployment deletion, continuing anyway")
			// Continue with other deployments
		}
	}

	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc_name":  pvcName,
	}).Info("[DR-SYNC-DETAIL] Finished cleaning up existing deployments for PVC")

	return nil
}

// waitForDeploymentDeletion waits for a deployment to be deleted
func waitForDeploymentDeletion(ctx context.Context, client kubernetes.Interface, namespace, name string) error {
	// Create a timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	log.WithFields(logrus.Fields{
		"deployment": name,
		"namespace":  namespace,
	}).Debug("[DR-SYNC-DETAIL] Waiting for deployment deletion")

	// Poll until the deployment is gone
	return wait.PollUntilContextCancel(timeoutCtx, 2*time.Second, true, func(ctx context.Context) (bool, error) {
		_, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			// Deployment is gone
			return true, nil
		}
		if err != nil {
			// Some other error
			log.WithFields(logrus.Fields{
				"deployment": name,
				"namespace":  namespace,
				"error":      err,
			}).Warn("[DR-SYNC-DETAIL] Error checking deployment existence, will retry")
		}
		// Deployment still exists
		return false, nil
	})
}

// sanitizeNameForLabel ensures a name is valid for use in a Kubernetes label
func sanitizeNameForLabel(name string) string {
	// Replace characters that aren't allowed in labels
	result := name
	invalidChars := []rune{'/', '.', ':', '~'}
	for _, char := range invalidChars {
		for i := 0; i < len(result); i++ {
			if rune(result[i]) == char {
				if i == 0 {
					result = "-" + result[1:]
				} else if i == len(result)-1 {
					result = result[:i] + "-"
				} else {
					result = result[:i] + "-" + result[i+1:]
				}
			}
		}
	}

	// Trim to 63 characters max (Kubernetes label value limit)
	if len(result) > 63 {
		result = result[:63]
	}

	return result
}
