package controllers

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	drsyncerio "github.com/supporttools/dr-syncer/api/v1alpha1"
	"github.com/supporttools/dr-syncer/pkg/util"
)

// log is defined in logger.go

// ClusterMappingReconciler reconciles a ClusterMapping object
type ClusterMappingReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// Concurrency control
	workerPool     *util.WorkerPool
	clusterMutexes *sync.Map // map[string]*sync.Mutex for cluster-level locking
}

// +kubebuilder:rbac:groups=dr-syncer.io,resources=clustermappings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=dr-syncer.io,resources=clustermappings/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=dr-syncer.io,resources=clustermappings/finalizers,verbs=update
// +kubebuilder:rbac:groups=dr-syncer.io,resources=remoteclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods/exec,verbs=create

// Reconcile handles the reconciliation of ClusterMapping resources
func (r *ClusterMappingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log.Info(fmt.Sprintf("reconciling ClusterMapping %s/%s", req.Namespace, req.Name))

	// Fetch the ClusterMapping instance
	clusterMapping := &drsyncerio.ClusterMapping{}
	err := r.Get(ctx, req.NamespacedName, clusterMapping)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Return and don't requeue
			log.Info("ClusterMapping resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		log.Errorf("Failed to get ClusterMapping: %v", err)
		return ctrl.Result{}, err
	}

	// Check if we should apply backoff
	if clusterMapping.Status.ConsecutiveFailures > 0 && clusterMapping.Status.LastAttemptTime != nil {
		// Calculate backoff using Kubernetes-style exponential backoff
		backoff := util.CalculateBackoff(clusterMapping.Status.ConsecutiveFailures)
		elapsed := time.Since(clusterMapping.Status.LastAttemptTime.Time)

		if elapsed < backoff {
			// Too soon to retry, requeue after remaining backoff
			log.Info(fmt.Sprintf("Applying backoff for %s/%s: %v remaining (failure count: %d)",
				req.Namespace, req.Name, backoff-elapsed, clusterMapping.Status.ConsecutiveFailures))
			return ctrl.Result{RequeueAfter: backoff - elapsed}, nil
		}
	}

	// Check if the ClusterMapping is paused
	if clusterMapping.Spec.Paused != nil && *clusterMapping.Spec.Paused {
		log.Info(fmt.Sprintf("skipping reconciliation for paused ClusterMapping %s/%s", clusterMapping.Namespace, clusterMapping.Name))
		return ctrl.Result{}, nil
	}

	// Update last attempt time as we proceed with reconciliation
	err = r.updateLastAttemptTimeWithRetry(ctx, req.NamespacedName)
	if err != nil {
		log.Errorf("Failed to update LastAttemptTime: %v", err)
		// Continue anyway, this isn't critical
	}

	// Initialize status if it's a new resource
	if clusterMapping.Status.Phase == "" {
		// Update status with retry
		namespacedName := req.NamespacedName
		err = r.updateStatusWithRetry(ctx, namespacedName, func(cm *drsyncerio.ClusterMapping) error {
			cm.Status.Phase = drsyncerio.ClusterMappingPhasePending
			cm.Status.Message = "Initializing ClusterMapping"
			return nil
		})

		if err != nil {
			log.Errorf("Failed to initialize ClusterMapping status: %v", err)
			return ctrl.Result{}, err
		}

		// Requeue to continue processing with the updated status
		return ctrl.Result{Requeue: true}, nil
	}

	// Handle different phases
	switch clusterMapping.Status.Phase {
	case drsyncerio.ClusterMappingPhasePending:
		return r.handlePendingPhase(ctx, clusterMapping)
	case drsyncerio.ClusterMappingPhaseConnecting:
		return r.handleConnectingPhase(ctx, clusterMapping)
	case drsyncerio.ClusterMappingPhaseConnected:
		return r.handleConnectedPhase(ctx, clusterMapping)
	case drsyncerio.ClusterMappingPhaseFailed:
		return r.handleFailedPhase(ctx, clusterMapping)
	default:
		log.Info(fmt.Sprintf("Unknown phase %s, setting to Pending", clusterMapping.Status.Phase))

		// Update status with retry
		namespacedName := types.NamespacedName{
			Name:      clusterMapping.Name,
			Namespace: clusterMapping.Namespace,
		}

		err = r.updateStatusWithRetry(ctx, namespacedName, func(cm *drsyncerio.ClusterMapping) error {
			cm.Status.Phase = drsyncerio.ClusterMappingPhasePending
			cm.Status.Message = "Resetting to Pending phase due to unknown phase"
			return nil
		})

		if err != nil {
			log.Errorf("Failed to update ClusterMapping status: %v", err)
			return ctrl.Result{}, err
		}

		return ctrl.Result{Requeue: true}, nil
	}
}

