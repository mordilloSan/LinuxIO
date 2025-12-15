package web

import (
	"context"
	"sync"
)

// OperationCanceller keeps track of cancel functions for long-running operations keyed by progress key.
type OperationCanceller struct {
	mu      sync.Mutex
	cancels map[string]*operationCancelEntry
}

type operationCancelEntry struct {
	cancel context.CancelFunc
}

// GlobalOperationCanceller allows HTTP handlers to register cancellable operations that
// can be aborted later (for example, when the UI cancels a transfer).
var GlobalOperationCanceller = &OperationCanceller{
	cancels: make(map[string]*operationCancelEntry),
}

// Register associates a cancel function with the provided key. It returns a cleanup function
// that must be called when the operation completes naturally to remove the registration.
func (oc *OperationCanceller) Register(key string, cancel context.CancelFunc) func() {
	if key == "" || cancel == nil {
		return func() {}
	}
	entry := &operationCancelEntry{cancel: cancel}
	oc.mu.Lock()
	oc.cancels[key] = entry
	oc.mu.Unlock()
	return func() {
		oc.mu.Lock()
		if current, ok := oc.cancels[key]; ok && current == entry {
			delete(oc.cancels, key)
		}
		oc.mu.Unlock()
	}
}

// Cancel aborts the operation associated with the provided key, if any.
func (oc *OperationCanceller) Cancel(key string) {
	if key == "" {
		return
	}
	oc.mu.Lock()
	entry := oc.cancels[key]
	delete(oc.cancels, key)
	oc.mu.Unlock()
	if entry != nil && entry.cancel != nil {
		entry.cancel()
	}
}
