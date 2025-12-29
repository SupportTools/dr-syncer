package replication

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
	"github.com/supporttools/dr-syncer/pkg/agent/rsyncpod"
)

// Import ReplicationMode constants
const (
	ScheduledMode  = drv1alpha1.ScheduledMode
	ContinuousMode = drv1alpha1.ContinuousMode
	ManualMode     = drv1alpha1.ManualMode
)

// init updates the log component field for PVC sync operations
func init() {
	// Update the existing logger with the PVC syncer component
	log = log.WithField("subcomponent", "pvc-syncer")
}

// NamespaceMappingPVCSyncStatus represents the status of a PVC sync operation for a namespace mapping
type NamespaceMappingPVCSyncStatus struct {
	// Phase is the current phase of the PVC sync operation
	Phase string `json:"phase,omitempty"`

	// Message is a human-readable message explaining the current phase
	Message string `json:"message,omitempty"`

	// LastSyncTime is the time of the last successful sync
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// NextSyncTime is the scheduled time for the next sync
	NextSyncTime *metav1.Time `json:"nextSyncTime,omitempty"`
}

// PVCSyncOptions contains options for PVC synchronization
type PVCSyncOptions struct {
	// SourcePVC is the source PVC to sync from
	SourcePVC *corev1.PersistentVolumeClaim

	// DestinationPVC is the destination PVC to sync to
	DestinationPVC *corev1.PersistentVolumeClaim

	// SourceNamespace is the namespace of the source PVC
	SourceNamespace string

	// DestinationNamespace is the namespace of the destination PVC
	DestinationNamespace string

	// SourceNode is the node where the source PVC is mounted
	SourceNode string

	// DestinationNode is the node where the destination PVC is mounted
	DestinationNode string

	// TempPodKeySecretName is the name of the secret containing the SSH keys for temporary pods
	TempPodKeySecretName string

	// RsyncOptions is a list of options to pass to rsync
	RsyncOptions []string
}

// PVCSyncer handles PVC synchronization
type PVCSyncer struct {
	// SourceClient is the client for the source cluster
	SourceClient client.Client

	// DestinationClient is the client for the destination cluster
	DestinationClient client.Client

	// SourceConfig is the config for the source cluster
	SourceConfig *rest.Config

	// DestinationConfig is the config for the destination cluster
	DestinationConfig *rest.Config

	// SourceK8sClient is the Kubernetes client for the source cluster
	SourceK8sClient kubernetes.Interface

	// DestinationK8sClient is the Kubernetes client for the destination cluster
	DestinationK8sClient kubernetes.Interface

	// SourceNamespace is the namespace in the source cluster
	SourceNamespace string

	// DestinationNamespace is the namespace in the destination cluster
	DestinationNamespace string

	// SourceEventRecorder records events on source PVCs for observability
	SourceEventRecorder record.EventRecorder

	// SourceRemoteClusterName is the name of the RemoteCluster for the source cluster
	// Used to look up cached SSH keys for rsync operations
	SourceRemoteClusterName string
}

// CreateEventRecorderForCluster creates an EventRecorder for emitting events to a Kubernetes cluster
func CreateEventRecorderForCluster(config *rest.Config, componentName string) (record.EventRecorder, error) {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset for event recorder: %w", err)
	}

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{
		Interface: clientset.CoreV1().Events(""),
	})

	return eventBroadcaster.NewRecorder(
		scheme.Scheme,
		corev1.EventSource{Component: componentName},
	), nil
}

