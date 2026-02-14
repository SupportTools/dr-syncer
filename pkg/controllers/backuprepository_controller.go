package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
	"github.com/supporttools/dr-syncer/pkg/logging"
)

const (
	// BackupRepositoryFinalizerName is the finalizer added to BackupRepository resources
	BackupRepositoryFinalizerName = "dr-syncer.io/cleanup-backuprepository"

	// Default intervals for health checks and requeue
	defaultHealthCheckInterval = 5 * time.Minute
	defaultRequeueOnError      = 1 * time.Minute
)

// RepositoryClient defines the interface for Kopia repository operations.
// This will be implemented by task #9675 in pkg/controller/backup/repository_client.go.
type RepositoryClient interface {
	// InitializeRepository creates or connects to a Kopia repository in S3
	InitializeRepository(ctx context.Context, s3Config drv1alpha1.S3Config, kopiaConfig drv1alpha1.KopiaConfig, namespace string) error

	// CheckHealth verifies repository connectivity and returns stats
	CheckHealth(ctx context.Context, s3Config drv1alpha1.S3Config, kopiaConfig drv1alpha1.KopiaConfig, namespace string) (*RepositoryStats, error)

	// RunMaintenance triggers repository maintenance (index compaction, blob GC, snapshot GC)
	RunMaintenance(ctx context.Context, s3Config drv1alpha1.S3Config, kopiaConfig drv1alpha1.KopiaConfig, namespace string) error
}

// RepositoryStats holds repository statistics returned by health checks
type RepositoryStats struct {
	RepositorySize int64
	SnapshotCount  int32
}

// BackupRepositoryReconciler reconciles BackupRepository objects
type BackupRepositoryReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	// RepoClient is the Kopia repository client. If nil, the reconciler will
	// log warnings and requeue until a client is set (allows incremental rollout).
	RepoClient RepositoryClient
}

// +kubebuilder:rbac:groups=dr-syncer.io,resources=backuprepositories,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=dr-syncer.io,resources=backuprepositories/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=dr-syncer.io,resources=backuprepositories/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// SetupWithManager sets up the controller with the manager
func (r *BackupRepositoryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	logging.LogInfo(nil, "setting up BackupRepository controller")

	r.Recorder = mgr.GetEventRecorderFor("backuprepository-controller")

	return ctrl.NewControllerManagedBy(mgr).
		For(&drv1alpha1.BackupRepository{}).
		Complete(r)
}

