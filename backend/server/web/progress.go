package web

import (
	"sync"
)

// ProgressUpdate represents a generic operation progress update (download, upload, compression, etc.)
type ProgressUpdate struct {
	Type           string  `json:"type"`           // e.g. "download_progress", "download_ready", "compression_progress", "compression_complete", "upload_progress", ...
	Percent        float64 `json:"percent"`        // 0-100
	BytesProcessed int64   `json:"bytesProcessed"` // Bytes processed so far
	TotalBytes     int64   `json:"totalBytes"`     // Total bytes to process (if known)
}

// ProgressBroadcaster manages progress update handlers for operations (keyed by "sessionID:requestID")
type ProgressBroadcaster struct {
	mu       sync.RWMutex
	handlers map[string]func(ProgressUpdate) // key: "sessionID:requestID"
}

// GlobalProgressBroadcaster is a singleton for managing download progress
var GlobalProgressBroadcaster = &ProgressBroadcaster{
	handlers: make(map[string]func(ProgressUpdate)),
}

// Register adds a progress handler for a specific session and request ID
func (pb *ProgressBroadcaster) Register(key string, handler func(ProgressUpdate)) {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	pb.handlers[key] = handler
}

// Unregister removes a progress handler
func (pb *ProgressBroadcaster) Unregister(key string) {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	delete(pb.handlers, key)
}

// Send sends a progress update to the registered handler
func (pb *ProgressBroadcaster) Send(key string, update ProgressUpdate) {
	pb.mu.RLock()
	defer pb.mu.RUnlock()
	if handler, ok := pb.handlers[key]; ok {
		handler(update)
	}
}
