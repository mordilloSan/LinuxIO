package dbusclient

import "context"

func closeSignalsForTest(ctx context.Context) error {
	ctx = requireContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}

	signals.mu.Lock()
	for sub := range signals.subs {
		sub.closeOnce.Do(func() {
			// Closing the shared connection drops the bus-side match rules, so
			// individual subscriptions intentionally retain a nil closeErr.
			close(sub.ch)
		})
	}
	signals.subs = nil
	signals.matchRefs = nil
	signals.raw = nil

	if signals.conn == nil {
		signals.mu.Unlock()
		return nil
	}

	conn := signals.conn
	signals.conn = nil
	signals.mu.Unlock()
	return conn.Close()
}