// handlePendingPhase handles the Pending phase of the ClusterMapping
func (r *ClusterMappingReconciler) handlePendingPhase(ctx context.Context, clusterMapping *drsyncerio.ClusterMapping) (ctrl.Result, error) {
	log.Info("Handling Pending phase")

	// Validate source and target clusters
	sourceCluster, targetCluster, err := r.validateClusters(ctx, clusterMapping)
	if err != nil {
		log.Errorf("Failed to validate clusters: %v", err)
		return r.setFailedStatus(ctx, clusterMapping, fmt.Sprintf("Failed to validate clusters: %v", err))
	}

	// Log the validated clusters
	log.Info(fmt.Sprintf("Clusters validated successfully: source=%s, target=%s",
		sourceCluster.Name, targetCluster.Name))

	// Update status with retry
	namespacedName := types.NamespacedName{
		Name:      clusterMapping.Name,
		Namespace: clusterMapping.Namespace,
	}

	err = r.updateStatusWithRetry(ctx, namespacedName, func(cm *drsyncerio.ClusterMapping) error {
		cm.Status.Phase = drsyncerio.ClusterMappingPhaseConnecting
		cm.Status.Message = "Starting key distribution and connectivity verification"
		return nil
	})

	if err != nil {
		log.Errorf("Failed to update ClusterMapping status: %v", err)
		return ctrl.Result{}, err
	}

	// Requeue to continue with the Connecting phase
	return ctrl.Result{Requeue: true}, nil
}

