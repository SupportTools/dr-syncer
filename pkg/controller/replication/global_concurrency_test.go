package replication

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestInitBackupConcurrencyManager(t *testing.T) {
	// Reset singleton for test isolation.
	backupManagerMu.Lock()
	backupManager = nil
	backupManagerMu.Unlock()

	m := InitBackupConcurrencyManager(3)
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
	if m.GetLimit() != 3 {
		t.Fatalf("expected limit 3, got %d", m.GetLimit())
	}

	// Same limit returns same instance (no re-creation).
	m2 := InitBackupConcurrencyManager(3)
	if m2 != m {
		t.Fatal("expected same instance for same limit")
	}

	// Different limit creates new instance.
	m3 := InitBackupConcurrencyManager(5)
	if m3 == m {
		t.Fatal("expected new instance for different limit")
	}
	if m3.GetLimit() != 5 {
		t.Fatalf("expected limit 5, got %d", m3.GetLimit())
	}
}

func TestGetBackupConcurrencyManager(t *testing.T) {
	backupManagerMu.Lock()
	backupManager = nil
	backupManagerMu.Unlock()

	// Before init, Get returns nil.
	if got := GetBackupConcurrencyManager(); got != nil {
		t.Fatal("expected nil before init")
	}

	InitBackupConcurrencyManager(3)
	if got := GetBackupConcurrencyManager(); got == nil {
		t.Fatal("expected non-nil after init")
	}
}

func TestBackupConcurrencyManager_AcquireRelease(t *testing.T) {
	backupManagerMu.Lock()
	backupManager = nil
	backupManagerMu.Unlock()

	m := InitBackupConcurrencyManager(2)

	ctx := context.Background()

	// Acquire two slots (should not block).
	if err := m.Acquire(ctx, "ns1", "pvc1"); err != nil {
		t.Fatalf("acquire 1: %v", err)
	}
	if err := m.Acquire(ctx, "ns1", "pvc2"); err != nil {
		t.Fatalf("acquire 2: %v", err)
	}

	active, waiting, limit := m.GetStats()
	if active != 2 || waiting != 0 || limit != 2 {
		t.Fatalf("expected active=2 waiting=0 limit=2, got active=%d waiting=%d limit=%d", active, waiting, limit)
	}

	// Release one slot.
	m.Release("ns1", "pvc1")

	active, _, _ = m.GetStats()
	if active != 1 {
		t.Fatalf("expected active=1 after release, got %d", active)
	}

	// Release second slot.
	m.Release("ns1", "pvc2")

	active, _, _ = m.GetStats()
	if active != 0 {
		t.Fatalf("expected active=0 after all releases, got %d", active)
	}
}

func TestBackupConcurrencyManager_BlocksAtLimit(t *testing.T) {
	backupManagerMu.Lock()
	backupManager = nil
	backupManagerMu.Unlock()

	m := InitBackupConcurrencyManager(1)

	ctx := context.Background()

	// Fill the single slot.
	if err := m.Acquire(ctx, "ns1", "pvc1"); err != nil {
		t.Fatalf("acquire: %v", err)
	}

	// Second acquire should block until released or cancelled.
	ctx2, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := m.Acquire(ctx2, "ns1", "pvc2")
	if err == nil {
		t.Fatal("expected error from blocked acquire with timeout")
	}

	// Release and verify we can acquire again.
	m.Release("ns1", "pvc1")

	if err := m.Acquire(context.Background(), "ns1", "pvc2"); err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	m.Release("ns1", "pvc2")
}

func TestBackupConcurrencyManager_ContextCancellation(t *testing.T) {
	backupManagerMu.Lock()
	backupManager = nil
	backupManagerMu.Unlock()

	m := InitBackupConcurrencyManager(1)

	// Fill the slot.
	if err := m.Acquire(context.Background(), "ns1", "pvc1"); err != nil {
		t.Fatalf("acquire: %v", err)
	}

	// Cancel context before acquiring.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := m.Acquire(ctx, "ns1", "pvc2")
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}

	m.Release("ns1", "pvc1")
}

func TestBackupSemaphoreIndependentFromRsync(t *testing.T) {
	// Reset both singletons.
	globalManagerMu.Lock()
	globalManager = nil
	globalManagerMu.Unlock()
	backupManagerMu.Lock()
	backupManager = nil
	backupManagerMu.Unlock()

	rsyncMgr := InitGlobalConcurrencyManager(1)
	backupMgr := InitBackupConcurrencyManager(1)

	ctx := context.Background()

	// Fill rsync slot.
	if err := rsyncMgr.Acquire(ctx, "ns1", "pvc1"); err != nil {
		t.Fatalf("rsync acquire: %v", err)
	}

	// Backup slot should still be available (independent pool).
	if err := backupMgr.Acquire(ctx, "ns1", "pvc1"); err != nil {
		t.Fatalf("backup acquire should succeed independently: %v", err)
	}

	rsyncMgr.Release("ns1", "pvc1")
	backupMgr.Release("ns1", "pvc1")
}

func TestBackupConcurrencyManager_ConcurrentAccess(t *testing.T) {
	backupManagerMu.Lock()
	backupManager = nil
	backupManagerMu.Unlock()

	m := InitBackupConcurrencyManager(3)

	ctx := context.Background()
	var wg sync.WaitGroup
	var maxConcurrent int64

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := m.Acquire(ctx, "ns1", "pvc"); err != nil {
				t.Errorf("acquire %d: %v", i, err)
				return
			}

			active, _, _ := m.GetStats()
			if cur := atomic.LoadInt64(&maxConcurrent); active > cur {
				atomic.CompareAndSwapInt64(&maxConcurrent, cur, active)
			}

			time.Sleep(10 * time.Millisecond)
			m.Release("ns1", "pvc")
		}(i)
	}

	wg.Wait()

	// Max concurrent should never exceed limit.
	if maxConcurrent > 3 {
		t.Fatalf("max concurrent %d exceeded limit of 3", maxConcurrent)
	}
}