// NewPVCSyncer creates a new PVC syncer
func NewPVCSyncer(sourceClient client.Client, destinationClient client.Client, sourceConfig, destinationConfig *rest.Config) (*PVCSyncer, error) {
	// Create Kubernetes clients
	sourceK8sClient, err := kubernetes.NewForConfig(sourceConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create source Kubernetes client: %v", err)
	}

	destinationK8sClient, err := kubernetes.NewForConfig(destinationConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create destination Kubernetes client: %v", err)
	}

	// Create event recorder for source cluster (events appear on source PVCs)
	var sourceEventRecorder record.EventRecorder
	if sourceConfig != nil {
		recorder, err := CreateEventRecorderForCluster(sourceConfig, "dr-syncer-pvc-sync")
		if err != nil {
			log.WithFields(logrus.Fields{
				"error": err,
			}).Warn("Failed to create event recorder for source cluster, events will not be emitted")
		} else {
			sourceEventRecorder = recorder
		}
	}

	return &PVCSyncer{
		SourceClient:         sourceClient,
		DestinationClient:    destinationClient,
		SourceConfig:         sourceConfig,
		DestinationConfig:    destinationConfig,
		SourceK8sClient:      sourceK8sClient,
		DestinationK8sClient: destinationK8sClient,
		SourceEventRecorder:  sourceEventRecorder,
		// Namespaces will be set when syncing PVCs
		SourceNamespace:      "",
		DestinationNamespace: "",
	}, nil
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
		}).Info("Operation failed, retrying...")

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

// withRetryConfig performs operation with retry using CRD configuration
// This uses the RetryConfig from NamespaceMapping to configure retries
func withRetryConfig(ctx context.Context, config *drv1alpha1.RetryConfig, operation func() error) error {
	// Use defaults if config is nil
	maxRetries := int32(5)
	initialBackoff := 5 * time.Second
	maxBackoff := 5 * time.Minute
	multiplier := float64(2.0)

	if config != nil {
		if config.MaxRetries != nil {
			maxRetries = *config.MaxRetries
		}
		if config.InitialBackoff != "" {
			if parsed, err := time.ParseDuration(config.InitialBackoff); err == nil {
				initialBackoff = parsed
			} else {
				log.WithFields(logrus.Fields{
					"value": config.InitialBackoff,
					"error": err,
				}).Warn("Failed to parse InitialBackoff, using default")
			}
		}
		if config.MaxBackoff != "" {
			if parsed, err := time.ParseDuration(config.MaxBackoff); err == nil {
				maxBackoff = parsed
			} else {
				log.WithFields(logrus.Fields{
					"value": config.MaxBackoff,
					"error": err,
				}).Warn("Failed to parse MaxBackoff, using default")
			}
		}
		if config.BackoffMultiplier != nil {
			multiplier = float64(*config.BackoffMultiplier) / 100.0
		}
	}

	var err error
	currentBackoff := initialBackoff

	for attempt := int32(0); attempt < maxRetries; attempt++ {
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
			"backoff":     currentBackoff.String(),
			"error":       err,
		}).Info("Operation failed, retrying with exponential backoff...")

		// Add jitter: ±15% randomization to prevent thundering herd
		jitter := 1.0 + (rand.Float64()*0.3 - 0.15)
		backoffWithJitter := time.Duration(float64(currentBackoff) * jitter)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoffWithJitter):
			// Continue to next attempt
		}

		// Calculate next backoff with multiplier, capped at maxBackoff
		nextBackoff := time.Duration(float64(currentBackoff) * multiplier)
		if nextBackoff > maxBackoff {
			nextBackoff = maxBackoff
		}
		currentBackoff = nextBackoff
	}

	return fmt.Errorf("operation failed after %d attempts: %v", maxRetries, err)
}

