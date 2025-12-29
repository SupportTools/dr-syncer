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
)

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
	m.log.WithFields(logrus.Fields{
		"namespace": namespace,
		"pvc":       pvcName,
		"waiting":   waitingNow,
	}).Debug("Waiting for concurrency slot")

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
