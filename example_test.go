package workerpool

import (
	"fmt"
	"testing"
)

func TestExample(t *testing.T) {
	wp := New(2)
	requests := []string{"alpha", "beta", "gamma", "delta", "epsilon"}

	reqCount := 0
	for _, r := range requests {
		r := r
		wp.Submit(func() {
			fmt.Println("Handling request:", r)
			reqCount++
		})
	}

	wp.StopWait()

	if reqCount < len(requests) {
		t.Fatal("Did not handle all requests")
	}
}
