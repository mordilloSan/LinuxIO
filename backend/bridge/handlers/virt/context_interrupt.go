package virt

import "context"

// interruptOnContextDone calls interrupt when ctx is done. The returned stop
// function releases the watcher and waits for a started interrupt to finish.
func interruptOnContextDone(ctx context.Context, interrupt func()) func() {
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(done)
		interrupt()
	})
	return func() {
		if stop() {
			return
		}
		<-done
	}
}
