//go:build !pprofdebug

// Package debugserver serves net/http/pprof on a loopback listener in debug
// builds. Production builds (without the pprofdebug tag) compile this no-op
// variant, so no profiling endpoint ever ships.
package debugserver

// Start is a no-op without the pprofdebug build tag.
func Start(string) {}
