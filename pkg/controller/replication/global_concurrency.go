package replication

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/sync/semaphore"
)

// GlobalConcurrencyManager manages cluster-wide PVC sync concurrency
type GlobalConcurrencyManager struct {
	semaphore    *semaphore.Weighted
	maxWeight    int64
	mu           sync.RWMutex
	activeCount  int64
	waitingCount int64
	log          *logrus.Entry
}

var (
	globalManager   *GlobalConcurrencyManager
	globalManagerMu sync.RWMutex

	backupManager   *BackupConcurrencyManager
	backupManagerMu sync.RWMutex
)

// BackupConcurrencyManager manages cluster-wide backup operation concurrency.
// It uses a separate semaphore pool from the rsync GlobalConcurrencyManager
// so that backup and rsync operations do not compete for slots.
type BackupConcurrencyManager struct {
	semaphore    *semaphore.Weighted
	maxWeight    int64
	mu           sync.RWMutex
	activeCount  int64
	waitingCount int64
	log          *logrus.Entry
}

// GetGlobalConcurrencyManager returns the singleton manager instance
func GetGlobalConcurrencyManager() *GlobalConcurrencyManager {
	globalManagerMu.RLock()
	defer globalManagerMu.RUnlock()
	return globalManager
}

// InitGlobalConcurrencyManager initializes or updates the global manager with the specified limit
func InitGlobalConcurrencyManager(limit int64) *GlobalConcurrencyManager {
	globalManagerMu.Lock()
	defer globalManagerMu.Unlock()

	// If manager exists with same limit, return it
	if globalManager != nil && globalManager.maxWeight == limit {
		return globalManager
	}

	// Log if we're creating or updating the manager
	logger := logrus.WithField("component", "global-concurrency")
	if globalManager == nil {
		logger.WithField("limit", limit).Info("Initializing global PVC sync concurrency manager")
	} else {
		logger.WithFields(logrus.Fields{
			"old_limit": globalManager.maxWeight,
			"new_limit": limit,
		}).Info("Updating global PVC sync concurrency limit")
	}

	globalManager = &GlobalConcurrencyManager{
		semaphore: semaphore.NewWeighted(limit),
		maxWeight: limit,
		log:       logger,
	}

	// Initialize metrics
	PVCSyncConcurrentCount.Set(0)
	PVCSyncQueueDepth.Set(0)

	return globalManager
}

// Acquire attempts to acquire a slot for PVC sync, blocking until available or context cancelled
func (m *GlobalConcurrencyManager) Acquire(ctx context.Context, namespace, pvcName string) error {
	m.mu.Lock()
	m.waitingCount++
	waitingNow := m.waitingCount
	m.mu.Unlock()

	PVCSyncQueueDepth.Set(float64(waitingNow))

	startWait := time.Now()
	logEntry := m.log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc":       pvcName,
		"waiting":   waitingNow,
	})
	if waitingNow > 1 {
		logEntry.Info("Waiting for concurrency slot (queued)")
	} else {
		logEntry.Debug("Waiting for concurrency slot")
	}

	err := m.semaphore.Acquire(ctx, 1)

	m.mu.Lock()
	m.waitingCount--
	if err == nil {
		m.activeCount++
	}
	activeNow := m.activeCount
	waitingNow = m.waitingCount
	m.mu.Unlock()

	PVCSyncQueueDepth.Set(float64(waitingNow))
	PVCSyncConcurrentCount.Set(float64(activeNow))

	if err == nil {
		waitDuration := time.Since(startWait)
		PVCSyncQueueWaitDuration.Observe(waitDuration.Seconds())
		m.log.WithFields(logrus.Fields{
			"namespace":     namespace,
			"pvc":           pvcName,
			"wait_duration": waitDuration,
			"active":        activeNow,
			"waiting":       waitingNow,
		}).Debug("Acquired concurrency slot")
	} else {
		m.log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc":       pvcName,
			"error":     err,
		}).Debug("Failed to acquire concurrency slot")
	}

	return err
}

// Release releases a concurrency slot after PVC sync completes
func (m *GlobalConcurrencyManager) Release(namespace, pvcName string) {
	m.semaphore.Release(1)

	m.mu.Lock()
	m.activeCount--
	activeNow := m.activeCount
	waitingNow := m.waitingCount
	m.mu.Unlock()

	PVCSyncConcurrentCount.Set(float64(activeNow))

	m.log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc":       pvcName,
		"active":    activeNow,
		"waiting":   waitingNow,
	}).Debug("Released concurrency slot")
}