// Reconcile handles the reconciliation loop for BackupRepository resources
func (r *BackupRepositoryReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logging.LogInfo(nil, fmt.Sprintf("reconciling BackupRepository %s/%s", req.Namespace, req.Name))

	// Fetch the BackupRepository instance
	var repo drv1alpha1.BackupRepository
	if err := r.Get(ctx, req.NamespacedName, &repo); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		logging.LogError(nil, fmt.Sprintf("unable to fetch BackupRepository: %v", err))
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !repo.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, &repo)
	}

	// Add finalizer if it doesn't exist
	if !controllerutil.ContainsFinalizer(&repo, BackupRepositoryFinalizerName) {
		controllerutil.AddFinalizer(&repo, BackupRepositoryFinalizerName)
		if err := r.Update(ctx, &repo); err != nil {
			logging.LogError(nil, fmt.Sprintf("failed to add finalizer to BackupRepository %s: %v", repo.Name, err))
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Verify that referenced secrets exist
	if err := r.validateSecretRefs(ctx, &repo); err != nil {
		return r.updateStatusWithError(ctx, &repo, "SecretValidation", err)
	}

	// Check if RepositoryClient is available
	if r.RepoClient == nil {
		logging.LogWarn(nil, fmt.Sprintf("BackupRepository %s: RepositoryClient not configured, requeuing", repo.Name))
		return r.updateStatusCondition(ctx, &repo,
			drv1alpha1.BackupRepositoryStateInitializing,
			"RepositoryClientReady", metav1.ConditionFalse,
			"ClientNotConfigured", "RepositoryClient implementation not yet available",
			defaultRequeueOnError)
	}

	// Route based on current state
	switch repo.Status.State {
	case "", drv1alpha1.BackupRepositoryStateInitializing:
		return r.handleInitialization(ctx, &repo)
	case drv1alpha1.BackupRepositoryStateReady:
		return r.handleReady(ctx, &repo)
	case drv1alpha1.BackupRepositoryStateError:
		return r.handleError(ctx, &repo)
	case drv1alpha1.BackupRepositoryStateMaintenance:
		return r.handleMaintenance(ctx, &repo)
	default:
		logging.LogWarn(nil, fmt.Sprintf("BackupRepository %s has unknown state: %s", repo.Name, repo.Status.State))
		return r.handleInitialization(ctx, &repo)
	}
}

// handleDeletion handles BackupRepository deletion with finalizer cleanup
func (r *BackupRepositoryReconciler) handleDeletion(ctx context.Context, repo *drv1alpha1.BackupRepository) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(repo, BackupRepositoryFinalizerName) {
		return ctrl.Result{}, nil
	}

	logging.LogInfo(nil, fmt.Sprintf("handling deletion of BackupRepository %s (S3 data will be preserved)", repo.Name))
	r.Recorder.Event(repo, corev1.EventTypeNormal, "Deleting", "BackupRepository being deleted; S3 data is preserved")

	// Remove finalizer - we intentionally do NOT delete S3 data on CR deletion
	controllerutil.RemoveFinalizer(repo, BackupRepositoryFinalizerName)
	if err := r.Update(ctx, repo); err != nil {
		logging.LogError(nil, fmt.Sprintf("failed to remove finalizer from BackupRepository %s: %v", repo.Name, err))
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// handleInitialization initializes a new Kopia repository
func (r *BackupRepositoryReconciler) handleInitialization(ctx context.Context, repo *drv1alpha1.BackupRepository) (ctrl.Result, error) {
	logging.LogInfo(nil, fmt.Sprintf("initializing BackupRepository %s", repo.Name))
	r.Recorder.Event(repo, corev1.EventTypeNormal, "Initializing", "Starting repository initialization")

	err := r.RepoClient.InitializeRepository(ctx, repo.Spec.S3Config, repo.Spec.KopiaConfig, repo.Namespace)
	if err != nil {
		logging.LogError(nil, fmt.Sprintf("failed to initialize BackupRepository %s: %v", repo.Name, err))
		r.Recorder.Eventf(repo, corev1.EventTypeWarning, "InitializationFailed", "Repository initialization failed: %v", err)
		return r.updateStatusWithError(ctx, repo, "Initialization", err)
	}

	logging.LogInfo(nil, fmt.Sprintf("BackupRepository %s initialized successfully", repo.Name))
	r.Recorder.Event(repo, corev1.EventTypeNormal, "Initialized", "Repository initialized successfully")

	return r.updateStatusCondition(ctx, repo,
		drv1alpha1.BackupRepositoryStateReady,
		"RepositoryInitialized", metav1.ConditionTrue,
		"InitSuccess", "Repository has been initialized and is ready",
		defaultHealthCheckInterval)
}

// handleReady handles a repository in Ready state - runs health checks and checks maintenance schedule
func (r *BackupRepositoryReconciler) handleReady(ctx context.Context, repo *drv1alpha1.BackupRepository) (ctrl.Result, error) {
	// Run health check
	stats, err := r.RepoClient.CheckHealth(ctx, repo.Spec.S3Config, repo.Spec.KopiaConfig, repo.Namespace)
	if err != nil {
		logging.LogError(nil, fmt.Sprintf("health check failed for BackupRepository %s: %v", repo.Name, err))
		r.Recorder.Eventf(repo, corev1.EventTypeWarning, "HealthCheckFailed", "Health check failed: %v", err)
		return r.updateStatusWithError(ctx, repo, "HealthCheck", err)
	}

	// Check if maintenance is due
	if r.isMaintenanceDue(repo) {
		logging.LogInfo(nil, fmt.Sprintf("maintenance is due for BackupRepository %s", repo.Name))
		return r.handleMaintenance(ctx, repo)
	}

	// Update status with latest stats
	return r.updateStatusHealthy(ctx, repo, stats)
}

// handleError handles a repository in Error state - retry initialization or health check
func (r *BackupRepositoryReconciler) handleError(ctx context.Context, repo *drv1alpha1.BackupRepository) (ctrl.Result, error) {
	logging.LogInfo(nil, fmt.Sprintf("retrying health check for BackupRepository %s in Error state", repo.Name))

	// Try a health check to see if the issue resolved
	stats, err := r.RepoClient.CheckHealth(ctx, repo.Spec.S3Config, repo.Spec.KopiaConfig, repo.Namespace)
	if err != nil {
		logging.LogError(nil, fmt.Sprintf("health check still failing for BackupRepository %s: %v", repo.Name, err))
		return r.updateStatusWithError(ctx, repo, "HealthCheck", err)
	}

	// Repository recovered
	logging.LogInfo(nil, fmt.Sprintf("BackupRepository %s recovered from Error state", repo.Name))
	r.Recorder.Event(repo, corev1.EventTypeNormal, "Recovered", "Repository recovered from error state")

	return r.updateStatusHealthy(ctx, repo, stats)
}

// handleMaintenance runs Kopia repository maintenance
func (r *BackupRepositoryReconciler) handleMaintenance(ctx context.Context, repo *drv1alpha1.BackupRepository) (ctrl.Result, error) {
	logging.LogInfo(nil, fmt.Sprintf("running maintenance for BackupRepository %s", repo.Name))
	r.Recorder.Event(repo, corev1.EventTypeNormal, "MaintenanceStarted", "Starting repository maintenance")

	// Update state to Maintenance
	if _, err := r.updateStatusCondition(ctx, repo,
		drv1alpha1.BackupRepositoryStateMaintenance,
		"MaintenanceInProgress", metav1.ConditionTrue,
		"Running", "Repository maintenance is in progress",
		0); err != nil {
		return ctrl.Result{}, err
	}

	// Re-fetch after status update
	var latest drv1alpha1.BackupRepository
	if err := r.Get(ctx, client.ObjectKeyFromObject(repo), &latest); err != nil {
		return ctrl.Result{}, err
	}

	err := r.RepoClient.RunMaintenance(ctx, latest.Spec.S3Config, latest.Spec.KopiaConfig, latest.Namespace)
	if err != nil {
		logging.LogError(nil, fmt.Sprintf("maintenance failed for BackupRepository %s: %v", repo.Name, err))
		r.Recorder.Eventf(repo, corev1.EventTypeWarning, "MaintenanceFailed", "Repository maintenance failed: %v", err)
		return r.updateStatusWithError(ctx, &latest, "Maintenance", err)
	}

	logging.LogInfo(nil, fmt.Sprintf("maintenance completed for BackupRepository %s", repo.Name))
	r.Recorder.Event(repo, corev1.EventTypeNormal, "MaintenanceCompleted", "Repository maintenance completed successfully")

	// Update status with maintenance time and return to Ready
	now := metav1.Now()
	return r.updateStatusAfterMaintenance(ctx, &latest, &now)
}

// validateSecretRefs checks that the referenced S3 credentials and Kopia encryption secrets exist
func (r *BackupRepositoryReconciler) validateSecretRefs(ctx context.Context, repo *drv1alpha1.BackupRepository) error {
	// Check S3 credentials secret
	s3SecretRef := repo.Spec.S3Config.CredentialsSecretRef
	var s3Secret corev1.Secret
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: s3SecretRef.Namespace,
		Name:      s3SecretRef.Name,
	}, &s3Secret); err != nil {
		return fmt.Errorf("S3 credentials secret %s/%s not found: %w", s3SecretRef.Namespace, s3SecretRef.Name, err)
	}

	// Verify required keys exist
	if _, ok := s3Secret.Data["accessKeyID"]; !ok {
		return fmt.Errorf("S3 credentials secret %s/%s missing 'accessKeyID' key", s3SecretRef.Namespace, s3SecretRef.Name)
	}
	if _, ok := s3Secret.Data["secretAccessKey"]; !ok {
		return fmt.Errorf("S3 credentials secret %s/%s missing 'secretAccessKey' key", s3SecretRef.Namespace, s3SecretRef.Name)
	}

	// Check Kopia encryption secret
	encSecretRef := repo.Spec.KopiaConfig.EncryptionSecretRef
	var encSecret corev1.Secret
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: encSecretRef.Namespace,
		Name:      encSecretRef.Name,
	}, &encSecret); err != nil {
		return fmt.Errorf("Kopia encryption secret %s/%s not found: %w", encSecretRef.Namespace, encSecretRef.Name, err)
	}

	if _, ok := encSecret.Data["password"]; !ok {
		return fmt.Errorf("Kopia encryption secret %s/%s missing 'password' key", encSecretRef.Namespace, encSecretRef.Name)
	}

	return nil
}

