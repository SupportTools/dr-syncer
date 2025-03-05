package util

import (
	"sync"
)

// WorkerPool manages a pool of workers for concurrent operations
type WorkerPool struct {
	maxWorkers int
	semaphore  chan struct{}
}

// NewWorkerPool creates a new worker pool with the specified number of workers
func NewWorkerPool(maxWorkers int) *WorkerPool {
	return &WorkerPool{
		maxWorkers: maxWorkers,
		semaphore:  make(chan struct{}, maxWorkers),
	}
}

// Submit submits a task to the worker pool
func (wp *WorkerPool) Submit(task func()) {
	wp.semaphore <- struct{}{} // Acquire a slot

	go func() {
		defer func() { <-wp.semaphore }() // Release the slot when done
		task()
	}()
}

// SubmitAndWait submits multiple tasks to the worker pool and waits for them to complete
func (wp *WorkerPool) SubmitAndWait(tasks []func()) {
	var wg sync.WaitGroup
	wg.Add(len(tasks))

	for _, task := range tasks {
		taskFn := task // Capture the task function
		wp.Submit(func() {
			defer wg.Done()
			taskFn()
		})
	}

	wg.Wait()
}
