package ipc

import (
	"io"
)

// ProgressTracker throttles progress writes to a stream based on a byte interval.
// It accumulates incremental byte counts and only writes a progress frame when
// enough bytes have been processed since the last write, or when the total is reached.
type ProgressTracker struct {
	stream    io.Writer
	streamID  uint32
	interval  int64
	last      int64
	processed int64
}

// NewProgressTracker creates a tracker that writes progress every interval bytes.
func NewProgressTracker(stream io.Writer, streamID uint32, interval int64) *ProgressTracker {
	return &ProgressTracker{
		stream:   stream,
		streamID: streamID,
		interval: interval,
	}
}

// Report sends a progress update if enough bytes have been processed since the
// last report, or if processed >= total (final update). When total is 0, no
// progress is written (unknown total). The data parameter is JSON-serialized
// and sent as the progress payload.
func (pt *ProgressTracker) Report(processed, total int64, data any) error {
	if total <= 0 {
		return nil
	}
	if processed-pt.last < pt.interval && processed < total {
		return nil
	}
	pt.last = processed
	return WriteProgress(pt.stream, pt.streamID, data)
}

// AsCallback returns an OperationCallbacks wired to this tracker.
// makeProgress is called to build the handler-specific progress struct from
// the current (processed, total) values. total is the known total byte count.
func (pt *ProgressTracker) AsCallback(cancelFn CancelFunc, makeProgress func(processed, total int64) any, total int64) *OperationCallbacks {
	return &OperationCallbacks{
		Progress: func(n int64) {
			pt.processed += n
			if total <= 0 || makeProgress == nil {
				return
			}
			_ = pt.Report(pt.processed, total, makeProgress(pt.processed, total))
		},
		Cancel: cancelFn,
	}
}
