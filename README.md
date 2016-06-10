# workerpool
[![Build Status](https://travis-ci.org/gammazero/workerpool.svg)](https://travis-ci.org/gammazero/workerpool)
[![Go Report Card](https://goreportcard.com/badge/github.com/gammazero/workerpool)](https://goreportcard.com/report/github.com/gammazero/workerpool)

Concurrency limiting goroutine pool

[![GoDoc](https://godoc.org/github.com/gammazero/workerpool?status.svg)](https://godoc.org/github.com/gammazero/workerpool)

## Installation
To install this package, you need to setup your Go workspace.  The simplest way to install the library is to run:
```
$ go get github.com/gammazero/workerpool
```

## Example
```go
package main

import (
	"fmt"
	"github.com/gammazero/workerpool"
)

func main() {
	wp := workerpool.New(2)
	requests := []string{"alpha", "beta", "gamma", "delta", "epsilon"}

	for _, r := range requests {
		r := r
		wp.Submit(func() {
			fmt.Println("Handling request:", r)
		})
	}

	wp.Stop()
}
```
