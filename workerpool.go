package workerpool

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gammazero/deque"
)

// ErrStopped is returned by Do when the worker pool is stopped.
var ErrStopped = errors.New("submitting work to stopped workerpool")

// New creates and starts a pool of worker goroutines.
//
// The maxWorkers parameter specifies the maximum number of workers that can
// execute tasks concurrently. When there are no incoming tasks, workers are
// gradually stopped until there are no remaining workers.
func New(maxWorkers int, options ...Option) *WorkerPool {
	// There must be at least one worker.
	if maxWorkers < 1 {
		maxWorkers = 1
	}

	cfg := config{
		idleTimeout: DefaultIdleTimeout,
	}
	for _, opt := range options {
		opt(&cfg)
	}

	pool := &WorkerPool{
		maxWorkers:  maxWorkers,
		taskQueue:   make(chan func()),
		workerQueue: make(chan func()),
		stopSignal:  make(chan struct{}),
		stoppedChan: make(chan struct{}),
	}

	// Start the task dispatcher.
	go pool.dispatch(cfg.idleTimeout)

	return pool
}

// WorkerPool is a collection of goroutines, where the number of concurrent
// goroutines processing requests does not exceed the specified maximum.
type WorkerPool struct {
	maxWorkers   int
	taskQueue    chan func()
	workerQueue  chan func()
	stoppedChan  chan struct{}
	stopSignal   chan struct{}
	waitingQueue deque.Deque[func()]
	stopLock     sync.Mutex
	submitLock   sync.RWMutex
	stopOnce     sync.Once
	stopped      atomic.Bool
	waiting      atomic.Int64
	wait         bool
}

// Size returns the maximum number of concurrent workers.
func (p *WorkerPool) Size() int {
	return p.maxWorkers
}

// Stop stops the worker pool and waits for only currently running tasks to
// complete. Pending tasks that are not currently running are abandoned. Once
// stopped, Do and DoWait return ErrStopped.
//
// Since creating the worker pool starts at least one goroutine, for the
// dispatcher, Stop() or StopWait() should be called when the worker pool is no
// longer needed.
func (p *WorkerPool) Stop() {
	p.stop(false)
}

// StopWait stops the worker pool and waits for all queued tasks to complete.
// No additional tasks may be submitted, but all pending tasks are executed by
// workers before this function returns.
func (p *WorkerPool) StopWait() {
	p.stop(true)
}

// Stopped returns true if this worker pool has been stopped.
func (p *WorkerPool) Stopped() bool {
	return p.stopped.Load()
}

// Do enqueues a function for a worker to execute. Returns ErrStopped if the
// worker pool is stopped.
//
// Any external values needed by the task function must be captured in a
// closure. Any return values should be returned over a channel that is
// captured in the task function closure.
//
// Do will not block regardless of the number of tasks submitted. Each task
// is immediately given to an available worker or to a newly started worker. If
// there are no available workers, and the maximum number of workers are
// already created, then the task is put onto a waiting queue.
//
// When there are tasks on the waiting queue, any additional new tasks are put
// on the waiting queue. Tasks are removed from the waiting queue as workers
// become available.
//
// As long as no new tasks arrive, one available worker is shutdown each time
// period until there are no more idle workers. Since the time to start new
// goroutines is not significant, there is no need to retain idle workers
// indefinitely.
//
// Do is safe to call concurrently with Stop or StopWait. A task submitted
// while the worker pool is stopping is either accepted, the same as a task
// submitted just before stopping, or rejected with ErrStopped.
//
// A task that panics crashes the program, as with any goroutine. Recover
// within the task function to prevent this.
func (p *WorkerPool) Do(task func()) error {
	if task == nil {
		return nil
	}
	// Holding submitLock for reading keeps stop from closing the taskQueue
	// while sending to it.
	p.submitLock.RLock()
	defer p.submitLock.RUnlock()
	if p.stopped.Load() {
		return ErrStopped
	}
	p.taskQueue <- task
	return nil
}

// DoWait enqueues the given function and waits for it to be executed. Returns
// ErrStopped if the worker pool is stopped, or if the worker pool is stopped
// with Stop before the task is executed. When stopped with StopWait, the task
// is executed before DoWait returns.
func (p *WorkerPool) DoWait(task func()) error {
	if task == nil {
		return nil
	}
	doneChan := make(chan struct{})
	err := p.Do(func() {
		task()
		close(doneChan)
	})
	if err != nil {
		return err
	}
	select {
	case <-doneChan:
	case <-p.stoppedChan:
		// The stoppedChan is closed after all workers exit, so if the task
		// was executed then doneChan is already closed.
		select {
		case <-doneChan:
		default:
			return ErrStopped
		}
	}
	return nil
}

// Submit enqueues the given function without blocking. Submit is like Do but
// panics instead of returning ErrStopped. Use Submit when submitting to a
// stopped worker pool is a programming error.
func (p *WorkerPool) Submit(task func()) {
	if err := p.Do(task); err != nil {
		panic(err)
	}
}

// SubmitWait enqueues the given function and waits for it to be executed.
// SubmitWait is like DoWait but panics instead of returning ErrStopped. Use
// SubmitWait when submitting to a stopped worker pool is a programming error.
func (p *WorkerPool) SubmitWait(task func()) {
	if err := p.DoWait(task); err != nil {
		panic(err)
	}
}

// WaitingQueueSize returns the count of tasks in the waiting queue.
func (p *WorkerPool) WaitingQueueSize() int {
	return int(p.waiting.Load())
}

