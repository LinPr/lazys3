// Package parallel provides a bounded-concurrency task Manager and a
// Waiter for collecting errors from concurrently running tasks.
//
// The Manager uses a buffered channel as a counting semaphore to cap the
// number of in-flight tasks. The Waiter exposes an unbuffered error channel
// that callers must drain concurrently with Wait, because task goroutines
// block on sending errors until a reader is ready.
//
// This is a minimal port of github.com/LinPr/s6cmd/internal/parallel for
// lazys3's storage.sync: signal-em + Waiter, no globals/fdlimit.
package parallel

import (
	"runtime"
	"sync"
)

const (
	// minNumWorkers is the floor for Manager concurrency. Below this the
	// semaphore would permit only one task at a time, defeating the point
	// of a parallel manager.
	minNumWorkers = 2
)

// Task is a function executed by the Manager.
type Task func() error

// Manager runs Tasks with bounded concurrency.
type Manager struct {
	wg        *sync.WaitGroup
	semaphore chan struct{}
}

// New creates a Manager whose concurrency is workercount. A negative
// workercount is interpreted as -N * runtime.NumCPU() (so -2 on a 4-core
// machine yields 8 workers). Values below minNumWorkers are raised to
// minNumWorkers.
func New(workercount int) *Manager {
	if workercount < 0 {
		workercount = runtime.NumCPU() * -workercount
	}
	if workercount < minNumWorkers {
		workercount = minNumWorkers
	}
	return &Manager{
		wg:        &sync.WaitGroup{},
		semaphore: make(chan struct{}, workercount),
	}
}

// acquire blocks until a semaphore slot is available.
func (p *Manager) acquire() {
	p.semaphore <- struct{}{}
}

// release frees a semaphore slot.
func (p *Manager) release() {
	<-p.semaphore
}

// Run schedules task on the Manager and registers it with waiter. The
// caller must be draining waiter.Err() before (or concurrently with)
// calling Wait, since the error channel is unbuffered and task goroutines
// block until a reader is ready.
func (p *Manager) Run(task Task, waiter *Waiter) {
	waiter.wg.Add(1)
	p.acquire()
	p.wg.Add(1)
	go func() {
		defer waiter.wg.Done()
		defer p.release()
		defer p.wg.Done()

		if err := task(); err != nil {
			waiter.errch <- err
		}
	}()
}

// Close blocks until all in-flight tasks have completed and then closes
// the semaphore. It must be called exactly once when the Manager is no
// longer needed.
func (p *Manager) Close() {
	p.wg.Wait()
	close(p.semaphore)
}

// Waiter collects errors from tasks scheduled via Manager.Run.
type Waiter struct {
	wg    sync.WaitGroup
	errch chan error
}

// NewWaiter creates a Waiter with an unbuffered error channel.
func NewWaiter() *Waiter {
	return &Waiter{
		errch: make(chan error),
	}
}

// Wait blocks until every task registered with this Waiter has finished
// and then closes the error channel. After Wait returns, Err() returns a
// closed channel.
//
// Callers MUST start draining Err() before calling Wait — otherwise a
// task that errors will block forever on the unbuffered send, and Wait
// will never return.
func (w *Waiter) Wait() {
	w.wg.Wait()
	close(w.errch)
}

// Err returns the read-only error channel. Callers must drain this channel
// concurrently with Wait; otherwise task goroutines that report errors
// will block forever.
func (w *Waiter) Err() <-chan error {
	return w.errch
}
