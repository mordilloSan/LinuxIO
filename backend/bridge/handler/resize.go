package handler

import "context"

// ResizeEvent carries terminal size updates for bidirectional handlers.
type ResizeEvent struct {
	Cols uint16
	Rows uint16
}

type resizeContextKey struct{}

// WithResizeChannel attaches a resize channel to the context for handlers to consume.
func WithResizeChannel(ctx context.Context, ch chan ResizeEvent) context.Context {
	return context.WithValue(ctx, resizeContextKey{}, ch)
}

// ResizeChannel retrieves the resize channel from context.
func ResizeChannel(ctx context.Context) (<-chan ResizeEvent, bool) {
	ch, ok := ctx.Value(resizeContextKey{}).(chan ResizeEvent)
	return ch, ok
}
