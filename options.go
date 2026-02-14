package workerpool

import "time"

const (
	// DefaultIdleTimeout is default amount of time the worker pool must be
	// idle before stopping a worker.
	DefaultIdleTimeout = 2 * time.Second
)

type config struct {
	idleTimeout time.Duration
}

// Option is a function that sets a value in a config.
type Option func(*config)

// WithIdleTimeout configures the the amount of time that the worker pool must
// be idle before a worker is automatically stopped. If zero or unset the value
// defaults to DefaultIdleTimeout.
func WithIdleTimeout(timeout time.Duration) Option {
	return func(c *config) {
		if timeout != 0 {
			c.idleTimeout = timeout
		}
	}
}
