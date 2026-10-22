package goroutinelabel

import (
	"bytes"
	"context"
	"runtime/pprof"
	"strings"
	"sync"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
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

// TestWithSessionLabelsReachTraceback is the property the package exists for:
// safe ownership labels reach tracebacks and child goroutines, while the
// authentication cookie and username never do.
func TestWithSessionLabelsReachTraceback(t *testing.T) {
	const credential = "0123456789abcdef0123456789abcdef"
	labeled := make(chan struct{})
	release := make(chan struct{})
	var wg sync.WaitGroup

	wg.Go(func() {
		WithSession(context.Background(), credential, 1000, "component", "bridge")
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
	refLabel := `session_ref: "` + session.DiagnosticRef(credential) + `"`
	if got := strings.Count(dump, refLabel); got != 2 {
		t.Errorf("labeled goroutines in traceback = %d, want 2 (parent + inheriting child)\n%s", got, dump)
	}
	if strings.Contains(dump, credential) {
		t.Errorf("traceback contains session credential:\n%s", dump)
	}
	if strings.Contains(dump, "session_id:") || strings.Contains(dump, "user:") {
		t.Errorf("traceback contains a prohibited identity label:\n%s", dump)
	}
	if !strings.Contains(dump, "component: bridge") {
		t.Errorf("traceback missing safe component label:\n%s", dump)
	}
	if got := strings.Count(dump, "uid: 1000"); got != 2 {
		t.Errorf("UID labels in traceback = %d, want 2 (parent + inheriting child)\n%s", got, dump)
	}
}

func TestWithOverwritesInheritedKey(t *testing.T) {
	// A promoted queued task corrects identity inherited from the goroutine
	// that happened to start it; the later value must win.
	ctx := WithSession(context.Background(), "stale", 1000, "route", "keep")
	got := labelsOf(WithSession(ctx, "fresh", 1001))

	if want := session.DiagnosticRef("fresh"); got["session_ref"] != want {
		t.Errorf("session_ref = %q, want %q", got["session_ref"], want)
	}
	if got["route"] != "keep" {
		t.Errorf("route = %q, want %q (unrelated keys must survive)", got["route"], "keep")
	}
	if got["uid"] != "1001" {
		t.Errorf("uid = %q, want %q", got["uid"], "1001")
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
