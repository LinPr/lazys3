package parallel_test

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/LinPr/lazys3/internal/parallel"
)

// TestManager_RunsAllTasksConcurrentlyBounded verifies that submitting N
// tasks to a Manager runs every task exactly once, that the error channel
// is closed after Wait, and that no errors are emitted for nil-returning
// tasks.
func TestManager_RunsAllTasksConcurrentlyBounded(t *testing.T) {
	t.Parallel()
	m := parallel.New(4)
	w := parallel.NewWaiter()
	const n = 50
	var count int32
	for i := 0; i < n; i++ {
		m.Run(func() error {
			atomic.AddInt32(&count, 1)
			return nil
		}, w)
	}
	// Drain errors concurrently with Wait because the errch is unbuffered.
	var errs []error
	var drainWG sync.WaitGroup
	drainWG.Add(1)
	go func() {
		defer drainWG.Done()
		for err := range w.Err() {
			errs = append(errs, err)
		}
	}()
	m.Close()
	w.Wait()
	drainWG.Wait()
	if got := atomic.LoadInt32(&count); got != n {
		t.Errorf("ran %d tasks, want %d", got, n)
	}
	if len(errs) != 0 {
		t.Errorf("got %d errors, want 0: %v", len(errs), errs)
	}
}

// TestManager_BoundedConcurrency verifies that the manager never exceeds
// the configured worker count: at no point in time are more than `jobs`
// tasks running concurrently.
func TestManager_BoundedConcurrency(t *testing.T) {
	t.Parallel()
	const jobs = 4
	const n = 100
	m := parallel.New(jobs)
	w := parallel.NewWaiter()

	var active, peak int32
	for i := 0; i < n; i++ {
		m.Run(func() error {
			cur := atomic.AddInt32(&active, 1)
			for {
				p := atomic.LoadInt32(&peak)
				if cur <= p || atomic.CompareAndSwapInt32(&peak, p, cur) {
					break
				}
			}
			atomic.AddInt32(&active, -1)
			return nil
		}, w)
	}
	// Drain errors so workers do not block on the unbuffered errch.
	go func() {
		for range w.Err() {
		}
	}()
	m.Close()
	w.Wait()
	if got := atomic.LoadInt32(&peak); got > int32(jobs) {
		t.Errorf("peak concurrency = %d, want <= %d", got, jobs)
	}
}

// TestManager_ErrorAggregation verifies that multiple failing tasks surface
// on the error channel and that the channel is closed after Wait.
func TestManager_ErrorAggregation(t *testing.T) {
	t.Parallel()
	m := parallel.New(8)
	w := parallel.NewWaiter()
	errA := errors.New("a")
	errB := errors.New("b")
	errC := errors.New("c")
	tasks := []error{errA, errB, errC, nil, nil}
	for _, e := range tasks {
		e := e
		m.Run(func() error { return e }, w)
	}
	var got []error
	var drainWG sync.WaitGroup
	drainWG.Add(1)
	go func() {
		defer drainWG.Done()
		for err := range w.Err() {
			got = append(got, err)
		}
	}()
	m.Close()
	w.Wait()
	drainWG.Wait()
	if len(got) != 3 {
		t.Fatalf("got %d errors, want 3: %v", len(got), got)
	}
	seen := map[error]bool{}
	for _, e := range got {
		seen[e] = true
	}
	for _, want := range []error{errA, errB, errC} {
		if !seen[want] {
			t.Errorf("error %v not in channel output %v", want, got)
		}
	}
}

// TestNew_DefaultsToMinWorkers verifies that worker count 0 or 1 is bumped
// up to minNumWorkers.
func TestNew_DefaultsToMinWorkers(t *testing.T) {
	t.Parallel()
	for _, wc := range []int{0, 1} {
		wc := wc
		t.Run("", func(t *testing.T) {
			t.Parallel()
			m := parallel.New(wc)
			w := parallel.NewWaiter()
			var count int32
			for i := 0; i < 4; i++ {
				m.Run(func() error {
					atomic.AddInt32(&count, 1)
					return nil
				}, w)
			}
			go func() {
				for range w.Err() {
				}
			}()
			m.Close()
			w.Wait()
			if got := atomic.LoadInt32(&count); got != 4 {
				t.Errorf("ran %d tasks, want 4", got)
			}
		})
	}
}

// TestWaiter_ChannelClosedAfterWait verifies that after Wait returns, the
// error channel is closed (range loop terminates).
func TestWaiter_ChannelClosedAfterWait(t *testing.T) {
	t.Parallel()
	m := parallel.New(2)
	w := parallel.NewWaiter()
	m.Run(func() error { return nil }, w)
	go func() {
		for range w.Err() {
		}
	}()
	m.Close()
	w.Wait()
	if _, ok := <-w.Err(); ok {
		t.Errorf("Err() channel should be closed after Wait")
	}
}
