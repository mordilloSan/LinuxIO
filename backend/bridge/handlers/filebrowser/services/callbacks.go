package services

import "errors"

// Errors shared by the file and archive operations and their callers.
var (
	ErrAborted           = errors.New("operation aborted")
	ErrUnsupportedFormat = errors.New("unsupported format")
)

// OperationCallbacks provides common callbacks for long-running operations.
// All fields are optional - nil callbacks are safely ignored.
type OperationCallbacks struct {
	Progress   func(bytes int64) // Called with bytes processed
	Cancel     func() bool       // Returns true if operation should abort
	OnComplete func(path string) // Called when an item completes
}

// ReportProgress safely calls the progress callback if set.
func (o *OperationCallbacks) ReportProgress(bytes int64) {
	if o != nil && o.Progress != nil {
		o.Progress(bytes)
	}
}

// IsCancelled safely checks the cancel function if set.
func (o *OperationCallbacks) IsCancelled() bool {
	if o != nil && o.Cancel != nil {
		return o.Cancel()
	}
	return false
}

// ReportComplete safely calls the completion callback if set.
func (o *OperationCallbacks) ReportComplete(path string) {
	if o != nil && o.OnComplete != nil {
		o.OnComplete(path)
	}
}
