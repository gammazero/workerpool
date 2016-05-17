package workerpool

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

const max = 10

func TestMaxWorkers(t *testing.T) {
	t.Parallel()

	var workers int32
	wp := New(max)
	defer wp.Stop()

	sync := make(chan struct{})

	// Start tasks with enough time apart to allow worker to finish with
	// previous task and be reused for next.
	for i := 0; i < max; i++ {
		wp.Submit(func() {
			atomic.AddInt32(&workers, 1)
			<-sync
		})
	}

	// Release workers.
	close(sync)

	// Wait for all enqueued tasks to be dispatched to workers.
	for i := 0; i < 10; i++ {
		time.Sleep(100 * time.Millisecond)
		if atomic.LoadInt32(&workers) == max {
			break
		}
		runtime.Gosched()
	}

	if atomic.LoadInt32(&workers) != max {
		t.Fatal("Did not get to maximum number of go routines")
	}
}

func TestReuseWorkers(t *testing.T) {
	t.Parallel()

	wp := New(5)
	wpImpl := wp.(*workerPool)
	defer wp.Stop()

	sync := make(chan struct{})

	// Cause worker to be created, and available for reuse before next task.
	for i := 0; i < 10; i++ {
		wp.Submit(func() { <-sync })
		sync <- struct{}{}
		time.Sleep(100 * time.Millisecond)
	}

	if len(wpImpl.readyWorkers) > 1 {
		t.Fatal("Worker not reused")
	}
}

func TestWorkerTimeout(t *testing.T) {
	t.Parallel()

	wp := New(max)
	wpImpl := wp.(*workerPool)
	defer wp.Stop()

	sync := make(chan struct{})

	// Cause workers to be created.  Workers wait on channel, keeping them busy
	// and causing the worker pool to create more.
	for i := 0; i < max; i++ {
		wp.Submit(func() { <-sync })
	}

	// Release workers.
	close(sync)

	// Wait for all enqueued tasks to be ready again.
	// Wait for all enqueued tasks to be dispatched to workers.
	for i := 0; i < 10; i++ {
		time.Sleep(100 * time.Millisecond)
		if len(wpImpl.readyWorkers) == max {
			break
		}
		runtime.Gosched()
	}

	startCount := len(wpImpl.readyWorkers)
	if startCount != max {
		t.Fatal("Wrong number of ready workers")
	}

	// Check that a worker timed out.
	time.Sleep((idleTimeoutSec + 1) * time.Second)
	if len(wpImpl.readyWorkers) != startCount-1 {
		t.Fatal("First worker did not timeout")
	}

	// Check that another worker timed out.
	time.Sleep((idleTimeoutSec + 1) * time.Second)
	if len(wpImpl.readyWorkers) != startCount-2 {
		t.Fatal("Second worker did not timeout")
	}
}
