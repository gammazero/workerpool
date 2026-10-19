package pacer

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gammazero/workerpool"
)

func TestPacedWorkers(t *testing.T) {
	t.Parallel()

	delay1 := 100 * time.Millisecond
	delay2 := 300 * time.Millisecond
	wp := workerpool.New(5)
	defer wp.Stop()

	pacer := NewPacer(delay1)
	defer pacer.Stop()

	slowPacer := NewPacer(delay2)
	defer slowPacer.Stop()

	tasksDone := new(sync.WaitGroup)
	tasksDone.Add(20)
	start := time.Now()

	pacedTask := pacer.Pace(func() {
		//fmt.Println("Task")
		tasksDone.Done()
	})

	slowPacedTask := slowPacer.Pace(func() {
		//fmt.Println("SlowTask")
		tasksDone.Done()
	})

	// Cause worker to be created, and available for reuse before next task.
	for range 10 {
		wp.Submit(pacedTask)
		wp.Submit(slowPacedTask)
	}

	time.Sleep(500 * time.Millisecond)
	pacer.Pause()
	time.Sleep(time.Second)
	if !pacer.IsPaused() {
		t.Fatal("should be paused")
	}
	pacer.Resume()
	if pacer.IsPaused() {
		t.Fatal("should not be paused")
	}

	tasksDone.Wait()
	elapsed := time.Since(start)
	// 9 times delay2 since no wait for first task, and pacer and slowPacer run
	// currently so only limiter is slowPacer.
	if elapsed < 9*delay2 {
		t.Fatal("Did not pace tasks correctly - finished too soon:", elapsed)
	}
}

func TestPauseBeforeFirstTask(t *testing.T) {
	p := NewPacer(time.Millisecond)
	defer p.Stop()
	p.Pause()

	done := make(chan struct{})
	go func() {
		p.Next()
		close(done)
	}()

	select {
	case <-done:
		t.Error("Next returned while the pacer was paused")
	case <-time.After(100 * time.Millisecond):
	}

	p.Resume()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Next did not return after Resume")
	}
}

func TestPauseWithWaitingCallers(t *testing.T) {
	t.Parallel()

	const (
		delay   = 50 * time.Millisecond
		callers = 10
	)
	p := NewPacer(delay)
	defer p.Stop()

	var released atomic.Int32
	var wg sync.WaitGroup
	for range callers {
		wg.Go(func() {
			p.Next()
			released.Add(1)
		})
	}
	// Let the callers queue up behind the first released one.
	time.Sleep(delay / 2)

	before := released.Load()
	start := time.Now()
	p.Pause()
	elapsed := time.Since(start)
	after := released.Load()

	// Pause should only wait for the caller already holding the pause token,
	// not for every waiting caller.
	if elapsed > 3*delay {
		t.Errorf("Pause took %v with %d waiting callers", elapsed, callers)
	}
	if n := after - before; n > 1 {
		t.Errorf("%d tasks released while Pause was waiting", n)
	}

	time.Sleep(3 * delay)
	if n := released.Load() - after; n != 0 {
		t.Errorf("%d tasks released after Pause returned", n)
	}

	p.Resume()
	wg.Wait()
	if n := released.Load(); n != callers {
		t.Errorf("expected %d tasks released, got %d", callers, n)
	}
}
