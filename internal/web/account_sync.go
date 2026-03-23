package web

import (
	"context"
	"sync"
)

type accountSyncCall struct {
	done  chan struct{}
	count int
	err   error
}

type accountSyncTracker struct {
	mu    sync.Mutex
	calls map[int64]*accountSyncCall
}

func newAccountSyncTracker() *accountSyncTracker {
	return &accountSyncTracker{calls: make(map[int64]*accountSyncCall)}
}

func (t *accountSyncTracker) Do(ctx context.Context, accountID int64, fn func(context.Context) (int, error)) (int, error) {
	if t == nil || accountID <= 0 {
		return fn(ctx)
	}

	t.mu.Lock()
	if call, ok := t.calls[accountID]; ok {
		t.mu.Unlock()
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-call.done:
			return call.count, call.err
		}
	}

	call := &accountSyncCall{done: make(chan struct{})}
	t.calls[accountID] = call
	t.mu.Unlock()

	count, err := fn(ctx)
	call.count = count
	call.err = err
	close(call.done)

	t.mu.Lock()
	delete(t.calls, accountID)
	t.mu.Unlock()

	return count, err
}
