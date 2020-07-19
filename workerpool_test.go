package workerpool

import (
	"sync"
	"testing"
	"time"
)

const max = 20

func TestExample(t *testing.T) {
	wp := New(2)
	requests := []string{"alpha", "beta", "gamma", "delta", "epsilon"}

	rspChan := make(chan string, len(requests))
	for _, r := range requests {
		r := r
		wp.Submit(func() {
			rspChan <- r
		})
	}

	wp.StopWait()

	close(rspChan)
	rspSet := map[string]struct{}{}
	for rsp := range rspChan {
		rspSet[rsp] = struct{}{}
	}
	if len(rspSet) < len(requests) {
		t.Fatal("Did not handle all requests")
	}
	for _, req := range requests {
		if _, ok := rspSet[req]; !ok {
			t.Fatal("Missing expected values:", req)
		}
	}
}

func TestMaxWorkers(t *testing.T) {
	t.Parallel()

	wp := New(0)
	if wp.maxWorkers != 1 {
		t.Fatal("should have created one worker")
	}

	wp = New(max)
	defer wp.Stop()

	if wp.Size() != max {
		t.Fatal("wrong size returned")
	}

	started := make(chan struct{}, max)
	release := make(chan struct{})

	// Start workers, and have them all wait on a channel before completing.
	for i := 0; i < max; i++ {
		wp.Submit(func() {
			started <- struct{}{}
			<-release
		})
	}

	// Wait for all queued tasks to be dispatched to workers.
	if wp.waitingQueue.Len() != wp.WaitingQueueSize() {
		t.Fatal("Working Queue size returned should not be 0")
	}
	timeout := time.After(5 * time.Second)
	for startCount := 0; startCount < max; {
		select {
		case <-started:
			startCount++
		case <-timeout:
			t.Fatal("timed out waiting for workers to start")
		}
	}

	// Release workers.
	close(release)
}

func TestReuseWorkers(t *testing.T) {
	t.Parallel()

	wp := New(5)
	defer wp.Stop()

	release := make(chan struct{})

	// Cause worker to be created, and available for reuse before next task.
	for i := 0; i < 10; i++ {
		wp.Submit(func() { <-release })
		release <- struct{}{}
		time.Sleep(time.Millisecond)
	}
	close(release)

	// If the same worker was always reused, then only one worker would have
	// been created and there should only be one ready.
	if countReady(wp) > 1 {
		t.Fatal("Worker not reused")
	}
}

func TestWorkerTimeout(t *testing.T) {
	t.Parallel()

	wp := New(max)
	defer wp.Stop()

	var started sync.WaitGroup
	started.Add(max)
	release := make(chan struct{})

	// Cause workers to be created.  Workers wait on channel, keeping them busy
	// and causing the worker pool to create more.
	for i := 0; i < max; i++ {
		wp.Submit(func() {
			started.Done()
			<-release
		})
	}

	// Wait for tasks to start.
	started.Wait()

	if anyReady(wp) {
		t.Fatal("number of ready workers should be zero")
	}

	if wp.killIdleWorker() {
		t.Fatal("should have been no idle workers to kill")
	}

	// Release workers.
	close(release)

	if countReady(wp) != max {
		t.Fatal("Expected", max, "ready workers")
	}

	// Check that a worker timed out.
	time.Sleep(idleTimeout + time.Second)
	if countReady(wp) != max-1 {
		t.Fatal("First worker did not timeout")
	}
}

