package ipc

import "errors"

// Common errors for handlers
var (
	ErrInvalidArgs = errors.New("invalid arguments")
	ErrHandlerNotFound    = errors.New("handler not found")
)

// Events allows handlers to send various types of data back to the client.
// All methods are non-blocking and thread-safe.
//
// The typical flow for a handler is:
//  1. Emit Progress updates during long operations (optional)
//  2. Emit Data chunks for streaming binary data (optional)
//  3. Emit Result with the final outcome (required for success)
//  4. Return error for failures (framework will send error automatically)
//
// The stream is automatically closed by the framework after Execute() returns.
// Handlers can call Close() explicitly to terminate early with a reason.
type Events interface {
	// Data sends a binary chunk to the client (OpStreamData)
	// Use for raw file data, terminal output, etc.
	// Returns error if the stream is closed or write fails.
	Data(chunk []byte) error

	// Progress sends a progress update (OpStreamProgress)
	// The progress object will be JSON-serialized.
	// Use for file upload/download progress, long-running operations.
	Progress(progress any) error

	// Result sends the final result (OpStreamResult)
	// The result object will be JSON-serialized.
	// Note: This does NOT close the stream automatically.
	// The framework closes the stream when Execute() returns.
	Result(result any) error

	// Error sends an error to the client (OpStreamResult with error status)
	// This does NOT close the stream - allows multiple errors in batch operations.
	// Most handlers should just return an error from Execute() instead.
	// Use this for batch operations where you want to report errors but continue.
	Error(err error, code int) error

	// Close terminates the stream immediately (OpStreamClose)
	// Optional reason can be provided for debugging.
	// Most handlers don't need to call this - the framework closes automatically.
	// Use this to abort early or signal explicit termination.
	Close(reason string) error
}
