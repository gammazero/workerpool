package job

import (
	"testing"

	"github.com/gammazero/workerpool"
)

func TestJobStandalone(t *testing.T) {
	expectedName := "A"
	expectedResult := "from a"
	var expectedErr error = nil

	results := make(chan Result)

	job := NewJob("A", results, func() (interface{}, error) {
		return "from a", nil
	})

	go job.Run()

	select {
	case r := <-results:
		if r.JobName() != expectedName {
			t.Fatalf("Expected %s : got %s", expectedName, r.JobName())
		}
		if r.Data() != expectedResult {
			t.Fatalf("Expected %s : got %s", expectedResult, r.Data())
		}
		if r.Err() != expectedErr {
			t.Fatalf("Expected %s : got %s", expectedErr, r.Err())
		}
	}
}

func TestJobWorkerPool(t *testing.T) {
	expectedName := "A"
	expectedResult := "from a"
	var expectedErr error = nil

	results := make(chan Result)

	job := NewJob("A", results, func() (interface{}, error) {
		return "from a", nil
	})

	wp := workerpool.New(3)
	wp.Submit(job.Run)

	select {
	case r := <-results:
		if r.JobName() != expectedName {
			t.Fatalf("Expected %s : got %s", expectedName, r.JobName())
		}
		if r.Data() != expectedResult {
			t.Fatalf("Expected %s : got %s", expectedResult, r.Data())
		}
		if r.Err() != expectedErr {
			t.Fatalf("Expected %s : got %s", expectedErr, r.Err())
		}
	}

	wp.Stop()
}
