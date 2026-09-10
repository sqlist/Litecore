package main

import (
	"sync"
	"testing"
	"time"
)

type fakeStopper struct {
	gracefulStarted chan struct{}
	release         chan struct{}
	releaseOnce     sync.Once

	mu        sync.Mutex
	stopCalls int
}

func newFakeStopper(block bool) *fakeStopper {
	f := &fakeStopper{gracefulStarted: make(chan struct{}, 1), release: make(chan struct{})}
	if !block {
		f.releaseOnce.Do(func() { close(f.release) })
	}
	return f
}

func (f *fakeStopper) GracefulStop() {
	signalChannel(f.gracefulStarted)
	<-f.release
}

func (f *fakeStopper) Stop() {
	f.mu.Lock()
	f.stopCalls++
	f.mu.Unlock()
	f.releaseOnce.Do(func() { close(f.release) })
}

func (f *fakeStopper) stops() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stopCalls
}

func TestStopGRPCServerFinishesGracefully(t *testing.T) {
	server := newFakeStopper(false)
	if graceful := stopGRPCServer(server, time.Second); !graceful {
		t.Fatal("expected graceful shutdown")
	}
	if calls := server.stops(); calls != 0 {
		t.Fatalf("Stop called during graceful shutdown: %d", calls)
	}
}

func TestStopGRPCServerForcesStopAfterTimeout(t *testing.T) {
	server := newFakeStopper(true)
	if graceful := stopGRPCServer(server, 10*time.Millisecond); graceful {
		t.Fatal("expected forced shutdown")
	}
	if calls := server.stops(); calls != 1 {
		t.Fatalf("expected exactly one Stop call, got %d", calls)
	}
}