// isMaintenanceDue checks if maintenance should run based on the cron schedule
func (r *BackupRepositoryReconciler) isMaintenanceDue(repo *drv1alpha1.BackupRepository) bool {
	schedule := repo.Spec.MaintenanceSchedule
	if schedule == "" {
		schedule = "0 2 * * *" // default: daily at 2am
	}

	sched, err := cron.ParseStandard(schedule)
	if err != nil {
		logging.LogError(nil, fmt.Sprintf("invalid maintenance schedule for BackupRepository %s: %v", repo.Name, err))
		return false
	}

	var lastMaintenance time.Time
	if repo.Status.LastMaintenanceTime != nil {
		lastMaintenance = repo.Status.LastMaintenanceTime.Time
	} else {
		// Never run maintenance before, it's due
		return true
	}

	// Next scheduled time after last maintenance
	nextDue := sched.Next(lastMaintenance)
	return time.Now().After(nextDue)
}

// updateStatusWithError updates the BackupRepository status to Error state
func (r *BackupRepositoryReconciler) updateStatusWithError(ctx context.Context, repo *drv1alpha1.BackupRepository, operation string, opErr error) (ctrl.Result, error) {
	return r.updateStatusCondition(ctx, repo,
		drv1alpha1.BackupRepositoryStateError,
		"RepositoryHealthy", metav1.ConditionFalse,
		fmt.Sprintf("%sFailed", operation), opErr.Error(),
		defaultRequeueOnError)
}

