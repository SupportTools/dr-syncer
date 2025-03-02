package replication

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// LockAnnotation is the annotation used to indicate a PVC is being synced
	LockAnnotation = "dr-syncer.io/replication-lock"
	
	// DefaultLockTimeout is the default timeout for a lock (in minutes)
	DefaultLockTimeout = 60
)

// LockInfo contains information about a PVC replication lock
type LockInfo struct {
	// ControllerPodName is the name of the controller pod that created the lock
	ControllerPodName string
	
	// Timestamp is when the lock was created
	Timestamp time.Time
}

// ParseLockInfo parses a lock annotation value into a LockInfo struct
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

// FormatLockInfo formats a LockInfo struct into a lock annotation value
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

// GetLockTimeout gets the lock timeout in minutes
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

// AcquirePVCLock attempts to acquire a lock on a PVC for replication
func (p *PVCSyncer) AcquirePVCLock(ctx context.Context, namespace, pvcName string) (bool, *LockInfo, error) {
	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc_name":  pvcName,
	}).Info("[DR-SYNC-DETAIL] Attempting to acquire lock on PVC for replication")
	
	// Get current controller pod name
	controllerPodName := GetCurrentControllerPodName()
	
	// Get lock timeout
	lockTimeout := GetLockTimeout()
	
	// Get the PVC
	pvc, err := p.SourceK8sClient.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		return false, nil, fmt.Errorf("failed to get PVC: %v", err)
	}
	
	// Check if the PVC already has a lock annotation
	if lockValue, exists := pvc.Annotations[LockAnnotation]; exists {
		// Parse lock info
		lockInfo, err := ParseLockInfo(lockValue)
		if err != nil {
			log.WithFields(logrus.Fields{
				"namespace":  namespace,
				"pvc_name":   pvcName,
				"lock_value": lockValue,
				"error":      err,
			}).Warn("[DR-SYNC-DETAIL] Failed to parse existing lock, will override")
			// If we can't parse the lock, we'll treat it as invalid and override it
		} else {
			// If the lock is owned by this controller, we can proceed
			if lockInfo.ControllerPodName == controllerPodName {
				log.WithFields(logrus.Fields{
					"namespace":     namespace,
					"pvc_name":      pvcName,
					"controller":    controllerPodName,
					"lock_timestamp": lockInfo.Timestamp,
				}).Info("[DR-SYNC-DETAIL] Lock already owned by this controller, proceeding")
				return true, lockInfo, nil
			}
			
			// Check if the lock has expired
			if time.Since(lockInfo.Timestamp) > lockTimeout {
				log.WithFields(logrus.Fields{
					"namespace":      namespace,
					"pvc_name":       pvcName,
					"old_controller": lockInfo.ControllerPodName,
					"new_controller": controllerPodName,
					"lock_age":       time.Since(lockInfo.Timestamp),
					"timeout":        lockTimeout,
				}).Info("[DR-SYNC-DETAIL] Found stale lock, taking over")
				
				// Lock has expired, we'll break it
				breakingInfo := &LockInfo{
					ControllerPodName: controllerPodName,
					Timestamp:         time.Now().UTC(),
				}
				
				// Update the PVC with our lock
				return p.updatePVCLock(ctx, namespace, pvcName, breakingInfo)
			}
			
			// Lock is still valid and owned by another controller
			log.WithFields(logrus.Fields{
				"namespace":      namespace,
				"pvc_name":       pvcName,
				"controller":     lockInfo.ControllerPodName,
				"lock_timestamp": lockInfo.Timestamp,
				"lock_age":       time.Since(lockInfo.Timestamp),
				"timeout":        lockTimeout,
			}).Info("[DR-SYNC-DETAIL] PVC is locked by another controller, skipping")
			
			return false, lockInfo, nil
		}
	}
	
	// No lock exists or the lock is invalid, create a new one
	newLockInfo := &LockInfo{
		ControllerPodName: controllerPodName,
		Timestamp:         time.Now().UTC(),
	}
	
	// Update the PVC with our lock
	acquired, lockInfo, err := p.updatePVCLock(ctx, namespace, pvcName, newLockInfo)
	if err != nil {
		return false, nil, err
	}
	
	if acquired {
		log.WithFields(logrus.Fields{
			"namespace":  namespace,
			"pvc_name":   pvcName,
			"controller": controllerPodName,
		}).Info("[DR-SYNC-DETAIL] Successfully acquired lock on PVC")
	}
	
	return acquired, lockInfo, nil
}

