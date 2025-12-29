package replication

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/supporttools/dr-syncer/pkg/logging"
	coordinationv1 "k8s.io/api/coordination/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

const (
	// LockAnnotation is the annotation used to indicate a PVC is being synced (legacy)
	LockAnnotation = "dr-syncer.io/replication-lock"

	// DefaultLockTimeout is the default timeout for a lock (in minutes) - legacy
	DefaultLockTimeout = 60

	// DefaultLeaseDuration is the default duration for PVC locks
	DefaultLeaseDuration = 1 * time.Hour

	// DefaultHeartbeatInterval is how often to renew the lease
	DefaultHeartbeatInterval = 30 * time.Second

	// LeasePrefix is the prefix for PVC lock lease names
	LeasePrefix = "dr-syncer-pvc-lock-"

	// LeaseLabelKey is the label key for identifying PVC lock leases
	LeaseLabelKey = "dr-syncer.io/lock-type"

	// LeaseLabelValue is the label value for PVC lock leases
	LeaseLabelValue = "pvc-sync"
)

var lockLog = logrus.WithField("component", "pvc-lock")

// LockInfo contains information about a PVC replication lock (legacy annotation-based)
type LockInfo struct {
	// ControllerPodName is the name of the controller pod that created the lock
	ControllerPodName string

	// Timestamp is when the lock was created
	Timestamp time.Time
}

// PVCLockManager manages Lease-based locks for PVC sync operations
type PVCLockManager struct {
	// Client is the Kubernetes client for the controller cluster
	Client kubernetes.Interface

	// Namespace is where Leases are created (typically dr-syncer-system)
	Namespace string

	// HolderIdentity is the identity of the lock holder (typically pod name)
	HolderIdentity string

	// LeaseDuration is how long a lease is valid without renewal
	LeaseDuration time.Duration

	// HeartbeatInterval is how often to renew the lease
	HeartbeatInterval time.Duration

	// log is the logger for this instance
	log *logrus.Entry
}

// PVCLock represents an acquired lock on a PVC
type PVCLock struct {
	// Manager is the lock manager that created this lock
	Manager *PVCLockManager

	// LeaseName is the name of the Lease resource
	LeaseName string

	// PVCNamespace is the namespace of the locked PVC
	PVCNamespace string

	// PVCName is the name of the locked PVC
	PVCName string

	// AcquiredAt is when the lock was acquired
	AcquiredAt time.Time

	// stopCh signals the heartbeat goroutine to stop
	stopCh chan struct{}

	// stopped indicates if the lock has been released
	stopped bool

	// mu protects stopped flag
	mu sync.Mutex

	// log is the logger for this lock
	log *logrus.Entry
}

// PVCLeaseInfo contains information about an existing lock
type PVCLeaseInfo struct {
	HolderIdentity string
	AcquireTime    time.Time
	RenewTime      time.Time
	LeaseDuration  time.Duration
}

// ParseLockInfo parses a lock annotation value into a LockInfo struct (legacy)
func ParseLockInfo(lockValue string) (*LockInfo, error) {
	parts := strings.Split(lockValue, "|")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid lock value format: %s", lockValue)
	}

	timestamp, err := time.Parse(time.RFC3339, parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid timestamp in lock value: %v", err)
	}

	return &LockInfo{
		ControllerPodName: parts[0],
		Timestamp:         timestamp,
	}, nil
}

// FormatLockInfo formats a LockInfo struct into a lock annotation value (legacy)
func FormatLockInfo(info *LockInfo) string {
	return fmt.Sprintf("%s|%s", info.ControllerPodName, info.Timestamp.Format(time.RFC3339))
}

// GetCurrentControllerPodName gets the current controller pod name from environment
func GetCurrentControllerPodName() string {
	// Try to get pod name from environment variable (set by Downward API)
	podName := os.Getenv("POD_NAME")
	if podName != "" {
		return podName
	}

	// Fallback to hostname, which is typically the pod name in Kubernetes
	hostname, err := os.Hostname()
	if err == nil && hostname != "" {
		return hostname
	}

	// If all else fails, generate a unique identifier
	return fmt.Sprintf("dr-syncer-controller-%d", time.Now().UnixNano())
}