// executeCommandInPod executes a command in a pod
func executeCommandInPod(ctx context.Context, client kubernetes.Interface, namespace, podName string, command []string) (string, string, error) {
	if client == nil {
		return "", "", fmt.Errorf("kubernetes client is nil")
	}

	commandStr := strings.Join(command, " ")
	log.WithFields(logrus.Fields{
		"pod":       podName,
		"namespace": namespace,
		"command":   commandStr,
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

	// Get the config from context or use PVCSyncer configs
	var config *rest.Config

	// First priority: explicit config in context
	if configFromCtx := ctx.Value(k8sConfigKey); configFromCtx != nil {
		config = configFromCtx.(*rest.Config)
		log.WithFields(logrus.Fields{
			"host": config.Host,
		}).Info("[DR-SYNC-INFO] Using explicit config from context")
	}

	// No explicit config provided - check for PVCSyncer in context
	if config == nil {
		syncerValue := ctx.Value(syncerKey)
		if syncerValue != nil {
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
					}).Info("[DR-SYNC-INFO] Using source config from PVCSyncer (matched client)")
				} else if clientHost == destHost {
					config = provider.GetDestinationConfig()
					log.WithFields(logrus.Fields{
						"host":        config.Host,
						"client_host": clientHost,
					}).Info("[DR-SYNC-INFO] Using destination config from PVCSyncer (matched client)")
				} else {
					// If no direct match, use simple namespace-based heuristic
					usingNamespace := namespace
					// Trim any pod-specific suffixes (for agent finding)
					if strings.Contains(namespace, "/") {
						parts := strings.Split(namespace, "/")
						usingNamespace = parts[0]
					}

					// Check if this is a source namespace
					syncer, ok := syncerValue.(*PVCSyncer)
					if ok {
						// Compare using source or destination namespace based on prefix
						if syncer.SourceNamespace != "" && strings.HasPrefix(usingNamespace, syncer.SourceNamespace) {
							config = provider.GetSourceConfig()
							log.WithFields(logrus.Fields{
								"host":             config.Host,
								"source_namespace": syncer.SourceNamespace,
								"namespace":        namespace,
							}).Info("[DR-SYNC-INFO] Using source config from PVCSyncer (namespace match)")
						} else if syncer.DestinationNamespace != "" {
							config = provider.GetDestinationConfig()
							log.WithFields(logrus.Fields{
								"host":           config.Host,
								"dest_namespace": syncer.DestinationNamespace,
								"namespace":      namespace,
							}).Info("[DR-SYNC-INFO] Using destination config from PVCSyncer (fallback)")
						}
					}
				}
			}
		}
	}

	// If config is still nil after all checks, return an error
	if config == nil {
		clientHost := client.CoreV1().RESTClient().Get().URL().Host
		log.WithFields(logrus.Fields{
			"namespace":   namespace,
			"pod":         podName,
			"client_host": clientHost,
		}).Error("[DR-SYNC-ERROR] No REST config found in context for client")
		return "", "", fmt.Errorf("no REST config found in context for client %s", clientHost)
	}

	// Log the URL
	log.WithFields(logrus.Fields{
		"url": req.URL().String(),
	}).Debug("[DR-SYNC-EXEC] Preparing execution URL")

	// Create a SPDY executor
	executor, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		log.WithFields(logrus.Fields{
			"error": err,
		}).Error("[DR-SYNC-ERROR] Failed to create SPDY executor")
		return "", "", &RetryableError{Err: fmt.Errorf("failed to create SPDY executor: %v", err)}
	}

	// Create buffers for stdout and stderr
	var stdout, stderr bytes.Buffer

	// Execute the command
	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})

	// Check for errors
	if err != nil {
		// Determine if the error is retryable
		if strings.Contains(err.Error(), "connection refused") ||
			strings.Contains(err.Error(), "connection reset") ||
			strings.Contains(err.Error(), "broken pipe") {
			return stdout.String(), stderr.String(), &RetryableError{Err: fmt.Errorf("transient error: %v", err)}
		}

		log.WithFields(logrus.Fields{
			"error":  err,
			"stderr": stderr.String(),
		}).Error("[DR-SYNC-ERROR] Failed to execute command")
		return stdout.String(), stderr.String(), fmt.Errorf("failed to execute command: %v, stderr: %s", err, stderr.String())
	}

	// Log completion
	log.WithFields(logrus.Fields{
		"pod":        podName,
		"namespace":  namespace,
		"command":    commandStr,
		"stdout_len": stdout.Len(),
		"stderr_len": stderr.Len(),
	}).Debug("[DR-SYNC-EXEC] Command execution completed")

	if stderr.Len() > 0 {
		log.WithFields(logrus.Fields{
			"pod":       podName,
			"namespace": namespace,
			"command":   commandStr,
			"stderr":    stderr.String(),
		}).Warn("[DR-SYNC-EXEC] Command produced stderr output but no error")
	}

	return stdout.String(), stderr.String(), nil
}