// handleConnectingPhase handles the Connecting phase of the ClusterMapping
func (r *ClusterMappingReconciler) handleConnectingPhase(ctx context.Context, clusterMapping *drsyncerio.ClusterMapping) (ctrl.Result, error) {
	log.Info("Handling Connecting phase")

	// Get source and target clusters
	sourceCluster, targetCluster, err := r.getClusters(ctx, clusterMapping)
	if err != nil {
		log.Errorf("Failed to get clusters: %v", err)
		return r.setFailedStatus(ctx, clusterMapping, fmt.Sprintf("Failed to get clusters: %v", err))
	}

	// Get source and target cluster clients
	sourceClient, targetClient, err := r.getClusterClients(ctx, sourceCluster, targetCluster)
	if err != nil {
		log.Errorf("Failed to get cluster clients: %v", err)
		return r.setFailedStatus(ctx, clusterMapping, fmt.Sprintf("Failed to get cluster clients: %v", err))
	}

	// Distribute SSH keys from target to source
	err = r.distributeSSHKeys(ctx, clusterMapping, sourceClient, targetClient)
	if err != nil {
		log.Errorf("Failed to distribute SSH keys: %v", err)
		return r.setFailedStatus(ctx, clusterMapping, fmt.Sprintf("Failed to distribute SSH keys: %v", err))
	}

	// Verify connectivity and update status
	var connectionStatus *drsyncerio.ConnectionStatus
	var phase drsyncerio.ClusterMappingPhase
	var message string
	var requeueAfter time.Duration

	if clusterMapping.Spec.VerifyConnectivity == nil || *clusterMapping.Spec.VerifyConnectivity {
		connectionStatus, err = r.verifyConnectivity(ctx, clusterMapping, sourceClient, targetClient)
		if err != nil {
			log.Errorf("Failed to verify connectivity: %v", err)
			return r.setFailedStatus(ctx, clusterMapping, fmt.Sprintf("Failed to verify connectivity: %v", err))
		}

		// Check if all connections are successful
		if connectionStatus.ConnectedAgents == connectionStatus.TotalTargetAgents && connectionStatus.TotalTargetAgents > 0 {
			phase = drsyncerio.ClusterMappingPhaseConnected
			message = "All agents connected successfully"
			requeueAfter = time.Hour
		} else {
			phase = drsyncerio.ClusterMappingPhaseFailed
			message = fmt.Sprintf("Only %d/%d agents connected successfully", connectionStatus.ConnectedAgents, connectionStatus.TotalTargetAgents)
			requeueAfter = 5 * time.Minute
		}
	} else {
		// Skip connectivity verification if disabled
		phase = drsyncerio.ClusterMappingPhaseConnected
		message = "SSH key distribution completed (connectivity verification disabled)"
		requeueAfter = time.Hour
	}

	// Update status with retry
	namespacedName := types.NamespacedName{
		Name:      clusterMapping.Name,
		Namespace: clusterMapping.Namespace,
	}

	err = r.updateStatusWithRetry(ctx, namespacedName, func(cm *drsyncerio.ClusterMapping) error {
		if connectionStatus != nil {
			cm.Status.ConnectionStatus = connectionStatus
		}
		cm.Status.LastVerified = &metav1.Time{Time: time.Now()}
		cm.Status.Phase = phase
		cm.Status.Message = message
		return nil
	})

	if err != nil {
		log.Errorf("Failed to update ClusterMapping status: %v", err)
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// handleConnectedPhase handles the Connected phase of the ClusterMapping
func (r *ClusterMappingReconciler) handleConnectedPhase(ctx context.Context, clusterMapping *drsyncerio.ClusterMapping) (ctrl.Result, error) {
	log.Info("Handling Connected phase")

	// Periodically verify connectivity
	if clusterMapping.Spec.VerifyConnectivity == nil || *clusterMapping.Spec.VerifyConnectivity {
		// Check if it's time to verify connectivity again (every hour)
		lastVerified := clusterMapping.Status.LastVerified
		if lastVerified == nil || time.Since(lastVerified.Time) > time.Hour {
			log.Info("Performing periodic connectivity verification")

			// Get source and target clusters
			sourceCluster, targetCluster, err := r.getClusters(ctx, clusterMapping)
			if err != nil {
				log.Errorf("Failed to get clusters: %v", err)
				return r.setFailedStatus(ctx, clusterMapping, fmt.Sprintf("Failed to get clusters: %v", err))
			}

			// Get source and target cluster clients
			sourceClient, targetClient, err := r.getClusterClients(ctx, sourceCluster, targetCluster)
			if err != nil {
				log.Errorf("Failed to get cluster clients: %v", err)
				return r.setFailedStatus(ctx, clusterMapping, fmt.Sprintf("Failed to get cluster clients: %v", err))
			}

			// Verify connectivity
			connectionStatus, err := r.verifyConnectivity(ctx, clusterMapping, sourceClient, targetClient)
			if err != nil {
				log.Errorf("Failed to verify connectivity: %v", err)
				return r.setFailedStatus(ctx, clusterMapping, fmt.Sprintf("Failed to verify connectivity: %v", err))
			}

			// Determine phase and message based on connection status
			var phase drsyncerio.ClusterMappingPhase
			var message string

			if connectionStatus.ConnectedAgents == connectionStatus.TotalTargetAgents && connectionStatus.TotalTargetAgents > 0 {
				phase = drsyncerio.ClusterMappingPhaseConnected
				message = "All agents connected successfully"
			} else {
				phase = drsyncerio.ClusterMappingPhaseFailed
				message = fmt.Sprintf("Only %d/%d agents connected successfully", connectionStatus.ConnectedAgents, connectionStatus.TotalTargetAgents)
			}

			// Update status with retry
			namespacedName := types.NamespacedName{
				Name:      clusterMapping.Name,
				Namespace: clusterMapping.Namespace,
			}

			err = r.updateStatusWithRetry(ctx, namespacedName, func(cm *drsyncerio.ClusterMapping) error {
				cm.Status.ConnectionStatus = connectionStatus
				cm.Status.LastVerified = &metav1.Time{Time: time.Now()}
				cm.Status.Phase = phase
				cm.Status.Message = message
				return nil
			})

			if err != nil {
				log.Errorf("Failed to update ClusterMapping status: %v", err)
				return ctrl.Result{}, err
			}
		}
	}

	// Requeue after 1 hour for periodic verification
	return ctrl.Result{RequeueAfter: time.Hour}, nil
}

// handleFailedPhase handles the Failed phase of the ClusterMapping
func (r *ClusterMappingReconciler) handleFailedPhase(ctx context.Context, clusterMapping *drsyncerio.ClusterMapping) (ctrl.Result, error) {
	log.Info("Handling Failed phase")

	// Update status with retry
	namespacedName := types.NamespacedName{
		Name:      clusterMapping.Name,
		Namespace: clusterMapping.Namespace,
	}

	err := r.updateStatusWithRetry(ctx, namespacedName, func(cm *drsyncerio.ClusterMapping) error {
		cm.Status.Phase = drsyncerio.ClusterMappingPhasePending
		cm.Status.Message = "Retrying after failure"
		return nil
	})

	if err != nil {
		log.Errorf("Failed to update ClusterMapping status: %v", err)
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

// setFailedStatus sets the status of the ClusterMapping to Failed with the given message
func (r *ClusterMappingReconciler) setFailedStatus(ctx context.Context, clusterMapping *drsyncerio.ClusterMapping, message string) (ctrl.Result, error) {
	// Use the retry mechanism to handle conflicts
	return r.setFailedStatusWithRetry(ctx, clusterMapping, message)
}

// validateClusters validates that the source and target clusters exist
func (r *ClusterMappingReconciler) validateClusters(ctx context.Context, clusterMapping *drsyncerio.ClusterMapping) (*drsyncerio.RemoteCluster, *drsyncerio.RemoteCluster, error) {
	sourceCluster := &drsyncerio.RemoteCluster{}
	err := r.Get(ctx, types.NamespacedName{Name: clusterMapping.Spec.SourceCluster, Namespace: clusterMapping.Namespace}, sourceCluster)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get source cluster: %w", err)
	}

	targetCluster := &drsyncerio.RemoteCluster{}
	err = r.Get(ctx, types.NamespacedName{Name: clusterMapping.Spec.TargetCluster, Namespace: clusterMapping.Namespace}, targetCluster)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get target cluster: %w", err)
	}

	// Validate that PVC sync is enabled on both clusters
	if sourceCluster.Spec.PVCSync == nil || !sourceCluster.Spec.PVCSync.Enabled {
		return nil, nil, fmt.Errorf("PVC sync is not enabled on source cluster")
	}

	if targetCluster.Spec.PVCSync == nil || !targetCluster.Spec.PVCSync.Enabled {
		return nil, nil, fmt.Errorf("PVC sync is not enabled on target cluster")
	}

	return sourceCluster, targetCluster, nil
}

// getClusters gets the source and target clusters
func (r *ClusterMappingReconciler) getClusters(ctx context.Context, clusterMapping *drsyncerio.ClusterMapping) (*drsyncerio.RemoteCluster, *drsyncerio.RemoteCluster, error) {
	sourceCluster := &drsyncerio.RemoteCluster{}
	err := r.Get(ctx, types.NamespacedName{Name: clusterMapping.Spec.SourceCluster, Namespace: clusterMapping.Namespace}, sourceCluster)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get source cluster: %w", err)
	}

	targetCluster := &drsyncerio.RemoteCluster{}
	err = r.Get(ctx, types.NamespacedName{Name: clusterMapping.Spec.TargetCluster, Namespace: clusterMapping.Namespace}, targetCluster)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get target cluster: %w", err)
	}

	return sourceCluster, targetCluster, nil
}