// GetLockTimeout gets the lock timeout in minutes (legacy)
func GetLockTimeout() time.Duration {
	// Try to get lock timeout from environment variable
	timeoutStr := os.Getenv("LOCK_TIMEOUT_MINUTES")
	if timeoutStr != "" {
		timeout, err := strconv.Atoi(timeoutStr)
		if err == nil && timeout > 0 {
			return time.Duration(timeout) * time.Minute
		}
	}

	// Fallback to default timeout
	return DefaultLockTimeout * time.Minute
}

// NewPVCLockManager creates a new PVC lock manager
func NewPVCLockManager(client kubernetes.Interface, namespace string) *PVCLockManager {
	holderID := GetCurrentControllerPodName()

	return &PVCLockManager{
		Client:            client,
		Namespace:         namespace,
		HolderIdentity:    holderID,
		LeaseDuration:     DefaultLeaseDuration,
		HeartbeatInterval: DefaultHeartbeatInterval,
		log: lockLog.WithFields(logrus.Fields{
			"namespace": namespace,
			"holder":    holderID,
		}),
	}
}

// WithLeaseDuration sets the lease duration
func (m *PVCLockManager) WithLeaseDuration(d time.Duration) *PVCLockManager {
	if d > 0 {
		m.LeaseDuration = d
	}
	return m
}

// WithHeartbeatInterval sets the heartbeat interval
func (m *PVCLockManager) WithHeartbeatInterval(d time.Duration) *PVCLockManager {
	if d > 0 {
		m.HeartbeatInterval = d
	}
	return m
}

// generateLeaseName creates a deterministic lease name from namespace/pvcname
func (m *PVCLockManager) generateLeaseName(pvcNamespace, pvcName string) string {
	// Create a hash to keep name within 63 char limit
	input := fmt.Sprintf("%s/%s", pvcNamespace, pvcName)
	hash := sha256.Sum256([]byte(input))
	shortHash := hex.EncodeToString(hash[:])[:16]
	return fmt.Sprintf("%s%s", LeasePrefix, shortHash)
}

