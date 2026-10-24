// Package goroutinelabel attaches runtime/pprof labels to long-lived goroutines.
//
// Go 1.27 prints pprof labels in the traceback header of every goroutine, so a
// panic dump, a SIGQUIT stack, or any pprof profile can name the component,
// non-secret session reference, route, stream, or task each goroutine belongs
// to. That identity is what stacks alone cannot supply: a stalled bridge prints
// dozens of byte-identical monitorReadOwnedStream frames, and without labels
// there is no way to tell which client each one is waiting on.
//
// Labels carry coarse component and ownership identity, never credentials or
// arbitrary request values. The frame names in a traceback already explain
// the detailed operation.
package goroutinelabel

import (
	"context"
	"runtime/pprof"
	"strconv"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// With attaches kv to the calling goroutine and returns a context carrying the
// same labels. kv is alternating key/value strings.
//
// Labels persist for the goroutine's remaining lifetime and are copied into
// every goroutine it later spawns, so labeling one handler also covers the
// monitors and watchdogs started beneath it. Repeating an inherited key
// overwrites it, which lets a goroutine that owns its own identity correct
// labels it inherited from whichever goroutine happened to start it.
//
// Call it once per goroutine at a coarse boundary — a session, a stream, a
// task — and never inside a per-frame loop: each call allocates a label map.
//
// An odd-length or empty kv returns ctx untouched rather than panicking the way
// pprof.Labels would. Labeling is a diagnostic; a malformed one must never take
// the bridge down.
func With(ctx context.Context, kv ...string) context.Context {
	if len(kv) == 0 || len(kv)%2 != 0 {
		return ctx
	}
	ctx = pprof.WithLabels(ctx, pprof.Labels(kv...))
	pprof.SetGoroutineLabels(ctx)
	return ctx
}

// WithSession attaches a one-way session correlation reference, numeric UID,
// and the safe labels in kv. Callers must never pass the raw session credential
// to With; this helper is the only supported way to identify session ownership
// in a goroutine label.
func WithSession(ctx context.Context, sessionID string, uid uint32, kv ...string) context.Context {
	ref := session.DiagnosticRef(sessionID)
	if ref == "" {
		return With(ctx, kv...)
	}

	labels := make([]string, 0, len(kv)+4)
	labels = append(labels, kv...)
	labels = append(labels,
		"session_ref", ref,
		"uid", strconv.FormatUint(uint64(uid), 10),
	)
	return With(ctx, labels...)
}
