package web

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestAccountSyncTrackerCoalescesConcurrentCalls(t *testing.T) {
	tracker := newAccountSyncTracker()
	started := make(chan struct{})
	release := make(chan struct{})
	var calls int32

	run := func() (int, error) {
		return tracker.Do(context.Background(), 7, func(context.Context) (int, error) {
			if atomic.AddInt32(&calls, 1) == 1 {
				close(started)
			}
			<-release
			return 42, nil
		})
	}

	type result struct {
		count int
		err   error
	}
	results := make([]result, 1)
	done := make(chan struct{})
	go func() {
		count, err := run()
		results[0] = result{count: count, err: err}
		close(done)
	}()
	<-started

	waitCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	count, err := tracker.Do(waitCtx, 7, func(context.Context) (int, error) {
		atomic.AddInt32(&calls, 1)
		return 7, nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting call = (%d, %v), want deadline exceeded", count, err)
	}
	close(release)
	<-done

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
	for i, result := range results {
		if result.err != nil {
			t.Fatalf("result[%d].err = %v, want nil", i, result.err)
		}
		if result.count != 42 {
			t.Fatalf("result[%d].count = %d, want 42", i, result.count)
		}
	}
}
