package workerpool

import "time"

// Pacer is a goroutine rate limiter.  When concurrent goroutines call
// Pacer.Next(), the call returns in one goroutine at a time, at a rate no
// faster than one per delay time.
//
// To use Pacer, create a new Pacer giving the interval that must elapse
// between the time one task is started and the next task is started.  Then
// call WorkerPool.SubmitPaced() or WorkerPool.SubmitPacedWait().
type Pacer struct {
	delay time.Duration
	gate  chan struct{}
}

// NewPacer creates and runs a new Pacer.
func NewPacer(delay time.Duration) *Pacer {
	p := &Pacer{
		delay: delay,
		gate:  make(chan struct{}),
	}
	go p.run()
	return p
}

// Pace wraps a function in a paced function.  The returned paced function can
// then be submitted to the workerpool, using Submit or SubmitWait, and
// starting the tasks is paced according to the pacer's delay.
func (p *Pacer) Pace(task func()) func() {
	return func() {
		p.Next()
		task()
	}
}

// Next submits a run request to the gate and returns when it is time to run.
func (p *Pacer) Next() {
	// Wait for item to be read from gate.
	p.gate <- struct{}{}
	return
}

// Stop stops the Pacer from running.
func (p *Pacer) Stop() {
	close(p.gate)
}

func (p *Pacer) run() {
	// Read item from gate no faster that one per delay.
	// Reading from the unbuffered channel serves as a "tick"
	// and unblocks the writer.
	for _ = range p.gate {
		time.Sleep(p.delay)
	}
}
