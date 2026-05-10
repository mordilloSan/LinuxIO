package ipc

import (
	"context"
)

// Handler processes bridge requests and emits events back to the client.
// All handlers must implement this interface.
//
// Handlers should:
// - Use emit.Result() to send the final result
// - Use emit.Progress() for long-running operations
// - Use emit.Data() for streaming binary data
// - Return an error if the operation fails (will be sent to client automatically)
type Handler interface {
	Execute(ctx context.Context, args []string, emit Events) error
}

// HandlerFunc is a function adapter for Handler interface.
// It allows regular functions to be used as handlers.
type HandlerFunc func(ctx context.Context, args []string, emit Events) error

func (f HandlerFunc) Execute(ctx context.Context, args []string, emit Events) error {
	return f(ctx, args, emit)
}