// AddPublicKeyToSourceAgent adds a public key to the agent in the source cluster
func (p *PVCSyncer) AddPublicKeyToSourceAgent(ctx context.Context, publicKey, trackingInfo string) error {
	log.WithFields(logrus.Fields{
		"tracking_info": trackingInfo,
	}).Info("[DR-SYNC] Adding public key to agent in source cluster")

	// Find the agent pod in the source cluster
	podList, err := p.SourceK8sClient.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name=dr-syncer-agent",
	})
	if err != nil {
		return fmt.Errorf("failed to list agent pods in source cluster: %v", err)
	}

	if len(podList.Items) == 0 {
		return fmt.Errorf("no agent pods found in source cluster")
	}

	// Use the first agent pod found
	agentPod := podList.Items[0]
	log.WithFields(logrus.Fields{
		"pod":       agentPod.Name,
		"namespace": agentPod.Namespace,
	}).Info("[DR-SYNC] Found agent pod in source cluster")

	// Create command to add the public key to the agent's authorized_keys file
	cmd := []string{
		"sh",
		"-c",
		fmt.Sprintf("mkdir -p /root/.ssh && echo '%s %s' >> /root/.ssh/authorized_keys && chmod 600 /root/.ssh/authorized_keys",
			publicKey, trackingInfo),
	}

	// Put the PVCSyncer in the context for ExecuteCommandInPod
	pvcSyncCtx := context.WithValue(ctx, syncerKey, p)

	// Execute the command in agent pod with context that includes PVCSyncer
	log.WithFields(logrus.Fields{
		"agent_pod":          agentPod.Name,
		"namespace":          agentPod.Namespace,
		"source_config_host": p.SourceConfig.Host,
	}).Info("[DR-SYNC-DETAIL] Executing command with source config context")

	stdout, stderr, err := executeCommandInPod(pvcSyncCtx, p.SourceK8sClient, agentPod.Namespace, agentPod.Name, cmd)
	if err != nil {
		log.WithFields(logrus.Fields{
			"stderr": stderr,
			"error":  err,
		}).Error("[DR-SYNC-ERROR] Failed to add public key to agent pod")
		return fmt.Errorf("failed to add public key to agent pod: %v", err)
	}

	log.WithFields(logrus.Fields{
		"pod":       agentPod.Name,
		"namespace": agentPod.Namespace,
		"stdout":    stdout,
	}).Info("[DR-SYNC] Successfully added public key to agent pod")

	return nil
}