// AcquireLock attempts to acquire a lock on the specified PVC
// Returns a PVCLock if successful, or an error if the lock is held by another controller
func (m *PVCLockManager) AcquireLock(ctx context.Context, pvcNamespace, pvcName string) (*PVCLock, error) {
	leaseName := m.generateLeaseName(pvcNamespace, pvcName)

	m.log.WithFields(logrus.Fields{
		"pvc_namespace": pvcNamespace,
		"pvc_name":      pvcName,
		"lease_name":    leaseName,
	}).Info(logging.LogTagInfo + " Attempting to acquire PVC lock via Lease")

	now := metav1.NewMicroTime(time.Now())
	leaseDurationSeconds := int32(m.LeaseDuration.Seconds())

	// Build the lease object
	lease := &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      leaseName,
			Namespace: m.Namespace,
			Labels: map[string]string{
				LeaseLabelKey:                LeaseLabelValue,
				"dr-syncer.io/pvc-namespace": pvcNamespace,
				"dr-syncer.io/pvc-name":      pvcName,
			},
			Annotations: map[string]string{
				"dr-syncer.io/pvc-namespace": pvcNamespace,
				"dr-syncer.io/pvc-name":      pvcName,
			},
		},
		Spec: coordinationv1.LeaseSpec{
			HolderIdentity:       ptr.To(m.HolderIdentity),
			LeaseDurationSeconds: ptr.To(leaseDurationSeconds),
			AcquireTime:          &now,
			RenewTime:            &now,
		},
	}

	// Try to create the lease (atomic - fails if exists)
	_, err := m.Client.CoordinationV1().Leases(m.Namespace).Create(ctx, lease, metav1.CreateOptions{})
	if err == nil {
		// Successfully created - we have the lock
		m.log.WithFields(logrus.Fields{
			"pvc_namespace": pvcNamespace,
			"pvc_name":      pvcName,
			"lease_name":    leaseName,
		}).Info(logging.LogTagInfo + " Successfully acquired PVC lock (new lease)")

		return m.newPVCLock(leaseName, pvcNamespace, pvcName), nil
	}

	if !errors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("failed to create lease: %w", err)
	}

	// Lease already exists - check if it's expired or if we already own it
	existing, err := m.Client.CoordinationV1().Leases(m.Namespace).Get(ctx, leaseName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get existing lease: %w", err)
	}

	// Check if we already own this lease
	if existing.Spec.HolderIdentity != nil && *existing.Spec.HolderIdentity == m.HolderIdentity {
		m.log.WithFields(logrus.Fields{
			"pvc_namespace": pvcNamespace,
			"pvc_name":      pvcName,
			"lease_name":    leaseName,
		}).Info(logging.LogTagInfo + " We already own this PVC lock")

		// Update the renew time
		existing.Spec.RenewTime = &now
		_, err = m.Client.CoordinationV1().Leases(m.Namespace).Update(ctx, existing, metav1.UpdateOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to update our own lease: %w", err)
		}

		return m.newPVCLock(leaseName, pvcNamespace, pvcName), nil
	}

	// Check if the lease is expired
	if m.isLeaseExpired(existing) {
		m.log.WithFields(logrus.Fields{
			"pvc_namespace":   pvcNamespace,
			"pvc_name":        pvcName,
			"lease_name":      leaseName,
			"previous_holder": *existing.Spec.HolderIdentity,
		}).Info(logging.LogTagInfo + " Taking over expired PVC lock")

		// Take over the lease
		existing.Spec.HolderIdentity = ptr.To(m.HolderIdentity)
		existing.Spec.AcquireTime = &now
		existing.Spec.RenewTime = &now
		existing.Spec.LeaseDurationSeconds = ptr.To(leaseDurationSeconds)

		_, err = m.Client.CoordinationV1().Leases(m.Namespace).Update(ctx, existing, metav1.UpdateOptions{})
		if err != nil {
			if errors.IsConflict(err) {
				return nil, fmt.Errorf("lock contention: another controller took the lock")
			}
			return nil, fmt.Errorf("failed to take over expired lease: %w", err)
		}

		return m.newPVCLock(leaseName, pvcNamespace, pvcName), nil
	}

	// Lease is held by someone else and not expired
	holder := "unknown"
	if existing.Spec.HolderIdentity != nil {
		holder = *existing.Spec.HolderIdentity
	}

	m.log.WithFields(logrus.Fields{
		"pvc_namespace": pvcNamespace,
		"pvc_name":      pvcName,
		"lease_name":    leaseName,
		"holder":        holder,
	}).Info(logging.LogTagInfo + " PVC is locked by another controller")

	return nil, fmt.Errorf("PVC %s/%s is locked by %s", pvcNamespace, pvcName, holder)
}

// isLeaseExpired checks if a lease has expired
func (m *PVCLockManager) isLeaseExpired(lease *coordinationv1.Lease) bool {
	if lease.Spec.RenewTime == nil || lease.Spec.LeaseDurationSeconds == nil {
		return true
	}

	renewTime := lease.Spec.RenewTime.Time
	duration := time.Duration(*lease.Spec.LeaseDurationSeconds) * time.Second
	expirationTime := renewTime.Add(duration)

	return time.Now().After(expirationTime)
}

// newPVCLock creates a new PVCLock instance
func (m *PVCLockManager) newPVCLock(leaseName, pvcNamespace, pvcName string) *PVCLock {
	return &PVCLock{
		Manager:      m,
		LeaseName:    leaseName,
		PVCNamespace: pvcNamespace,
		PVCName:      pvcName,
		AcquiredAt:   time.Now(),
		stopCh:       make(chan struct{}),
		stopped:      false,
		log: lockLog.WithFields(logrus.Fields{
			"pvc_namespace": pvcNamespace,
			"pvc_name":      pvcName,
			"lease_name":    leaseName,
		}),
	}
}