// Pause causes all workers to wait on the given Context, thereby making them
// unavailable to run tasks. Pause returns when all workers are waiting. Tasks
// can continue to be queued to the workerpool, but are not executed until the
// Context is canceled or times out.
//
// Calling Pause when the worker pool is already paused causes Pause to wait
// until all previous pauses are canceled. This allows a goroutine to take
// control of pausing and unpausing the pool as soon as other goroutines have
// unpaused it.
//
// When the workerpool is stopped, workers are unpaused and queued tasks are
// executed during StopWait.
func (p *WorkerPool) Pause(ctx context.Context) {
	p.stopLock.Lock()
	defer p.stopLock.Unlock()
	if p.stopped.Load() {
		return
	}
	ready := new(sync.WaitGroup)
	ready.Add(p.maxWorkers)
	for range p.maxWorkers {
		p.taskQueue <- func() {
			ready.Done()
			select {
			case <-ctx.Done():
			case <-p.stopSignal:
			}
		}
	}
	// Wait for workers to all be paused
	ready.Wait()
}

// dispatch sends the next queued task to an available worker.
func (p *WorkerPool) dispatch(idleTimeout time.Duration) {
	defer close(p.stoppedChan)
	var idle bool
	var idleTO <-chan time.Time
	var workerCount int
	var wg sync.WaitGroup

	var timeout *time.Timer
	if idleTimeout > 0 {
		timeout = time.NewTimer(idleTimeout)
		defer timeout.Stop()
		idleTO = timeout.C
	}

Loop:
	for {
		// As long as tasks are in the waiting queue, incoming tasks are put
		// into the waiting queue and tasks to run are taken from the waiting
		// queue. Once the waiting queue is empty, then go back to submitting
		// incoming tasks directly to available workers.
		if p.waitingQueue.Len() != 0 {
			if !p.processWaitingQueue() {
				break Loop
			}
			continue
		}

		select {
		case task, ok := <-p.taskQueue:
			if !ok {
				break Loop
			}
			// Got a task to do.
			select {
			case p.workerQueue <- task:
			default:
				// Create a new worker, if not at max.
				if workerCount < p.maxWorkers {
					wg.Add(1)
					go worker(task, p.workerQueue, &wg)
					workerCount++
				} else {
					// Enqueue task to be executed by next available worker.
					p.waitingQueue.PushBack(task)
					p.waiting.Store(int64(p.waitingQueue.Len()))
				}
			}
			idle = false
		case <-idleTO:
			// Timed out waiting for work to arrive. Kill a ready worker if
			// pool has been idle for a whole timeout.
			if idle && workerCount > 0 {
				if p.killIdleWorker() {
					workerCount--
				}
			}
			idle = true
			timeout.Reset(idleTimeout)
		}
	}

	// If instructed to wait, then run tasks that are already queued.
	if p.wait {
		p.runQueuedTasks()
	}
	p.waitingQueue = deque.Deque[func()]{}

	// Stop all remaining workers as they become ready.
	for workerCount > 0 {
		p.workerQueue <- nil
		workerCount--
	}
	wg.Wait()
}

// worker executes tasks and stops when it receives a nil task.
func worker(task func(), workerQueue chan func(), wg *sync.WaitGroup) {
	for task != nil {
		task()
		task = <-workerQueue
	}
	wg.Done()
}

// stop tells the dispatcher to exit, and whether or not to complete queued
// tasks.
func (p *WorkerPool) stop(wait bool) {
	p.stopOnce.Do(func() {
		// Signal that workerpool is stopping, to unpause any paused workers.
		close(p.stopSignal)
		// Acquire stopLock to wait for any pause in progress to complete. All
		// in-progress pauses will complete because the stopSignal unpauses the
		// workers.
		p.stopLock.Lock()
		// The stopped flag prevents any additional paused workers. This makes
		// it safe to close the taskQueue, since no Pause can be sending to it.
		// Do returns ErrStopped once it sees the stopped flag.
		p.stopped.Store(true)
		p.stopLock.Unlock()
		p.wait = wait
		// Close task queue and wait for currently running tasks to finish.
		// Acquiring submitLock waits for any Do that is sending to the
		// taskQueue, so that the close does not race with a send.
		p.submitLock.Lock()
		close(p.taskQueue)
		p.submitLock.Unlock()
	})
	<-p.stoppedChan
}

// processWaitingQueue puts new tasks onto the waiting queue, and removes
// tasks from the waiting queue as workers become available. Returns false if
// worker pool is stopped.
func (p *WorkerPool) processWaitingQueue() bool {
	select {
	case task, ok := <-p.taskQueue:
		if !ok {
			return false
		}
		p.waitingQueue.PushBack(task)
	case p.workerQueue <- p.waitingQueue.Front():
		// A worker was ready, so gave task to worker.
		p.waitingQueue.PopFront()
	}
	p.waiting.Store(int64(p.waitingQueue.Len()))
	return true
}

func (p *WorkerPool) killIdleWorker() bool {
	select {
	case p.workerQueue <- nil:
		// Sent kill signal to worker.
		return true
	default:
		// No ready workers. All, if any, workers are busy.
		return false
	}
}

// runQueuedTasks removes each task from the waiting queue and gives it to
// workers until queue is empty.
func (p *WorkerPool) runQueuedTasks() {
	for p.waitingQueue.Len() != 0 {
		// A worker is ready, so give task to worker.
		p.workerQueue <- p.waitingQueue.PopFront()
		p.waiting.Store(int64(p.waitingQueue.Len()))
	}
}