// CompleteNamespaceMappingPVCSync updates the namespace mapping status after a PVC sync operation
func (p *PVCSyncer) CompleteNamespaceMappingPVCSync(ctx context.Context, repl *drv1alpha1.NamespaceMapping, syncID string) error {
	mappingName := repl.Name

	log.WithFields(logrus.Fields{
		"namespacemapping": mappingName,
		"sync_id":          syncID,
	}).Info("[DR-SYNC] Updating namespace mapping status after PVC sync")

	// Get the latest version to update - Kubernetes requires a Get before Update
	var nm drv1alpha1.NamespaceMapping

	// The namespace mapping should be in a system namespace, which for our controller
	// is typically the same namespace where the operator is running
	operatorNamespace := "dr-syncer" // Using a default namespace for the operator

	if err := p.DestinationClient.Get(ctx, client.ObjectKey{
		Namespace: operatorNamespace,
		Name:      repl.Name,
	}, &nm); err != nil {
		// If we can't find by name in the operator namespace, try the destination namespace
		// as a fallback since the resource might be deployed there instead
		if p.DestinationNamespace != "" {
			if err := p.DestinationClient.Get(ctx, client.ObjectKey{
				Namespace: p.DestinationNamespace,
				Name:      repl.Name,
			}, &nm); err != nil {
				// If we still can't find it, try with no namespace as it might be a cluster-scoped resource
				if err := p.DestinationClient.Get(ctx, client.ObjectKey{Namespace: "", Name: repl.Name}, &nm); err != nil {
					// Finally, use the original object for the update as a last resort
					log.WithFields(logrus.Fields{
						"namespacemapping": repl.Name,
						"error":            err,
					}).Warn("[DR-SYNC] Could not find namespace mapping for update, will use provided object")
					nm = *repl
				}
			}
		}
	}

	// Update the status
	now := metav1.Now()

	// Initialize annotations if not present
	if nm.Annotations == nil {
		nm.Annotations = make(map[string]string)
	}

	// Update annotations with sync information
	nm.Annotations["dr-syncer.io/last-pvc-sync-time"] = now.Format(time.RFC3339)
	nm.Annotations["dr-syncer.io/last-pvc-sync-id"] = syncID
	nm.Annotations["dr-syncer.io/last-pvc-sync-status"] = "Completed"

	// Update the namespace mapping
	if err := p.DestinationClient.Update(ctx, &nm); err != nil {
		log.WithFields(logrus.Fields{
			"namespacemapping": repl.Name,
			"namespace":        nm.Namespace,
			"error":            err,
		}).Error("[DR-SYNC] Failed to update namespace mapping")
		return fmt.Errorf("failed to update namespace mapping status: %v", err)
	}

	log.WithFields(logrus.Fields{
		"namespacemapping": repl.Name,
		"sync_id":          syncID,
		"sync_time":        now.Format(time.RFC3339),
	}).Info("[DR-SYNC] Successfully updated namespace mapping status")

	return nil
}

// ScheduleNextPVCSync schedules the next PVC sync operation
func (p *PVCSyncer) ScheduleNextPVCSync(ctx context.Context, repl *drv1alpha1.NamespaceMapping) error {
	log.WithFields(logrus.Fields{
		"namespacemapping": repl.Name,
	}).Info("[DR-SYNC] Scheduling next PVC sync")

	// Get the latest version to update - Kubernetes requires a Get before Update
	var nm drv1alpha1.NamespaceMapping

	// The namespace mapping should be in a system namespace, which for our controller
	// is typically the same namespace where the operator is running
	operatorNamespace := "dr-syncer" // Using a default namespace for the operator

	if err := p.DestinationClient.Get(ctx, client.ObjectKey{
		Namespace: operatorNamespace,
		Name:      repl.Name,
	}, &nm); err != nil {
		// If we can't find by name in the operator namespace, try the destination namespace
		// as a fallback since the resource might be deployed there instead
		if p.DestinationNamespace != "" {
			if err := p.DestinationClient.Get(ctx, client.ObjectKey{
				Namespace: p.DestinationNamespace,
				Name:      repl.Name,
			}, &nm); err != nil {
				// If we still can't find it, try with no namespace as it might be a cluster-scoped resource
				if err := p.DestinationClient.Get(ctx, client.ObjectKey{Namespace: "", Name: repl.Name}, &nm); err != nil {
					// Finally, use the original object for the update as a last resort
					log.WithFields(logrus.Fields{
						"namespacemapping": repl.Name,
						"error":            err,
					}).Warn("[DR-SYNC] Could not find namespace mapping for update, will use provided object")
					nm = *repl
				}
			}
		}
	}

	// Calculate next sync time based on schedule
	var nextSyncTime metav1.Time

	// Default to 24 hours from now if no schedule is specified
	nextSyncTime = metav1.NewTime(time.Now().Add(24 * time.Hour))

	// If schedule is specified, parse and use it
	if repl.Spec.Schedule != "" {
		// Simple schedule parser (enhance as needed)
		// Format: "interval:value" e.g. "hours:6" or "minutes:30"
		parts := strings.Split(repl.Spec.Schedule, ":")
		if len(parts) == 2 {
			unit := parts[0]
			value, err := strconv.Atoi(parts[1])
			if err == nil {
				switch strings.ToLower(unit) {
				case "minutes":
					nextSyncTime = metav1.NewTime(time.Now().Add(time.Duration(value) * time.Minute))
				case "hours":
					nextSyncTime = metav1.NewTime(time.Now().Add(time.Duration(value) * time.Hour))
				case "days":
					nextSyncTime = metav1.NewTime(time.Now().Add(time.Duration(value) * 24 * time.Hour))
				}
			}
		}
	}

	// Initialize annotations if not present
	if nm.Annotations == nil {
		nm.Annotations = make(map[string]string)
	}

	// Update annotation with next sync time
	nm.Annotations["dr-syncer.io/next-pvc-sync-time"] = nextSyncTime.Format(time.RFC3339)

	// Update the namespace mapping
	if err := p.DestinationClient.Update(ctx, &nm); err != nil {
		log.WithFields(logrus.Fields{
			"namespacemapping": repl.Name,
			"namespace":        nm.Namespace,
			"error":            err,
		}).Error("[DR-SYNC] Failed to update namespace mapping")
		return fmt.Errorf("failed to update namespace mapping with next sync time: %v", err)
	}

	log.WithFields(logrus.Fields{
		"namespacemapping": repl.Name,
		"next_sync_time":   nextSyncTime.Format(time.RFC3339),
	}).Info("[DR-SYNC] Successfully scheduled next PVC sync")

	return nil
}

