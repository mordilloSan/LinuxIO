package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/config"
)

func TestParseComponentVersionOutput(t *testing.T) {
	tests := []struct {
		name      string
		component string
		output    string
		want      string
		ok        bool
	}{
		{
			name:      "bridge",
			component: "LinuxIO Bridge",
			output:    "LinuxIO Bridge v1.2.3\n",
			want:      "v1.2.3",
			ok:        true,
		},
		{
			name:      "auth",
			component: "LinuxIO Auth",
			output:    "LinuxIO Auth v1.2.3\n",
			want:      "v1.2.3",
			ok:        true,
		},
		{
			name:      "cli",
			component: "LinuxIO CLI",
			output:    "\nLinuxIO CLI v1.2.3\n",
			want:      "v1.2.3",
			ok:        true,
		},
		{
			name:      "malformed",
			component: "LinuxIO CLI",
			output:    "version=v1.2.3\n",
			ok:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseComponentVersionOutput(tt.component, []byte(tt.output))
			if ok != tt.ok {
				t.Fatalf("expected ok=%v, got %v", tt.ok, ok)
			}
			if got != tt.want {
				t.Fatalf("expected version %q, got %q", tt.want, got)
			}
		})
	}
}

func TestGetComponentVersionsAllSuccess(t *testing.T) { //nolint:gocognit
	restore := stubVersionCollector(t, "v9.9.9", 50*time.Millisecond, nil)
	defer restore()

	var mu sync.Mutex
	var calls []probeCall
	runComponentVersionCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		mu.Lock()
		calls = append(calls, probeCall{name: filepath.Base(name), args: slices.Clone(args)})
		mu.Unlock()

		switch filepath.Base(name) {
		case "linuxio-bridge":
			return []byte("LinuxIO Bridge v2.3.4\n"), nil
		case "linuxio-auth":
			return []byte("LinuxIO Auth v3.4.5\n"), nil
		case "linuxio":
			return []byte("LinuxIO CLI v4.5.6\n"), nil
		default:
			return nil, errors.New("unexpected binary")
		}
	}

	got := getComponentVersions(context.Background())

	want := map[string]string{
		"LinuxIO Web Server": "v9.9.9",
		"LinuxIO Bridge":     "v2.3.4",
		"LinuxIO Auth":       "v3.4.5",
		"LinuxIO CLI":        "v4.5.6",
	}
	assertVersionMap(t, got, want)

	mu.Lock()
	gotCalls := slices.Clone(calls)
	mu.Unlock()
	slices.SortFunc(gotCalls, func(a, b probeCall) int {
		if a.name != b.name {
			if a.name < b.name {
				return -1
			}
			return 1
		}
		switch {
		case len(a.args) < len(b.args):
			return -1
		case len(a.args) > len(b.args):
			return 1
		default:
			for i := range a.args {
				if a.args[i] == b.args[i] {
					continue
				}
				if a.args[i] < b.args[i] {
					return -1
				}
				return 1
			}
			return 0
		}
	})

	wantCalls := []probeCall{
		{name: "linuxio", args: []string{"version", "--self"}},
		{name: "linuxio-auth", args: []string{"version"}},
		{name: "linuxio-bridge", args: []string{"version"}},
	}
	if !slices.EqualFunc(gotCalls, wantCalls, func(a, b probeCall) bool {
		return a.name == b.name && slices.Equal(a.args, b.args)
	}) {
		t.Fatalf("unexpected probe calls: %#v", gotCalls)
	}
}

func TestGetComponentVersionsOmitsFailedProbes(t *testing.T) {
	restore := stubVersionCollector(t, "v9.9.9", 50*time.Millisecond, func(ctx context.Context, name string, args ...string) ([]byte, error) {
		switch filepath.Base(name) {
		case "linuxio-bridge":
			return []byte("LinuxIO Bridge v2.3.4\n"), nil
		case "linuxio-auth":
			return nil, errors.New("boom")
		case "linuxio":
			return []byte("version=v4.5.6\n"), nil
		default:
			return nil, errors.New("unexpected binary")
		}
	})
	defer restore()

	got := getComponentVersions(context.Background())
	want := map[string]string{
		"LinuxIO Web Server": "v9.9.9",
		"LinuxIO Bridge":     "v2.3.4",
	}
	assertVersionMap(t, got, want)
}