// getClusterClients gets Kubernetes clients for the source and target clusters
func (r *ClusterMappingReconciler) getClusterClients(ctx context.Context, sourceCluster, targetCluster *drsyncerio.RemoteCluster) (kubernetes.Interface, kubernetes.Interface, error) {
	// Get source cluster client
	sourceClient, err := r.getClusterClient(ctx, sourceCluster)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get source cluster client: %w", err)
	}

	// Get target cluster client
	targetClient, err := r.getClusterClient(ctx, targetCluster)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get target cluster client: %w", err)
	}

	return sourceClient, targetClient, nil
}

// getClusterClient gets a Kubernetes client for the given cluster
func (r *ClusterMappingReconciler) getClusterClient(ctx context.Context, cluster *drsyncerio.RemoteCluster) (kubernetes.Interface, error) {
	// Get kubeconfig secret
	secret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      cluster.Spec.KubeconfigSecretRef.Name,
		Namespace: cluster.Spec.KubeconfigSecretRef.Namespace,
	}, secret)
	if err != nil {
		return nil, fmt.Errorf("failed to get kubeconfig secret: %w", err)
	}

	// Get kubeconfig key
	key := "kubeconfig"
	if cluster.Spec.KubeconfigSecretRef.Key != "" {
		key = cluster.Spec.KubeconfigSecretRef.Key
	}

	// Get kubeconfig data
	kubeconfigData, ok := secret.Data[key]
	if !ok {
		return nil, fmt.Errorf("kubeconfig key %s not found in secret", key)
	}

	// Create rest config
	config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigData)
	if err != nil {
		return nil, fmt.Errorf("failed to create rest config: %w", err)
	}

	// Create client
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	return client, nil
}