// WaitForPVCBound waits for a PVC to be bound with a timeout
func (p *PVCSyncer) WaitForPVCBound(ctx context.Context, namespace, pvcName string, timeout time.Duration) error {
	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc_name":  pvcName,
		"timeout":   timeout,
	}).Info("Waiting for PVC to be bound")

	// Create a context with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Determine which Kubernetes client to use
	var k8sClient kubernetes.Interface
	if namespace == p.SourceNamespace {
		k8sClient = p.SourceK8sClient
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
		}).Debug("Using source Kubernetes client")
	} else {
		k8sClient = p.DestinationK8sClient
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
		}).Debug("Using destination Kubernetes client")
	}

	// Poll until the PVC is bound or timeout
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("timeout waiting for PVC %s/%s to be bound", namespace, pvcName)
		case <-ticker.C:
			// Get the PVC
			pvc, err := k8sClient.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
			if err != nil {
				log.WithFields(logrus.Fields{
					"namespace": namespace,
					"pvc_name":  pvcName,
					"error":     err,
				}).Warn("Failed to get PVC while waiting for bound state")
				continue
			}

			// Check if PVC is bound
			if pvc.Status.Phase == corev1.ClaimBound {
				log.WithFields(logrus.Fields{
					"namespace": namespace,
					"pvc_name":  pvcName,
				}).Info("PVC is now bound")
				return nil
			}

			log.WithFields(logrus.Fields{
				"namespace": namespace,
				"pvc_name":  pvcName,
				"phase":     pvc.Status.Phase,
			}).Debug("PVC not yet bound, waiting...")
		}
	}
}

// RunSSHCommand runs an SSH command from the rsync pod to the agent pod
func (p *PVCSyncer) RunSSHCommand(ctx context.Context, rsyncDeployment *rsyncpod.RsyncDeployment, agentIP string, port int, command string) (string, error) {
	log.WithFields(logrus.Fields{
		"rsync_pod": rsyncDeployment.PodName,
		"agent_ip":  agentIP,
		"port":      port,
		"command":   command,
	}).Info("Running SSH command")

	// Construct SSH command
	sshCommand := fmt.Sprintf("ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i /root/.ssh/id_rsa -p %d root@%s '%s'", port, agentIP, command)
	cmd := []string{"sh", "-c", sshCommand}

	// Execute command in rsync pod
	stdout, stderr, err := rsyncpod.ExecuteCommandInPod(ctx, p.DestinationK8sClient, rsyncDeployment.Namespace, rsyncDeployment.PodName, cmd)
	if err != nil {
		log.WithFields(logrus.Fields{
			"stderr": stderr,
			"error":  err,
		}).Error("Failed to execute SSH command")
		return "", fmt.Errorf("failed to execute SSH command: %v", err)
	}

	log.WithFields(logrus.Fields{
		"stdout": stdout,
	}).Debug("SSH command executed successfully")

	return stdout, nil
}

