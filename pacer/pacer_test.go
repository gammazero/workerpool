package pacer

import (
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/gammazero/workerpool"
)

func TestPacedWorkers(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		delay1 := 100 * time.Millisecond
		delay2 := 300 * time.Millisecond
		wp := workerpool.New(5)
		defer wp.Stop()

		pacer := NewPacer(delay1)
		defer stopPacer(pacer)

		slowPacer := NewPacer(delay2)
		defer stopPacer(slowPacer)

		tasksDone := new(sync.WaitGroup)
		tasksDone.Add(20)
		start := time.Now()

		pacedTask := pacer.Pace(func() {
			tasksDone.Done()
		})

		slowPacedTask := slowPacer.Pace(func() {
			tasksDone.Done()
		})

		// Cause worker to be created, and available for reuse before next
		// task.
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
		// 9 times delay2 since no wait for first task, and pacer and
		// slowPacer run currently so only limiter is slowPacer.
		if elapsed < 9*delay2 {
			t.Fatal("Did not pace tasks correctly - finished too soon:", elapsed)
		}
	})
}

func TestPauseBeforeFirstTask(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		p := NewPacer(time.Millisecond)
		defer stopPacer(p)
		p.Pause()

		done := make(chan struct{})
		go func() {
			p.Next()
			close(done)
		}()

		// Wait until Next is blocked.
		synctest.Wait()
		select {
		case <-done:
			t.Fatal("Next returned while the pacer was paused")
		default:
		}

		p.Resume()
		<-done
	})
}

func TestPauseWithWaitingCallers(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		const (
			delay   = 50 * time.Millisecond
			callers = 10
		)
		p := NewPacer(delay)
		defer stopPacer(p)

		var released atomic.Int32
		var wg sync.WaitGroup
		for range callers {
			wg.Go(func() {
				p.Next()
				released.Add(1)
			})
		}
		// Let the first caller be released and the rest queue up behind it.
		time.Sleep(delay / 2)
		synctest.Wait()
		if n := released.Load(); n != 1 {
			t.Fatalf("expected 1 task released before Pause, got %d", n)
		}

		start := time.Now()
		p.Pause()
		elapsed := time.Since(start)

		// Pause should only wait for the caller already holding the pause
		// token, which is released when the first delay ends.
		if elapsed != delay/2 {
			t.Errorf("Pause took %v with %d waiting callers, expected %v",
				elapsed, callers, delay/2)
		}
		synctest.Wait()
		after := released.Load()
		if after != 2 {
			t.Errorf("expected 1 task released while Pause was waiting, got %d",
				after-1)
		}

		time.Sleep(10 * delay)
		synctest.Wait()
		if n := released.Load() - after; n != 0 {
			t.Errorf("%d tasks released after Pause returned", n)
		}

		p.Resume()
		wg.Wait()
		if n := released.Load(); n != callers {
			t.Errorf("expected %d tasks released, got %d", callers, n)
		}
	})
}

// stopPacer stops p and lets its run goroutine finish any pacing delay it is
// sleeping through, so it can exit before the synctest bubble ends. Fake time
// does not advance after the bubble's main goroutine returns.
func stopPacer(p *Pacer) {
	p.Stop()
	time.Sleep(p.delay)
}