func TestStop(t *testing.T) {
	t.Parallel()

	wp := New(max)
	defer wp.Stop()

	release := make(chan struct{})
	var started sync.WaitGroup
	started.Add(max)

	// Start workers, and have them all wait on a channel before completing.
	for i := 0; i < max; i++ {
		wp.Submit(func() {
			started.Done()
			<-release
		})
	}

	// Wait for all queued tasks to be dispatched to workers.
	started.Wait()

	// Release workers.
	close(release)

	if wp.Stopped() {
		t.Fatal("pool should not be stopped")
	}

	wp.Stop()
	if anyReady(wp) {
		t.Fatal("should have zero workers after stop")
	}

	if !wp.Stopped() {
		t.Fatal("pool should be stopped")
	}

	// Start workers, and have them all wait on a channel before completing.
	wp = New(5)
	release = make(chan struct{})
	finished := make(chan struct{}, max)
	for i := 0; i < max; i++ {
		wp.Submit(func() {
			<-release
			finished <- struct{}{}
		})
	}

	// Call Stop() and see that only the already running tasks were completed.
	go func() {
		time.Sleep(10 * time.Millisecond)
		close(release)
	}()
	wp.Stop()
	var count int
Count:
	for count < max {
		select {
		case <-finished:
			count++
		default:
			break Count
		}
	}
	if count > 5 {
		t.Fatal("Should not have completed any queued tasks, did", count)
	}

	// Check that calling Stop() again is OK.
	wp.Stop()
}

func TestStopWait(t *testing.T) {
	t.Parallel()

	// Start workers, and have them all wait on a channel before completing.
	wp := New(5)
	release := make(chan struct{})
	finished := make(chan struct{}, max)
	for i := 0; i < max; i++ {
		wp.Submit(func() {
			<-release
			finished <- struct{}{}
		})
	}

	// Call StopWait() and see that all tasks were completed.
	go func() {
		time.Sleep(10 * time.Millisecond)
		close(release)
	}()
	wp.StopWait()
	for count := 0; count < max; count++ {
		select {
		case <-finished:
		default:
			t.Fatal("Should have completed all queued tasks")
		}
	}

	if anyReady(wp) {
		t.Fatal("should have zero workers after stopwait")
	}

	if !wp.Stopped() {
		t.Fatal("pool should be stopped")
	}

	// Make sure that calling StopWait() with no queued tasks is OK.
	wp = New(5)
	wp.StopWait()

	if anyReady(wp) {
		t.Fatal("should have zero workers after stopwait")
	}

	// Check that calling StopWait() again is OK.
	wp.StopWait()
}

func TestSubmitWait(t *testing.T) {
	wp := New(1)
	defer wp.Stop()

	// Check that these are noop.
	wp.Submit(nil)
	wp.SubmitWait(nil)

	done1 := make(chan struct{})
	wp.Submit(func() {
		time.Sleep(100 * time.Millisecond)
		close(done1)
	})
	select {
	case <-done1:
		t.Fatal("Submit did not return immediately")
	default:
	}

	done2 := make(chan struct{})
	wp.SubmitWait(func() {
		time.Sleep(100 * time.Millisecond)
		close(done2)
	})
	select {
	case <-done2:
	default:
		t.Fatal("SubmitWait did not wait for function to execute")
	}
}

func TestOverflow(t *testing.T) {
	wp := New(2)
	releaseChan := make(chan struct{})

	// Start workers, and have them all wait on a channel before completing.
	for i := 0; i < 64; i++ {
		wp.Submit(func() { <-releaseChan })
	}

	// Start a goroutine to free the workers after calling stop.  This way
	// the dispatcher can exit, then when this goroutine runs, the workerpool
	// can exit.
	go func() {
		<-time.After(time.Millisecond)
		close(releaseChan)
	}()
	wp.Stop()

	// Now that the worker pool has exited, it is safe to inspect its waiting
	// queue without causing a race.
	qlen := wp.waitingQueue.Len()
	if qlen != 62 {
		t.Fatal("Expected 62 tasks in waiting queue, have", qlen)
	}
}

func TestStopRace(t *testing.T) {
	wp := New(20)
	workRelChan := make(chan struct{})

	var started sync.WaitGroup
	started.Add(20)

	// Start workers, and have them all wait on a channel before completing.
	for i := 0; i < 20; i++ {
		wp.Submit(func() {
			started.Done()
			<-workRelChan
		})
	}

	started.Wait()

	const doneCallers = 5
	stopDone := make(chan struct{}, doneCallers)
	for i := 0; i < doneCallers; i++ {
		go func() {
			wp.Stop()
			stopDone <- struct{}{}
		}()
	}

	select {
	case <-stopDone:
		t.Fatal("Stop should not return in any goroutine")
	default:
	}

	close(workRelChan)

	timeout := time.After(time.Second)
	for i := 0; i < doneCallers; i++ {
		select {
		case <-stopDone:
		case <-timeout:
			t.Fatal("timedout waiting for Stop to return")
		}
	}
}

