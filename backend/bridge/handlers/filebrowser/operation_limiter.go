package filebrowser

import (
	"context"
	"sync"
	"time"
)

var heavyArchiveLimiter archiveResourceLimiter

type archiveResourceLimiter struct {
	mu     sync.Mutex
	active int
}

func (l *archiveResourceLimiter) acquire(ctx context.Context, max int) (func(), error) {
	if max <= 0 {
		max = 1
	}
	for {
		l.mu.Lock()
		if l.active < max {
			l.active++
			l.mu.Unlock()
			return func() {
				l.mu.Lock()
				if l.active > 0 {
					l.active--
				}
				l.mu.Unlock()
			}, nil
		}
		l.mu.Unlock()

		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, context.Canceled
		case <-timer.C:
		}
	}
}