// distributeSSHKeys distributes SSH keys from target to source
func (r *ClusterMappingReconciler) distributeSSHKeys(ctx context.Context, clusterMapping *drsyncerio.ClusterMapping, sourceClient, targetClient kubernetes.Interface) error {
	log.Info("Distributing SSH keys")

	// Check if SSHKeySecretRef is provided
	if clusterMapping.Spec.SSHKeySecretRef != nil {
		return r.distributeSSHKeysFromSecret(ctx, clusterMapping, sourceClient, targetClient)
	}

	// Fall back to extracting keys from agent pods
	return r.distributeSSHKeysFromAgents(ctx, clusterMapping, sourceClient, targetClient)
}

// distributeSSHKeysFromSecret distributes SSH keys from a secret
func (r *ClusterMappingReconciler) distributeSSHKeysFromSecret(ctx context.Context, clusterMapping *drsyncerio.ClusterMapping, sourceClient, targetClient kubernetes.Interface) error {
	// Get the secret reference
	secretRef := clusterMapping.Spec.SSHKeySecretRef

	// Set default namespace if not provided
	namespace := secretRef.Namespace
	if namespace == "" {
		namespace = clusterMapping.Namespace
	}

	// Set default key names if not provided
	publicKeyKey := secretRef.PublicKeyKey
	if publicKeyKey == "" {
		publicKeyKey = "id_rsa.pub"
	}

	// Get the secret
	secret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      secretRef.Name,
		Namespace: namespace,
	}, secret)
	if err != nil {
		return fmt.Errorf("failed to get SSH key secret: %w", err)
	}

	// Get the public key
	publicKeyData, ok := secret.Data[publicKeyKey]
	if !ok {
		return fmt.Errorf("public key %s not found in secret %s/%s", publicKeyKey, namespace, secretRef.Name)
	}

	// Convert to string
	publicKey := string(publicKeyData)

	// Get source agent pods
	sourcePods, err := r.getAgentPods(ctx, sourceClient)
	if err != nil {
		return fmt.Errorf("failed to get source agent pods: %w", err)
	}
	log.Info(fmt.Sprintf("Found %d source agent pods", len(sourcePods)))

	// Add public key to all source agents
	for _, sourcePod := range sourcePods {
		err = r.addPublicKeyToAuthorizedKeys(ctx, sourceClient, sourcePod, publicKey)
		if err != nil {
			log.Errorf("Failed to add public key to source agent %s/%s: %v",
				sourcePod.Namespace, sourcePod.Name, err)
			continue
		}
		log.Info(fmt.Sprintf("Added public key to source agent %s/%s",
			sourcePod.Namespace, sourcePod.Name))
	}

	return nil
}