// Run this test with race detector to test that using WaitingQueueSize has no
// race condition
func TestWaitingQueueSizeRace(t *testing.T) {
	const (
		goroutines = 10
		tasks      = 20
		workers    = 5
	)
	wp := New(workers)
	maxChan := make(chan int)
	for g := 0; g < goroutines; g++ {
		go func() {
			max := 0
			// Submit 100 tasks, checking waiting queue size each time.  Report
			// the maximum queue size seen.
			for i := 0; i < tasks; i++ {
				wp.Submit(func() {
					time.Sleep(time.Microsecond)
				})
				waiting := wp.WaitingQueueSize()
				if waiting > max {
					max = waiting
				}
			}
			maxChan <- max
		}()
	}

	// Find maximum queuesize seen by any goroutine.
	maxMax := 0
	for g := 0; g < goroutines; g++ {
		max := <-maxChan
		if max > maxMax {
			maxMax = max
		}
	}
	if maxMax == 0 {
		t.Error("expected to see waiting queue size > 0")
	}
	if maxMax >= goroutines*tasks {
		t.Error("should not have seen all tasks on waiting queue")
	}
}

func anyReady(w *WorkerPool) bool {
	select {
	case w.workerQueue <- nil:
		go worker(w.workerQueue)
		return true
	default:
	}
	return false
}

func countReady(w *WorkerPool) int {
	// Try to stop max workers.
	timeout := time.After(time.Second)
	var readyCount int
	for i := 0; i < max; i++ {
		select {
		case w.workerQueue <- nil:
			readyCount++
		case <-timeout:
			i = max
		}
	}

	// Restore workers.
	for i := 0; i < readyCount; i++ {
		go worker(w.workerQueue)
	}
	return readyCount
}

/*

Run benchmarking with: go test -bench '.'

*/

func BenchmarkEnqueue(b *testing.B) {
	wp := New(1)
	defer wp.Stop()
	releaseChan := make(chan struct{})

	b.ResetTimer()

	// Start workers, and have them all wait on a channel before completing.
	for i := 0; i < b.N; i++ {
		wp.Submit(func() { <-releaseChan })
	}
	close(releaseChan)
}

func BenchmarkEnqueue2(b *testing.B) {
	wp := New(2)
	defer wp.Stop()

	b.ResetTimer()

	// Start workers, and have them all wait on a channel before completing.
	for i := 0; i < b.N; i++ {
		releaseChan := make(chan struct{})
		for i := 0; i < 64; i++ {
			wp.Submit(func() { <-releaseChan })
		}
		close(releaseChan)
	}
}

func BenchmarkExecute1Worker(b *testing.B) {
	benchmarkExecWorkers(1, b)
}

func BenchmarkExecute2Worker(b *testing.B) {
	benchmarkExecWorkers(2, b)
}

func BenchmarkExecute4Workers(b *testing.B) {
	benchmarkExecWorkers(4, b)
}

func BenchmarkExecute16Workers(b *testing.B) {
	benchmarkExecWorkers(16, b)
}

func BenchmarkExecute64Workers(b *testing.B) {
	benchmarkExecWorkers(64, b)
}

func BenchmarkExecute1024Workers(b *testing.B) {
	benchmarkExecWorkers(1024, b)
}

func benchmarkExecWorkers(n int, b *testing.B) {
	wp := New(n)
	defer wp.Stop()
	var allDone sync.WaitGroup
	allDone.Add(b.N * n)

	b.ResetTimer()

	// Start workers, and have them all wait on a channel before completing.
	for i := 0; i < b.N; i++ {
		for j := 0; j < n; j++ {
			wp.Submit(func() {
				//time.Sleep(100 * time.Microsecond)
				allDone.Done()
			})
		}
	}
	allDone.Wait()
}
