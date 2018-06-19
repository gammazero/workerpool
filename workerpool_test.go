package workerpool

import (
	"sync"
	"testing"
	"time"
)

const max = 20

func TestMaxWorkers(t *testing.T) {
	t.Parallel()

	wp := New(0)
	if wp.maxWorkers != 1 {
		t.Fatal("should have created one worker")
	}

	wp = New(max)
	defer wp.Stop()

	started := make(chan struct{}, max)
	sync := make(chan struct{})

	// Start workers, and have them all wait on a channel before completing.
	for i := 0; i < max; i++ {
		wp.Submit(func() {
			started <- struct{}{}
			<-sync
		})
	}

	// Wait for all queued tasks to be dispatched to workers.
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
	close(sync)
}

func TestReuseWorkers(t *testing.T) {
	t.Parallel()

	wp := New(5)
	defer wp.Stop()

	sync := make(chan struct{})

	// Cause worker to be created, and available for reuse before next task.
	for i := 0; i < 10; i++ {
		wp.Submit(func() { <-sync })
		sync <- struct{}{}
		time.Sleep(100 * time.Millisecond)
	}

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

	sync := make(chan struct{})
	started := make(chan struct{}, max)
	// Cause workers to be created.  Workers wait on channel, keeping them busy
	// and causing the worker pool to create more.
	for i := 0; i < max; i++ {
		wp.Submit(func() {
			started <- struct{}{}
			<-sync
		})
	}

	// Wait for tasks to start.
	for i := 0; i < max; i++ {
		<-started
	}

	if anyReady(wp) {
		t.Fatal("number of ready workers should be zero")
	}
	// Release workers.
	close(sync)

	if countReady(wp) != max {
		t.Fatal("Expected", max, "ready workers")
	}

	// Check that a worker timed out.
	time.Sleep((idleTimeoutSec + 1) * time.Second)
	if countReady(wp) != max-1 {
		t.Fatal("First worker did not timeout")
	}

	// Check that another worker timed out.
	time.Sleep((idleTimeoutSec + 1) * time.Second)
	if countReady(wp) != max-2 {
		t.Fatal("Second worker did not timeout")
	}
}

func TestStop(t *testing.T) {
	t.Parallel()

	wp := New(max)
	defer wp.Stop()

	started := make(chan struct{}, max)
	sync := make(chan struct{})

	// Start workers, and have them all wait on a channel before completing.
	for i := 0; i < max; i++ {
		wp.Submit(func() {
			started <- struct{}{}
			<-sync
		})
	}

	// Wait for all queued tasks to be dispatched to workers.
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
	close(sync)

	if wp.Stopped() {
		t.Error("pool should not be stopped")
	}

	wp.Stop()
	if anyReady(wp) {
		t.Error("should have zero workers after stop")
	}

	if !wp.Stopped() {
		t.Error("pool should be stopped")
	}
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
	releaseChan := make(chan struct{})
	workRelChan := make(chan struct{})

	// Start workers, and have them all wait on a channel before completing.
	for i := 0; i < 20; i++ {
		wp.Submit(func() { <-workRelChan })
	}

	time.Sleep(5 * time.Second)
	for i := 0; i < 64; i++ {
		go func() {
			<-releaseChan
			wp.Stop()
		}()
	}

	close(workRelChan)
	close(releaseChan)
}

func anyReady(w *WorkerPool) bool {
	select {
	case wkCh := <-w.readyWorkers:
		w.readyWorkers <- wkCh
		return true
	default:
	}
	return false
}

func countReady(w *WorkerPool) int {
	// Try to pull max workers off of ready queue.
	timeout := time.After(5 * time.Second)
	readyTmp := make(chan chan func(), max)
	var readyCount int
	for i := 0; i < max; i++ {
		select {
		case wkCh := <-w.readyWorkers:
			readyTmp <- wkCh
			readyCount++
		case <-timeout:
			readyCount = i
			i = max
		}
	}

	// Restore ready workers.
	close(readyTmp)
	go func() {
		for r := range readyTmp {
			w.readyWorkers <- r
		}
	}()
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
	releaseChan := make(chan struct{})

	b.ResetTimer()

	// Start workers, and have them all wait on a channel before completing.
	for i := 0; i < b.N; i++ {
		for i := 0; i < 64; i++ {
			wp.Submit(func() { <-releaseChan })
		}
		for i := 0; i < 64; i++ {
			releaseChan <- struct{}{}
		}
	}
	close(releaseChan)
}

func BenchmarkExecute1Worker(b *testing.B) {
	wp := New(1)
	defer wp.Stop()
	var allDone sync.WaitGroup
	allDone.Add(b.N)

	b.ResetTimer()

	// Start workers, and have them all wait on a channel before completing.
	for i := 0; i < b.N; i++ {
		wp.Submit(func() {
			time.Sleep(time.Millisecond)
			allDone.Done()
		})
	}
	allDone.Wait()
}

func BenchmarkExecute2Worker(b *testing.B) {
	wp := New(2)
	defer wp.Stop()
	var allDone sync.WaitGroup
	allDone.Add(b.N)

	b.ResetTimer()

	// Start workers, and have them all wait on a channel before completing.
	for i := 0; i < b.N; i++ {
		wp.Submit(func() {
			time.Sleep(time.Millisecond)
			allDone.Done()
		})
	}
	allDone.Wait()
}

func BenchmarkExecute4Workers(b *testing.B) {
	wp := New(4)
	defer wp.Stop()
	var allDone sync.WaitGroup
	allDone.Add(b.N)

	b.ResetTimer()

	// Start workers, and have them all wait on a channel before completing.
	for i := 0; i < b.N; i++ {
		wp.Submit(func() {
			time.Sleep(time.Millisecond)
			allDone.Done()
		})
	}
	allDone.Wait()
}

func BenchmarkExecute16Workers(b *testing.B) {
	wp := New(16)
	defer wp.Stop()
	var allDone sync.WaitGroup
	allDone.Add(b.N)

	b.ResetTimer()

	// Start workers, and have them all wait on a channel before completing.
	for i := 0; i < b.N; i++ {
		wp.Submit(func() {
			time.Sleep(time.Millisecond)
			allDone.Done()
		})
	}
	allDone.Wait()
}

func BenchmarkExecute64Workers(b *testing.B) {
	wp := New(64)
	defer wp.Stop()
	var allDone sync.WaitGroup
	allDone.Add(b.N)

	b.ResetTimer()

	// Start workers, and have them all wait on a channel before completing.
	for i := 0; i < b.N; i++ {
		wp.Submit(func() {
			time.Sleep(time.Millisecond)
			allDone.Done()
		})
	}
	allDone.Wait()
}