// GetStats returns current concurrency statistics
func (m *GlobalConcurrencyManager) GetStats() (active, waiting, limit int64) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activeCount, m.waitingCount, m.maxWeight
}

// GetLimit returns the current concurrency limit
func (m *GlobalConcurrencyManager) GetLimit() int64 {
	return m.maxWeight
}

// GetBackupConcurrencyManager returns the singleton backup manager instance
func GetBackupConcurrencyManager() *BackupConcurrencyManager {
	backupManagerMu.RLock()
	defer backupManagerMu.RUnlock()
	return backupManager
}

// InitBackupConcurrencyManager initializes or updates the backup concurrency manager with the specified limit.
// The default limit is 3 if not explicitly configured.
func InitBackupConcurrencyManager(limit int64) *BackupConcurrencyManager {
	backupManagerMu.Lock()
	defer backupManagerMu.Unlock()

	if backupManager != nil && backupManager.maxWeight == limit {
		return backupManager
	}

	logger := logrus.WithField("component", "backup-concurrency")
	if backupManager == nil {
		logger.WithField("limit", limit).Info("Initializing backup concurrency manager")
	} else {
		logger.WithFields(logrus.Fields{
			"old_limit": backupManager.maxWeight,
			"new_limit": limit,
		}).Info("Updating backup concurrency limit")
	}

	backupManager = &BackupConcurrencyManager{
		semaphore: semaphore.NewWeighted(limit),
		maxWeight: limit,
		log:       logger,
	}

	BackupConcurrentCount.Set(0)
	BackupQueueDepth.Set(0)

	return backupManager
}

// Acquire attempts to acquire a slot for a backup operation, blocking until available or context cancelled.
func (m *BackupConcurrencyManager) Acquire(ctx context.Context, namespace, pvcName string) error {
	m.mu.Lock()
	m.waitingCount++
	waitingNow := m.waitingCount
	m.mu.Unlock()

	BackupQueueDepth.Set(float64(waitingNow))

	startWait := time.Now()
	logEntry := m.log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc":       pvcName,
		"waiting":   waitingNow,
	})
	if waitingNow > 1 {
		logEntry.Info("Waiting for backup concurrency slot (queued)")
	} else {
		logEntry.Debug("Waiting for backup concurrency slot")
	}

	err := m.semaphore.Acquire(ctx, 1)

	m.mu.Lock()
	m.waitingCount--
	if err == nil {
		m.activeCount++
	}
	activeNow := m.activeCount
	waitingNow = m.waitingCount
	m.mu.Unlock()

	BackupQueueDepth.Set(float64(waitingNow))
	BackupConcurrentCount.Set(float64(activeNow))

	if err == nil {
		waitDuration := time.Since(startWait)
		BackupQueueWaitDuration.Observe(waitDuration.Seconds())
		m.log.WithFields(logrus.Fields{
			"namespace":     namespace,
			"pvc":           pvcName,
			"wait_duration": waitDuration,
			"active":        activeNow,
			"waiting":       waitingNow,
		}).Debug("Acquired backup concurrency slot")
	} else {
		m.log.WithFields(logrus.Fields{
			"namespace": namespace,
			"pvc":       pvcName,
			"error":     err,
		}).Debug("Failed to acquire backup concurrency slot")
	}

	return err
}

// Release releases a backup concurrency slot after a backup operation completes.
func (m *BackupConcurrencyManager) Release(namespace, pvcName string) {
	m.semaphore.Release(1)

	m.mu.Lock()
	m.activeCount--
	activeNow := m.activeCount
	waitingNow := m.waitingCount
	m.mu.Unlock()

	BackupConcurrentCount.Set(float64(activeNow))

	m.log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc":       pvcName,
		"active":    activeNow,
		"waiting":   waitingNow,
	}).Debug("Released backup concurrency slot")
}

// GetStats returns current backup concurrency statistics.
func (m *BackupConcurrencyManager) GetStats() (active, waiting, limit int64) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activeCount, m.waitingCount, m.maxWeight
}

// GetLimit returns the current backup concurrency limit.
func (m *BackupConcurrencyManager) GetLimit() int64 {
	return m.maxWeight
}
