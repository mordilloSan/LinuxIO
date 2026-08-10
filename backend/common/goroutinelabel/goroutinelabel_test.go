package goroutinelabel

import (
	"bytes"
	"context"
	"runtime/pprof"
	"strings"
	"sync"
	"testing"
)

// labelsOf reads back the labels carried by ctx. With sets the calling
// goroutine's labels from this same set, so it stands in for both.
func labelsOf(ctx context.Context) map[string]string {
	got := map[string]string{}
	pprof.ForLabels(ctx, func(k, v string) bool {
		got[k] = v
		return true
	})
	return got
}

// TestWithLabelsReachTraceback is the property the package exists for: Go 1.27
// prints goroutine labels in traceback headers, and a goroutine that never
// labeled itself still shows the labels of the goroutine that spawned it.
func TestWithLabelsReachTraceback(t *testing.T) {
	labeled := make(chan struct{})
	release := make(chan struct{})
	var wg sync.WaitGroup

	wg.Go(func() {
		With(context.Background(), "session_id", "sess-1", "user", "alice")
		// An unlabeled child: it must inherit the set above.
		wg.Go(func() {
			close(labeled)
			<-release
		})
		<-release
	})

	<-labeled // both goroutines exist and are labeled before we dump

	var buf bytes.Buffer
	if err := pprof.Lookup("goroutine").WriteTo(&buf, 2); err != nil {
		t.Fatalf("dump goroutines: %v", err)
	}
	close(release)
	wg.Wait()

	dump := buf.String()
	// pprof quotes label values that are not bare identifiers, so the hyphen
	// in sess-1 makes this `session_id: "sess-1"` in the header.
	if got := strings.Count(dump, `session_id: "sess-1"`); got != 2 {
		t.Errorf("labeled goroutines in traceback = %d, want 2 (parent + inheriting child)\n%s", got, dump)
	}
	if !strings.Contains(dump, "user: alice") {
		t.Errorf("traceback missing user label:\n%s", dump)
	}
}

func TestWithOverwritesInheritedKey(t *testing.T) {
	// A promoted queued task corrects identity inherited from the goroutine
	// that happened to start it; the later value must win.
	ctx := With(context.Background(), "session_id", "stale", "route", "keep")
	got := labelsOf(With(ctx, "session_id", "fresh"))

	if got["session_id"] != "fresh" {
		t.Errorf("session_id = %q, want %q", got["session_id"], "fresh")
	}
	if got["route"] != "keep" {
		t.Errorf("route = %q, want %q (unrelated keys must survive)", got["route"], "keep")
	}
}

func TestWithMalformedInputIsInert(t *testing.T) {
	// pprof.Labels panics on odd-length input. A diagnostic must not.
	for _, kv := range [][]string{nil, {}, {"lonely"}, {"a", "b", "c"}} {
		ctx := context.Background()
		if got := With(ctx, kv...); got != ctx {
			t.Errorf("With(%v) returned a derived context; want the original untouched", kv)
		}
	}
}