// GetPVCsToSync returns a list of PVCs that should be synchronized
func (p *PVCSyncer) GetPVCsToSync(ctx context.Context, sourceNS, destNS string, selector client.MatchingLabels) ([]string, error) {
	log.WithFields(logrus.Fields{
		"source_namespace": sourceNS,
		"dest_namespace":   destNS,
	}).Info("Getting PVCs to sync")

	// List PVCs in source namespace
	pvcList, err := p.SourceK8sClient.CoreV1().PersistentVolumeClaims(sourceNS).List(ctx, metav1.ListOptions{
		LabelSelector: metav1.FormatLabelSelector(&metav1.LabelSelector{
			MatchLabels: selector,
		}),
	})
	if err != nil {
		log.WithFields(logrus.Fields{
			"source_namespace": sourceNS,
			"error":            err,
		}).Error("Failed to list PVCs in source namespace")
		return nil, fmt.Errorf("failed to list PVCs in source namespace: %v", err)
	}

	// Extract PVC names
	var pvcNames []string
	for _, pvc := range pvcList.Items {
		pvcNames = append(pvcNames, pvc.Name)
	}

	log.WithFields(logrus.Fields{
		"source_namespace": sourceNS,
		"pvc_count":        len(pvcNames),
	}).Info("Found PVCs to sync")

	return pvcNames, nil
}

// ValidatePVCSync validates that a PVC sync operation is valid
func (p *PVCSyncer) ValidatePVCSync(ctx context.Context, sourcePVCName, sourceNamespace, destPVCName, destNamespace string) error {
	log.WithFields(logrus.Fields{
		"source_pvc":       sourcePVCName,
		"source_namespace": sourceNamespace,
		"dest_pvc":         destPVCName,
		"dest_namespace":   destNamespace,
	}).Info("Validating PVC sync operation")

	// Check if source PVC exists
	_, err := p.SourceK8sClient.CoreV1().PersistentVolumeClaims(sourceNamespace).Get(ctx, sourcePVCName, metav1.GetOptions{})
	if err != nil {
		log.WithFields(logrus.Fields{
			"source_pvc":       sourcePVCName,
			"source_namespace": sourceNamespace,
			"error":            err,
		}).Error("Source PVC does not exist")
		return fmt.Errorf("source PVC does not exist: %v", err)
	}

	// Check if destination PVC exists
	_, err = p.DestinationK8sClient.CoreV1().PersistentVolumeClaims(destNamespace).Get(ctx, destPVCName, metav1.GetOptions{})
	if err != nil {
		log.WithFields(logrus.Fields{
			"dest_pvc":       destPVCName,
			"dest_namespace": destNamespace,
			"error":          err,
		}).Error("Destination PVC does not exist")
		return fmt.Errorf("destination PVC does not exist: %v", err)
	}

	log.WithFields(logrus.Fields{
		"source_pvc":       sourcePVCName,
		"source_namespace": sourceNamespace,
		"dest_pvc":         destPVCName,
		"dest_namespace":   destNamespace,
	}).Info("PVC sync operation is valid")

	return nil
}

// LogSyncProgress logs the progress of a sync operation
func (p *PVCSyncer) LogSyncProgress(ctx context.Context, sourcePVCName, sourceNamespace, destPVCName, destNamespace string, phase string, message string) {
	log.WithFields(logrus.Fields{
		"source_pvc":       sourcePVCName,
		"source_namespace": sourceNamespace,
		"dest_pvc":         destPVCName,
		"dest_namespace":   destNamespace,
		"phase":            phase,
		"message":          message,
	}).Info("PVC sync progress update")
}