func TestGetComponentVersionsTimeoutsAreBounded(t *testing.T) {
	restore := stubVersionCollector(t, "v9.9.9", 10*time.Millisecond, func(ctx context.Context, name string, args ...string) ([]byte, error) {
		switch filepath.Base(name) {
		case "linuxio-bridge":
			return []byte("LinuxIO Bridge v2.3.4\n"), nil
		case "linuxio-auth":
			<-ctx.Done()
			return nil, ctx.Err()
		case "linuxio":
			return []byte("LinuxIO CLI v4.5.6\n"), nil
		default:
			return nil, errors.New("unexpected binary")
		}
	})
	defer restore()

	got := getComponentVersions(context.Background())
	want := map[string]string{
		"LinuxIO Web Server": "v9.9.9",
		"LinuxIO Bridge":     "v2.3.4",
		"LinuxIO CLI":        "v4.5.6",
	}
	assertVersionMap(t, got, want)
}

func TestGetComponentVersionsRespectsCanceledParentContext(t *testing.T) {
	restore := stubVersionCollector(t, "v9.9.9", 50*time.Millisecond, func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, ctx.Err()
	})
	defer restore()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got := getComponentVersions(ctx)
	want := map[string]string{
		"LinuxIO Web Server": "v9.9.9",
	}
	assertVersionMap(t, got, want)
}

func TestVersionHandlerReturnsVersionsAndUsesSelfProbe(t *testing.T) {
	restore := stubVersionCollector(t, "v9.9.9", 50*time.Millisecond, nil)
	defer restore()

	var mu sync.Mutex
	var calls []probeCall
	runComponentVersionCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		mu.Lock()
		calls = append(calls, probeCall{name: filepath.Base(name), args: slices.Clone(args)})
		mu.Unlock()

		switch filepath.Base(name) {
		case "linuxio-bridge":
			return []byte("LinuxIO Bridge v2.3.4\n"), nil
		case "linuxio-auth":
			return []byte("LinuxIO Auth v3.4.5\n"), nil
		case "linuxio":
			return []byte("LinuxIO CLI v4.5.6\n"), nil
		default:
			return nil, errors.New("unexpected binary")
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	rec := httptest.NewRecorder()
	h := &Handlers{}

	h.Version(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	want := map[string]string{
		"LinuxIO Web Server": "v9.9.9",
		"LinuxIO Bridge":     "v2.3.4",
		"LinuxIO Auth":       "v3.4.5",
		"LinuxIO CLI":        "v4.5.6",
	}
	assertVersionMap(t, got, want)

	mu.Lock()
	defer mu.Unlock()
	for _, call := range calls {
		if call.name == "linuxio" && !slices.Equal(call.args, []string{"version", "--self"}) {
			t.Fatalf("linuxio CLI probe used unexpected args: %v", call.args)
		}
	}
}

func TestVersionHandlerReturnsPartialResultsOnProbeFailure(t *testing.T) {
	restore := stubVersionCollector(t, "v9.9.9", 50*time.Millisecond, func(ctx context.Context, name string, args ...string) ([]byte, error) {
		switch filepath.Base(name) {
		case "linuxio-bridge":
			return []byte("LinuxIO Bridge v2.3.4\n"), nil
		case "linuxio-auth":
			return nil, errors.New("boom")
		case "linuxio":
			return []byte("bad output\n"), nil
		default:
			return nil, errors.New("unexpected binary")
		}
	})
	defer restore()

	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	rec := httptest.NewRecorder()
	h := &Handlers{}

	h.Version(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	want := map[string]string{
		"LinuxIO Web Server": "v9.9.9",
		"LinuxIO Bridge":     "v2.3.4",
	}
	assertVersionMap(t, got, want)
}

type probeCall struct {
	name string
	args []string
}

func stubVersionCollector(
	t *testing.T,
	version string,
	timeout time.Duration,
	runner func(ctx context.Context, name string, args ...string) ([]byte, error),
) func() {
	t.Helper()

	oldVersion := config.Version
	oldTimeout := componentVersionCommandTimeout
	oldRunner := runComponentVersionCommand

	config.Version = version
	componentVersionCommandTimeout = timeout
	if runner != nil {
		runComponentVersionCommand = runner
	}

	return func() {
		config.Version = oldVersion
		componentVersionCommandTimeout = oldTimeout
		runComponentVersionCommand = oldRunner
	}
}

func assertVersionMap(t *testing.T, got, want map[string]string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("expected %d entries, got %d: %#v", len(want), len(got), got)
	}

	for key, wantValue := range want {
		gotValue, ok := got[key]
		if !ok {
			t.Fatalf("missing version key %q in %#v", key, got)
		}
		if gotValue != wantValue {
			t.Fatalf("expected %s=%q, got %q", key, wantValue, gotValue)
		}
	}
}