// GetLockInfo returns information about an existing lock without acquiring it
func (m *PVCLockManager) GetLockInfo(ctx context.Context, pvcNamespace, pvcName string) (*PVCLeaseInfo, error) {
	leaseName := m.generateLeaseName(pvcNamespace, pvcName)

	lease, err := m.Client.CoordinationV1().Leases(m.Namespace).Get(ctx, leaseName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, nil // No lock exists
		}
		return nil, fmt.Errorf("failed to get lease: %w", err)
	}

	info := &PVCLeaseInfo{}
	if lease.Spec.HolderIdentity != nil {
		info.HolderIdentity = *lease.Spec.HolderIdentity
	}
	if lease.Spec.AcquireTime != nil {
		info.AcquireTime = lease.Spec.AcquireTime.Time
	}
	if lease.Spec.RenewTime != nil {
		info.RenewTime = lease.Spec.RenewTime.Time
	}
	if lease.Spec.LeaseDurationSeconds != nil {
		info.LeaseDuration = time.Duration(*lease.Spec.LeaseDurationSeconds) * time.Second
	}

	return info, nil
}

// StartHeartbeat begins a background goroutine that renews the lease periodically
func (l *PVCLock) StartHeartbeat(ctx context.Context) {
	l.mu.Lock()
	if l.stopped {
		l.mu.Unlock()
		return
	}
	l.mu.Unlock()

	l.log.WithField("interval", l.Manager.HeartbeatInterval).Info(logging.LogTagInfo + " Starting lock heartbeat")

	go func() {
		ticker := time.NewTicker(l.Manager.HeartbeatInterval)
		defer ticker.Stop()

		for {
			select {
			case <-l.stopCh:
				l.log.Debug(logging.LogTagDetail + " Heartbeat stopped")
				return
			case <-ctx.Done():
				l.log.Debug(logging.LogTagDetail + " Heartbeat context cancelled")
				return
			case <-ticker.C:
				if err := l.renew(ctx); err != nil {
					l.log.WithError(err).Warn(logging.LogTagWarn + " Failed to renew lock heartbeat")
				}
			}
		}
	}()
}

// renew updates the lease's renewTime
func (l *PVCLock) renew(ctx context.Context) error {
	l.mu.Lock()
	if l.stopped {
		l.mu.Unlock()
		return nil
	}
	l.mu.Unlock()

	lease, err := l.Manager.Client.CoordinationV1().Leases(l.Manager.Namespace).Get(ctx, l.LeaseName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get lease for renewal: %w", err)
	}

	// Verify we still own the lease
	if lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity != l.Manager.HolderIdentity {
		return fmt.Errorf("lease holder changed, we no longer own the lock")
	}

	now := metav1.NewMicroTime(time.Now())
	lease.Spec.RenewTime = &now

	_, err = l.Manager.Client.CoordinationV1().Leases(l.Manager.Namespace).Update(ctx, lease, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update lease renewTime: %w", err)
	}

	l.log.Debug(logging.LogTagDetail + " Lock heartbeat renewed")
	return nil
}

// Release releases the lock by deleting the Lease
func (l *PVCLock) Release(ctx context.Context) error {
	l.mu.Lock()
	if l.stopped {
		l.mu.Unlock()
		l.log.Debug(logging.LogTagDetail + " Lock already released")
		return nil
	}
	l.stopped = true
	close(l.stopCh)
	l.mu.Unlock()

	l.log.Info(logging.LogTagInfo + " Releasing PVC lock")

	// Verify we still own the lease before deleting
	lease, err := l.Manager.Client.CoordinationV1().Leases(l.Manager.Namespace).Get(ctx, l.LeaseName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			l.log.Debug(logging.LogTagDetail + " Lease already deleted")
			return nil
		}
		return fmt.Errorf("failed to get lease for release: %w", err)
	}

	// Only delete if we own it
	if lease.Spec.HolderIdentity != nil && *lease.Spec.HolderIdentity != l.Manager.HolderIdentity {
		l.log.WithField("current_holder", *lease.Spec.HolderIdentity).Warn(logging.LogTagWarn + " Cannot release lock owned by another controller")
		return fmt.Errorf("cannot release lock owned by %s", *lease.Spec.HolderIdentity)
	}

	err = l.Manager.Client.CoordinationV1().Leases(l.Manager.Namespace).Delete(ctx, l.LeaseName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete lease: %w", err)
	}

	l.log.Info(logging.LogTagInfo + " PVC lock released")
	return nil
}

