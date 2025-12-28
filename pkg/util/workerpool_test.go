package util

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewWorkerPool(t *testing.T) {
	pool := NewWorkerPool(5)

	assert.NotNil(t, pool, "Pool should not be nil")
	assert.Equal(t, 5, pool.maxWorkers, "maxWorkers should be 5")
	assert.NotNil(t, pool.semaphore, "Semaphore channel should not be nil")
	assert.Equal(t, 5, cap(pool.semaphore), "Semaphore should have capacity of 5")
}

func TestNewWorkerPool_DifferentSizes(t *testing.T) {
	testCases := []int{1, 2, 10, 100}

	for _, size := range testCases {
		pool := NewWorkerPool(size)
		assert.Equal(t, size, pool.maxWorkers)
		assert.Equal(t, size, cap(pool.semaphore))
	}
}

func TestWorkerPool_Submit_ExecutesTask(t *testing.T) {
	pool := NewWorkerPool(2)

	var executed atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)

	pool.Submit(func() {
		executed.Store(true)
		wg.Done()
	})

	wg.Wait()
	assert.True(t, executed.Load(), "Task should have been executed")
}

func TestWorkerPool_Submit_MultipleTasks(t *testing.T) {
	pool := NewWorkerPool(4)

	var counter atomic.Int32
	var wg sync.WaitGroup
	taskCount := 10
	wg.Add(taskCount)

	for i := 0; i < taskCount; i++ {
		pool.Submit(func() {
			counter.Add(1)
			wg.Done()
		})
	}

	wg.Wait()
	assert.Equal(t, int32(taskCount), counter.Load(), "All tasks should have been executed")
}

func TestWorkerPool_Submit_ConcurrencyLimit(t *testing.T) {
	maxWorkers := 3
	pool := NewWorkerPool(maxWorkers)

	var concurrent atomic.Int32
	var maxConcurrent atomic.Int32
	var wg sync.WaitGroup
	taskCount := 20
	wg.Add(taskCount)

	for i := 0; i < taskCount; i++ {
		pool.Submit(func() {
			defer wg.Done()

			// Increment concurrent counter
			current := concurrent.Add(1)

			// Track maximum concurrent executions
			for {
				max := maxConcurrent.Load()
				if current <= max || maxConcurrent.CompareAndSwap(max, current) {
					break
				}
			}

			// Simulate work
			time.Sleep(10 * time.Millisecond)

			// Decrement concurrent counter
			concurrent.Add(-1)
		})
	}

	wg.Wait()

	// Max concurrent should not exceed pool size
	assert.LessOrEqual(t, maxConcurrent.Load(), int32(maxWorkers),
		"Maximum concurrent tasks should not exceed pool size")
}

func TestWorkerPool_SubmitAndWait_ExecutesAllTasks(t *testing.T) {
	pool := NewWorkerPool(4)

	var results []int
	var mu sync.Mutex

	tasks := make([]func(), 5)
	for i := 0; i < 5; i++ {
		idx := i
		tasks[i] = func() {
			mu.Lock()
			results = append(results, idx)
			mu.Unlock()
		}
	}

	pool.SubmitAndWait(tasks)

	// All tasks should have executed
	assert.Len(t, results, 5, "All 5 tasks should have executed")
}

func TestWorkerPool_SubmitAndWait_WaitsForCompletion(t *testing.T) {
	pool := NewWorkerPool(2)

	var completed atomic.Bool

	tasks := []func(){
		func() {
			time.Sleep(50 * time.Millisecond)
			completed.Store(true)
		},
	}

	pool.SubmitAndWait(tasks)

	// After SubmitAndWait returns, task should be complete
	assert.True(t, completed.Load(), "Task should be complete when SubmitAndWait returns")
}

func TestWorkerPool_SubmitAndWait_EmptyTasks(t *testing.T) {
	pool := NewWorkerPool(4)

	// Should not panic or block with empty task list
	tasks := []func(){}
	pool.SubmitAndWait(tasks)
}

func TestWorkerPool_SubmitAndWait_ConcurrentExecution(t *testing.T) {
	pool := NewWorkerPool(4)

	var startTimes []time.Time
	var mu sync.Mutex

	tasks := make([]func(), 4)
	for i := 0; i < 4; i++ {
		tasks[i] = func() {
			mu.Lock()
			startTimes = append(startTimes, time.Now())
			mu.Unlock()
			time.Sleep(50 * time.Millisecond)
		}
	}

	start := time.Now()
	pool.SubmitAndWait(tasks)
	duration := time.Since(start)

	// With 4 workers and 4 tasks, they should run concurrently
	// Total time should be ~50ms, not ~200ms (4 * 50ms)
	assert.Less(t, duration, 150*time.Millisecond,
		"Tasks should run concurrently, not sequentially")
}

func TestWorkerPool_SubmitAndWait_MoreTasksThanWorkers(t *testing.T) {
	pool := NewWorkerPool(2)

	var counter atomic.Int32
	taskCount := 10

	tasks := make([]func(), taskCount)
	for i := 0; i < taskCount; i++ {
		tasks[i] = func() {
			counter.Add(1)
			time.Sleep(5 * time.Millisecond)
		}
	}

	pool.SubmitAndWait(tasks)

	assert.Equal(t, int32(taskCount), counter.Load(),
		"All tasks should complete even when more tasks than workers")
}

func TestWorkerPool_SingleWorker(t *testing.T) {
	// Test with single worker (sequential execution)
	pool := NewWorkerPool(1)

	var sequence []int
	var mu sync.Mutex

	tasks := make([]func(), 3)
	for i := 0; i < 3; i++ {
		idx := i
		tasks[i] = func() {
			mu.Lock()
			sequence = append(sequence, idx)
			mu.Unlock()
			time.Sleep(10 * time.Millisecond)
		}
	}

	pool.SubmitAndWait(tasks)

	assert.Len(t, sequence, 3, "All tasks should have executed")
}

func TestWorkerPool_StressTest(t *testing.T) {
	pool := NewWorkerPool(10)

	var counter atomic.Int32
	taskCount := 1000

	tasks := make([]func(), taskCount)
	for i := 0; i < taskCount; i++ {
		tasks[i] = func() {
			counter.Add(1)
		}
	}

	pool.SubmitAndWait(tasks)

	require.Equal(t, int32(taskCount), counter.Load(),
		"All 1000 tasks should complete successfully")
}
