package workerpool

import (
	"fmt"
	"sync/atomic"
	"testing"
)

func TestExample(t *testing.T) {
	wp := New(2)
	requests := []string{"alpha", "beta", "gamma", "delta", "epsilon"}

	var reqCount int32
	for _, r := range requests {
		r := r
		wp.Submit(func() {
			fmt.Println("Handling request:", r)
			atomic.AddInt32(&reqCount, 1)
		})
	}

	wp.StopWait()

	if int(atomic.LoadInt32(&reqCount)) < len(requests) {
		t.Fatal("Did not handle all requests")
	}
}