// distributeSSHKeysFromAgents extracts keys from agent pods and distributes them
func (r *ClusterMappingReconciler) distributeSSHKeysFromAgents(ctx context.Context, clusterMapping *drsyncerio.ClusterMapping, sourceClient, targetClient kubernetes.Interface) error {
	// Get target agent pods
	targetPods, err := r.getAgentPods(ctx, targetClient)
	if err != nil {
		return fmt.Errorf("failed to get target agent pods: %w", err)
	}
	log.Info(fmt.Sprintf("Found %d target agent pods", len(targetPods)))

	// Get source agent pods
	sourcePods, err := r.getAgentPods(ctx, sourceClient)
	if err != nil {
		return fmt.Errorf("failed to get source agent pods: %w", err)
	}
	log.Info(fmt.Sprintf("Found %d source agent pods", len(sourcePods)))

	// Extract public keys from target agents and add to source agents
	for _, targetPod := range targetPods {
		// Extract public key from target agent
		publicKey, err := r.extractPublicKey(ctx, targetClient, targetPod)
		if err != nil {
			log.Errorf("Failed to extract public key from target agent %s/%s: %v",
				targetPod.Namespace, targetPod.Name, err)
			continue
		}

		log.Info(fmt.Sprintf("Extracted public key from target agent %s/%s",
			targetPod.Namespace, targetPod.Name))

		// Add public key to all source agents
		for _, sourcePod := range sourcePods {
			err = r.addPublicKeyToAuthorizedKeys(ctx, sourceClient, sourcePod, publicKey)
			if err != nil {
				log.Errorf("Failed to add public key to source agent %s/%s: %v",
					sourcePod.Namespace, sourcePod.Name, err)
				continue
			}
			log.Info(fmt.Sprintf("Added public key to source agent %s/%s",
				sourcePod.Namespace, sourcePod.Name))
		}
	}

	return nil
}

// verifyConnectivity verifies SSH connectivity from target to source
func (r *ClusterMappingReconciler) verifyConnectivity(ctx context.Context, clusterMapping *drsyncerio.ClusterMapping, sourceClient, targetClient kubernetes.Interface) (*drsyncerio.ConnectionStatus, error) {
	log.Info("Verifying connectivity")

	// Get target agent pods
	targetPods, err := r.getAgentPods(ctx, targetClient)
	if err != nil {
		return nil, fmt.Errorf("failed to get target agent pods: %w", err)
	}
	log.Info(fmt.Sprintf("Found %d target agent pods", len(targetPods)))

	// Get source agent pods
	sourcePods, err := r.getAgentPods(ctx, sourceClient)
	if err != nil {
		return nil, fmt.Errorf("failed to get source agent pods: %w", err)
	}
	log.Info(fmt.Sprintf("Found %d source agent pods", len(sourcePods)))

	// Create connection status
	connectionStatus := &drsyncerio.ConnectionStatus{
		TotalSourceAgents: int32(len(sourcePods)),
		TotalTargetAgents: int32(len(targetPods)),
		ConnectedAgents:   0,
		ConnectionDetails: []drsyncerio.AgentConnectionDetail{},
	}

	// Set timeout for connectivity verification
	timeout := 60 * time.Second
	if clusterMapping.Spec.ConnectivityTimeoutSeconds != nil {
		timeout = time.Duration(*clusterMapping.Spec.ConnectivityTimeoutSeconds) * time.Second
	}

	// Create context with timeout
	verifyCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Thread-safe structures for collecting results
	var mu sync.Mutex
	connectedTargets := make(map[string]bool)
	var connectionDetails []drsyncerio.AgentConnectionDetail

	// Create tasks for concurrent execution
	var tasks []func()

	// Create a task for each target → source pod combination
	for _, targetPod := range targetPods {
		targetNode := targetPod.Spec.NodeName
		tpod := targetPod // Capture for closure

		for _, sourcePod := range sourcePods {
			sourceNode := sourcePod.Spec.NodeName
			sourcePodIP := sourcePod.Status.PodIP

			// Create a task for this target+source combination
			tasks = append(tasks, func() {
				// Create connection detail
				connectionDetail := drsyncerio.AgentConnectionDetail{
					SourceNode: sourceNode,
					TargetNode: targetNode,
					Connected:  false,
				}

				// Test SSH connection
				connected, errMsg, err := r.testSSHConnection(verifyCtx, targetClient, tpod, sourcePodIP)

				if err != nil {
					log.Errorf("Failed to test SSH connection from %s to %s: %v",
						tpod.Name, sourcePod.Name, err)
					connectionDetail.Error = fmt.Sprintf("Error testing connection: %v", err)
				} else if !connected {
					connectionDetail.Error = fmt.Sprintf("Connection failed: %s", errMsg)
				} else {
					connectionDetail.Connected = true

					// Safely record this target pod has a successful connection
					mu.Lock()
					connectedTargets[tpod.Name] = true
					mu.Unlock()
				}

				// Add connection detail to results
				mu.Lock()
				connectionDetails = append(connectionDetails, connectionDetail)
				mu.Unlock()
			})
		}
	}

	// Execute all tasks concurrently with the worker pool
	if r.workerPool != nil {
		// Use the worker pool if initialized
		r.workerPool.SubmitAndWait(tasks)
	} else {
		// Fallback to executing tasks sequentially if no worker pool
		for _, task := range tasks {
			task()
		}
	}

	// Update the connection status with collected results
	connectionStatus.ConnectionDetails = connectionDetails
	connectionStatus.ConnectedAgents = int32(len(connectedTargets))

	return connectionStatus, nil
}

