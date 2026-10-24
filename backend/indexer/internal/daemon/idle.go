package daemon

import (
	"context"
	"log/slog"
	"os"
	"time"
)

// The daemon exits after idleGrace without activity: an in-flight request,
// event stream or scan, the webserver's runtime marker, or a recent release of
// any of those. The socket unit starts it again on the next connection.

func runtimeDirectoryExists() bool {
	info, err := os.Stat(runtimeDirectoryMarker)
	return err == nil && info.IsDir()
}

func (d *daemon) markActivity() {
	d.activityMu.Lock()
	d.lastActivity = time.Now()
	d.activityMu.Unlock()
}

func (d *daemon) activityTime() time.Time {
	d.activityMu.RLock()
	last := d.lastActivity
	d.activityMu.RUnlock()
	return last
}

func (d *daemon) stopWhenIdle(ctx context.Context, cancel context.CancelFunc, markerExists func() bool, checkInterval time.Duration) {
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()
	markerHeld := false

	for {
		select {
		case <-ticker.C:
			if markerExists() {
				d.markActivity()
				markerHeld = true
				continue
			}
			if markerHeld {
				d.markActivity()
				markerHeld = false
				continue
			}
			if d.activeRequests.Load() > 0 || d.running.Load() {
				d.markActivity()
				continue
			}
			last := d.activityTime()
			if last.IsZero() {
				continue
			}
			idleFor := time.Since(last)
			if idleFor >= idleGrace {
				slog.Info("idle timeout reached; shutting down daemon", "idle_for", idleFor.Truncate(time.Second), "idle_timeout", idleGrace)
				cancel()
				return
			}
		case <-ctx.Done():
			return
		}
	}
}
