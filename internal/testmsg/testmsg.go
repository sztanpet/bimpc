// Package testmsg holds the MessagePack types and the birpc services shared
// by the codec tests
package testmsg

import (
	"errors"
	"sync"
)

//go:generate msgp
//msgp:ignore Arith Plain Counter

// Args is the argument type of the test services
type Args struct {
	A int64  `msg:"a"`
	B int64  `msg:"b"`
	S string `msg:"s"`
}

// Result is the reply type of the test services
type Result struct {
	C int64  `msg:"c"`
	S string `msg:"s"`
}

// Plain deliberately has no MessagePack methods, it is used to check that
// codecs refuse to send payloads they cannot serialize
type Plain struct {
	Whatever int
}

// ErrExploded is what Arith.Explode returns
var ErrExploded = errors.New("exploded on purpose")

// Counter is a concurrency safe call counter
//
// It is a separate type on purpose: every exported method of a birpc service
// has to look like an RPC method, so the service itself cannot have one.
type Counter struct {
	mu sync.Mutex
	n  int
}

func (c *Counter) inc() {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
}

// Get reports how many calls were counted
func (c *Counter) Get() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// Arith is a birpc service exercising the request/reply paths
type Arith struct {
	// Calls counts every method invocation
	Calls Counter

	// Block, when non-nil, is waited on by Arith.Wait
	Block chan struct{}
}

// Add sums the arguments
func (a *Arith) Add(args *Args, reply *Result) error {
	a.Calls.inc()
	reply.C = args.A + args.B
	reply.S = args.S
	return nil
}

// Explode always fails, exercising the error path of the codecs
func (a *Arith) Explode(args *Args, reply *Result) error {
	a.Calls.inc()
	return ErrExploded
}

// Wait blocks until Block is closed, used to have several calls in flight
func (a *Arith) Wait(args *Args, reply *Result) error {
	a.Calls.inc()
	if a.Block != nil {
		<-a.Block
	}
	reply.C = args.A
	return nil
}