// IsHeld returns true if this lock instance is still held
func (l *PVCLock) IsHeld() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return !l.stopped
}

// HoldDuration returns how long the lock has been held
func (l *PVCLock) HoldDuration() time.Duration {
	return time.Since(l.AcquiredAt)
}

// CleanupOrphanedRsyncDeployments finds and cleans up orphaned rsync deployments
func (p *PVCSyncer) CleanupOrphanedRsyncDeployments(ctx context.Context, k8sClient kubernetes.Interface, namespace string) error {
	log.WithFields(logrus.Fields{
		"namespace": namespace,
	}).Info("[DR-SYNC-DETAIL] Cleaning up orphaned rsync deployments")

	// List deployments with rsync label
	labelSelector := "app.kubernetes.io/name=dr-syncer-rsync"
	deployments, err := k8sClient.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})

	if err != nil {
		return fmt.Errorf("failed to list rsync deployments: %v", err)
	}

	if len(deployments.Items) == 0 {
		log.WithFields(logrus.Fields{
			"namespace": namespace,
		}).Info("[DR-SYNC-DETAIL] No rsync deployments found to clean up")
		return nil
	}

	// Check each deployment to see if it's orphaned
	deletionPropagation := metav1.DeletePropagationForeground
	deleteOptions := metav1.DeleteOptions{
		PropagationPolicy: &deletionPropagation,
	}

	for _, deployment := range deployments.Items {
		// Get the PVC name from the labels
		pvcName, exists := deployment.Labels["dr-syncer.io/pvc-name"]
		if !exists {
			log.WithFields(logrus.Fields{
				"deployment": deployment.Name,
				"namespace":  deployment.Namespace,
			}).Warn("[DR-SYNC-DETAIL] Rsync deployment missing PVC name label, considering for cleanup")
			pvcName = ""
		}

		if pvcName != "" {
			// Check if the PVC exists
			_, err := k8sClient.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
			if err == nil {
				// PVC exists, check if it has a lock
				pvc, err := k8sClient.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
				if err == nil {
					if lockValue, hasLock := pvc.Annotations[LockAnnotation]; hasLock {
						// PVC has a lock, check if it's stale
						lockInfo, err := ParseLockInfo(lockValue)
						if err == nil && time.Since(lockInfo.Timestamp) <= GetLockTimeout() {
							// Lock is valid, skip this deployment
							log.WithFields(logrus.Fields{
								"deployment": deployment.Name,
								"namespace":  deployment.Namespace,
								"pvc_name":   pvcName,
								"lock_owner": lockInfo.ControllerPodName,
								"lock_age":   time.Since(lockInfo.Timestamp),
							}).Info("[DR-SYNC-DETAIL] Deployment has valid lock, skipping cleanup")
							continue
						}
					}
				}
			}
		}

		// If we get here, the deployment is orphaned or the PVC is gone
		log.WithFields(logrus.Fields{
			"deployment": deployment.Name,
			"namespace":  deployment.Namespace,
			"pvc_name":   pvcName,
		}).Info("[DR-SYNC-DETAIL] Cleaning up orphaned rsync deployment")

		if err := k8sClient.AppsV1().Deployments(namespace).Delete(ctx, deployment.Name, deleteOptions); err != nil {
			if !errors.IsNotFound(err) {
				log.WithFields(logrus.Fields{
					"deployment": deployment.Name,
					"namespace":  deployment.Namespace,
					"error":      err,
				}).Warn("[DR-SYNC-DETAIL] Failed to delete orphaned deployment")
				// Continue with other deployments
			}
		}
	}

	log.WithFields(logrus.Fields{
		"namespace": namespace,
	}).Info("[DR-SYNC-DETAIL] Finished cleaning up orphaned rsync deployments")

	return nil
}