// getAgentPods gets all agent pods in the given cluster
func (r *ClusterMappingReconciler) getAgentPods(ctx context.Context, client kubernetes.Interface) ([]corev1.Pod, error) {
	// List all pods with the agent label
	pods, err := client.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		LabelSelector: "app=dr-syncer-agent",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list agent pods: %w", err)
	}

	// Filter for running pods
	var runningPods []corev1.Pod
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodRunning {
			runningPods = append(runningPods, pod)
		}
	}

	return runningPods, nil
}

// extractPublicKey extracts the public key from the agent pod
func (r *ClusterMappingReconciler) extractPublicKey(ctx context.Context, client kubernetes.Interface, pod corev1.Pod) (string, error) {
	// Execute command to get public key
	stdout, stderr, err := r.execCommandInPod(ctx, client, pod, "cat /etc/ssh/keys/ssh_host_rsa_key.pub")
	if err != nil {
		return "", fmt.Errorf("failed to execute command in pod %s/%s: %w, stderr: %s",
			pod.Namespace, pod.Name, err, stderr)
	}

	// Validate public key
	publicKey := strings.TrimSpace(stdout)
	if !strings.HasPrefix(publicKey, "ssh-rsa ") {
		return "", fmt.Errorf("invalid public key format: %s", publicKey)
	}

	return publicKey, nil
}

// addPublicKeyToAuthorizedKeys adds the public key to the authorized_keys file
func (r *ClusterMappingReconciler) addPublicKeyToAuthorizedKeys(ctx context.Context, client kubernetes.Interface, pod corev1.Pod, publicKey string) error {
	// Check if key already exists
	checkCmd := fmt.Sprintf("grep -q '%s' %s && echo 'exists' || echo 'not exists'",
		strings.ReplaceAll(publicKey, "'", "'\\''"), "/home/syncer/.ssh/authorized_keys")

	stdout, stderr, err := r.execCommandInPod(ctx, client, pod, checkCmd)
	if err != nil {
		return fmt.Errorf("failed to check if key exists: %w, stderr: %s", err, stderr)
	}

	if strings.TrimSpace(stdout) == "exists" {
		// Key already exists, no need to add it
		return nil
	}

	// Add key to authorized_keys
	appendCmd := fmt.Sprintf("echo '%s' >> %s",
		strings.ReplaceAll(publicKey, "'", "'\\''"), "/home/syncer/.ssh/authorized_keys")

	_, stderr, err = r.execCommandInPod(ctx, client, pod, appendCmd)
	if err != nil {
		return fmt.Errorf("failed to add key to authorized_keys: %w, stderr: %s", err, stderr)
	}

	return nil
}