// updateStatusHealthy updates the BackupRepository status with healthy stats
func (r *BackupRepositoryReconciler) updateStatusHealthy(ctx context.Context, repo *drv1alpha1.BackupRepository, stats *RepositoryStats) (ctrl.Result, error) {
	// Get latest version before updating
	var latest drv1alpha1.BackupRepository
	if err := r.Get(ctx, client.ObjectKeyFromObject(repo), &latest); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	now := metav1.Now()
	latest.Status.State = drv1alpha1.BackupRepositoryStateReady
	latest.Status.LastHealthCheckTime = &now
	latest.Status.Message = "Repository is healthy"

	if stats != nil {
		latest.Status.RepositorySize = stats.RepositorySize
		latest.Status.SnapshotCount = stats.SnapshotCount
	}

	setBackupRepositoryCondition(&latest, "RepositoryHealthy", metav1.ConditionTrue, "HealthCheckPassed", "Repository health check passed")

	if err := r.Status().Update(ctx, &latest); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		logging.LogError(nil, fmt.Sprintf("failed to update BackupRepository %s status: %v", repo.Name, err))
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: defaultHealthCheckInterval}, nil
}

// updateStatusCondition updates state, sets a condition, and returns a result with the given requeue time
func (r *BackupRepositoryReconciler) updateStatusCondition(
	ctx context.Context,
	repo *drv1alpha1.BackupRepository,
	state drv1alpha1.BackupRepositoryState,
	conditionType string,
	conditionStatus metav1.ConditionStatus,
	reason, message string,
	requeueAfter time.Duration,
) (ctrl.Result, error) {
	// Get latest version before updating
	var latest drv1alpha1.BackupRepository
	if err := r.Get(ctx, client.ObjectKeyFromObject(repo), &latest); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	latest.Status.State = state
	latest.Status.Message = message
	setBackupRepositoryCondition(&latest, conditionType, conditionStatus, reason, message)

	if err := r.Status().Update(ctx, &latest); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		logging.LogError(nil, fmt.Sprintf("failed to update BackupRepository %s status: %v", repo.Name, err))
		return ctrl.Result{}, err
	}

	if requeueAfter > 0 {
		return ctrl.Result{RequeueAfter: requeueAfter}, nil
	}
	return ctrl.Result{}, nil
}

// updateStatusAfterMaintenance updates status after successful maintenance and transitions to Ready
func (r *BackupRepositoryReconciler) updateStatusAfterMaintenance(ctx context.Context, repo *drv1alpha1.BackupRepository, maintenanceTime *metav1.Time) (ctrl.Result, error) {
	// Get latest version before updating
	var latest drv1alpha1.BackupRepository
	if err := r.Get(ctx, client.ObjectKeyFromObject(repo), &latest); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	latest.Status.State = drv1alpha1.BackupRepositoryStateReady
	latest.Status.LastMaintenanceTime = maintenanceTime
	latest.Status.Message = "Repository is healthy"
	setBackupRepositoryCondition(&latest, "RepositoryHealthy", metav1.ConditionTrue, "MaintenanceCompleted", "Repository maintenance completed successfully")

	if err := r.Status().Update(ctx, &latest); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		logging.LogError(nil, fmt.Sprintf("failed to update BackupRepository %s status: %v", repo.Name, err))
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: defaultHealthCheckInterval}, nil
}

// setBackupRepositoryCondition sets a condition on the BackupRepository status
func setBackupRepositoryCondition(repo *drv1alpha1.BackupRepository, conditionType string, status metav1.ConditionStatus, reason, message string) {
	now := metav1.Now()
	for i := range repo.Status.Conditions {
		if repo.Status.Conditions[i].Type == conditionType {
			if repo.Status.Conditions[i].Status != status ||
				repo.Status.Conditions[i].Reason != reason ||
				repo.Status.Conditions[i].Message != message {
				repo.Status.Conditions[i].Status = status
				repo.Status.Conditions[i].Reason = reason
				repo.Status.Conditions[i].Message = message
				repo.Status.Conditions[i].LastTransitionTime = now
			}
			return
		}
	}

	// Condition not found, add new one
	repo.Status.Conditions = append(repo.Status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
	})
}