// updatePVCLock updates the lock annotation on a PVC
func (p *PVCSyncer) updatePVCLock(ctx context.Context, namespace, pvcName string, lockInfo *LockInfo) (bool, *LockInfo, error) {
	// Get the PVC
	pvc, err := p.SourceK8sClient.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		return false, nil, fmt.Errorf("failed to get PVC: %v", err)
	}
	
	// Create a copy of the PVC to update
	pvcCopy := pvc.DeepCopy()
	
	// Ensure annotations map exists
	if pvcCopy.Annotations == nil {
		pvcCopy.Annotations = make(map[string]string)
	}
	
	// Set the lock annotation
	lockValue := FormatLockInfo(lockInfo)
	pvcCopy.Annotations[LockAnnotation] = lockValue
	
	// Update the PVC
	updatedPVC, err := p.SourceK8sClient.CoreV1().PersistentVolumeClaims(namespace).Update(ctx, pvcCopy, metav1.UpdateOptions{})
	if err != nil {
		if errors.IsConflict(err) {
			log.WithFields(logrus.Fields{
				"namespace": namespace,
				"pvc_name":  pvcName,
				"error":     err,
			}).Warn("[DR-SYNC-DETAIL] Conflict while updating PVC lock, another process may have modified the PVC")
			return false, nil, nil
		}
		return false, nil, fmt.Errorf("failed to update PVC with lock: %v", err)
	}
	
	// Parse the lock info from the updated PVC to ensure consistency
	if updatedLockValue, exists := updatedPVC.Annotations[LockAnnotation]; exists {
		updatedLockInfo, err := ParseLockInfo(updatedLockValue)
		if err != nil {
			log.WithFields(logrus.Fields{
				"namespace":  namespace,
				"pvc_name":   pvcName,
				"lock_value": updatedLockValue,
				"error":      err,
			}).Warn("[DR-SYNC-DETAIL] Failed to parse updated lock value")
			return true, lockInfo, nil
		}
		
		// If the updated lock has a different controller, we didn't get the lock
		if updatedLockInfo.ControllerPodName != lockInfo.ControllerPodName {
			log.WithFields(logrus.Fields{
				"namespace":        namespace,
				"pvc_name":         pvcName,
				"our_controller":   lockInfo.ControllerPodName,
				"their_controller": updatedLockInfo.ControllerPodName,
			}).Warn("[DR-SYNC-DETAIL] Lock was acquired by another controller")
			return false, updatedLockInfo, nil
		}
		
		return true, updatedLockInfo, nil
	}
	
	// If the lock annotation is missing, something went wrong
	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc_name":  pvcName,
	}).Warn("[DR-SYNC-DETAIL] Lock annotation is missing after update")
	return false, nil, nil
}

// ReleasePVCLock releases a lock on a PVC after replication is complete
func (p *PVCSyncer) ReleasePVCLock(ctx context.Context, namespace, pvcName string) error {
	log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc_name":  pvcName,
	}).Info("[DR-SYNC-DETAIL] Releasing lock on PVC")
	
	// Get current controller pod name
	controllerPodName := GetCurrentControllerPodName()
	
	// Get the PVC
	pvc, err := p.SourceK8sClient.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// PVC no longer exists, lock is effectively released
			log.WithFields(logrus.Fields{
				"namespace": namespace,
				"pvc_name":  pvcName,
			}).Info("[DR-SYNC-DETAIL] PVC no longer exists, lock is released")
			return nil
		}
		return fmt.Errorf("failed to get PVC: %v", err)
	}
	
	// Check if the PVC has a lock annotation
	if lockValue, exists := pvc.Annotations[LockAnnotation]; exists {
		// Parse lock info
		lockInfo, err := ParseLockInfo(lockValue)
		if err != nil {
			log.WithFields(logrus.Fields{
				"namespace":  namespace,
				"pvc_name":   pvcName,
				"lock_value": lockValue,
				"error":      err,
			}).Warn("[DR-SYNC-DETAIL] Failed to parse existing lock, removing anyway")
		} else if lockInfo.ControllerPodName != controllerPodName {
			// Lock is owned by another controller, don't release it
			log.WithFields(logrus.Fields{
				"namespace":      namespace,
				"pvc_name":       pvcName,
				"our_controller": controllerPodName,
				"lock_owner":     lockInfo.ControllerPodName,
			}).Warn("[DR-SYNC-DETAIL] Lock is owned by another controller, not releasing")
			return nil
		}
		
		// Create a copy of the PVC to update
		pvcCopy := pvc.DeepCopy()
		
		// Delete the lock annotation
		delete(pvcCopy.Annotations, LockAnnotation)
		
		// Update the PVC
		_, err = p.SourceK8sClient.CoreV1().PersistentVolumeClaims(namespace).Update(ctx, pvcCopy, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to remove lock annotation: %v", err)
		}
		
		log.WithFields(logrus.Fields{
			"namespace":  namespace,
			"pvc_name":   pvcName,
			"controller": controllerPodName,
		}).Info("[DR-SYNC-DETAIL] Successfully released lock on PVC")
	} else {
		// No lock exists
		log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc_name":  pvcName,
		}).Info("[DR-SYNC-DETAIL] No lock found on PVC")
	}
	
	return nil
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
								"deployment":   deployment.Name,
								"namespace":    deployment.Namespace,
								"pvc_name":     pvcName,
								"lock_owner":   lockInfo.ControllerPodName,
								"lock_age":     time.Since(lockInfo.Timestamp),
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