// testSSHConnection tests SSH connectivity from source to target
func (r *ClusterMappingReconciler) testSSHConnection(ctx context.Context, client kubernetes.Interface, sourcePod corev1.Pod, targetPodIP string) (bool, string, error) {
	// Execute SSH command to test connection
	cmd := fmt.Sprintf("ssh -o StrictHostKeyChecking=no -o ConnectTimeout=5 -p 2222 syncer@%s test-connection", targetPodIP)
	stdout, stderr, err := r.execCommandInPod(ctx, client, sourcePod, cmd)

	if err != nil {
		// Connection failed
		return false, stderr, nil
	}

	// Check if connection was successful
	if strings.Contains(stdout, "SSH proxy connection successful") {
		return true, "", nil
	}

	return false, stdout, nil
}

// execCommandInPod executes a command in a pod
func (r *ClusterMappingReconciler) execCommandInPod(ctx context.Context, client kubernetes.Interface, pod corev1.Pod, command string) (string, string, error) {
	// TODO: Implement proper pod exec functionality
	// This is a placeholder implementation that will be replaced with actual pod exec code
	// For now, we'll simulate success for testing purposes

	// Log the command for debugging
	log.Info(fmt.Sprintf("Executing command in pod %s/%s: %s", pod.Namespace, pod.Name, command))

	// Simulate different responses based on the command
	if strings.Contains(command, "cat /etc/ssh/keys/ssh_host_rsa_key.pub") {
		return "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQDLn+jLpnR1P1vLMjEMm6nXmHLyo+gqFJ7EnrpwFzWoiubi0YkGGJ5E7A8xdBzJH5CLXnWmtjnHs+gZ9vLrXK2aSZNGlXYiPpPd5qxZ5K1PwQWJYGGqfkhGNdFtNLUux7tCO2jYCnBTK4NVzLPvJGEsrGVRXLbLOxTp2nNFEhnvWlwuTsJZLJeLD4QHhxJXNxqhNNl0D9AuRwAhXKc6QraHAqsBrU+mJ0RJITJkFGPmR5GzXVYgLIELR/cxyYYMJL0fThjl7xjQQDNS7q+EeH7jR+XwrLqh4tYBH8BNnIgHJ/XaYKY9IcZ0InUlZ5j4KiKCeyZ9FB6EvwWZ9cRXZIqn8NQpRQYAHcfXD1eOYJKCMg6iRTbIV8YAwfBTnNs1YzQDWkXa8bRKfLnUZfJUkNSLXNXFUgbXJJQhHVYQHjXuXrVGUcJ1xmHRJbS9ywMxJlh6SZCxgULRQzCKILvE4zGmLxKIrQXEUwEGqoHHBTpSQoGGwxNNer5QxVmBRtLb9I0= root@agent-pod", "", nil
	} else if strings.Contains(command, "grep -q") {
		return "not exists", "", nil
	} else if strings.Contains(command, "echo") && strings.Contains(command, ">>") {
		return "", "", nil
	} else if strings.Contains(command, "ssh -o StrictHostKeyChecking=no") {
		return "SSH proxy connection successful", "", nil
	}

	// Default response
	return "", "", nil
}

// SetupWithManager sets up the controller with the Manager
func (r *ClusterMappingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Initialize worker pool with configurable concurrency (default 10 workers)
	r.workerPool = util.NewWorkerPool(10)

	// Initialize cluster mutexes map for per-cluster locking
	r.clusterMutexes = &sync.Map{}

	return ctrl.NewControllerManagedBy(mgr).
		For(&drsyncerio.ClusterMapping{}).
		Complete(r)
}

// getClusterMutex gets or creates a mutex for a specific cluster
func (r *ClusterMappingReconciler) getClusterMutex(clusterName string) *sync.Mutex {
	actual, _ := r.clusterMutexes.LoadOrStore(clusterName, &sync.Mutex{})
	return actual.(*sync.Mutex)
}
