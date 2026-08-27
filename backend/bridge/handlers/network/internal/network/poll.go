package network

import (
	"context"
	"time"
)

func pollUntil(ctx context.Context, timeout, interval time.Duration, probe func() (bool, error), timeoutErr error) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		ok, err := probe()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return timeoutErr
		case <-ticker.C:
		}
	}
}
