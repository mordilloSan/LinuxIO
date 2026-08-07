package filebrowser

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func TestArchiveResourceLimiterQueuesAndReleases(t *testing.T) {
	var limiter archiveResourceLimiter
	release, err := limiter.acquire(context.Background(), 1)
	if err != nil {
		t.Fatalf("first acquire returned error: %v", err)
	}

	acquired := make(chan func(), 1)
	go func() {
		nextRelease, acquireErr := limiter.acquire(context.Background(), 1)
		if acquireErr != nil {
			t.Errorf("queued acquire returned error: %v", acquireErr)
			return
		}
		acquired <- nextRelease
	}()

	select {
	case nextRelease := <-acquired:
		nextRelease()
		t.Fatal("queued acquire completed before resource was released")
	case <-time.After(50 * time.Millisecond):
	}

	release()
	select {
	case nextRelease := <-acquired:
		nextRelease()
	case <-time.After(250 * time.Millisecond):
		t.Fatal("queued acquire did not complete after release")
	}
}

func TestArchiveResourceLimiterCancelsWhileWaiting(t *testing.T) {
	var limiter archiveResourceLimiter
	release, err := limiter.acquire(context.Background(), 1)
	if err != nil {
		t.Fatalf("first acquire returned error: %v", err)
	}
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		nextRelease, acquireErr := limiter.acquire(ctx, 1)
		if nextRelease != nil {
			nextRelease()
		}
		errCh <- acquireErr
	}()

	cancel()
	select {
	case acquireErr := <-errCh:
		if !errors.Is(acquireErr, context.Canceled) {
			t.Fatalf("acquire error = %v, want context.Canceled", acquireErr)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("queued acquire did not return after cancellation")
	}
}

func TestParseChmodBatchRequest(t *testing.T) {
	tests := []struct {
		name          string
		req           apischema.FileChmodBatchRequest
		wantPaths     []string
		wantMode      os.FileMode
		wantOwner     string
		wantGroup     string
		wantRecursive bool
		wantErr       bool
	}{
		{
			name:    "missing mode",
			req:     apischema.FileChmodBatchRequest{Paths: []string{"/tmp/file"}},
			wantErr: true,
		},
		{
			name:    "missing paths",
			req:     apischema.FileChmodBatchRequest{Mode: "0644"},
			wantErr: true,
		},
		{
			name:    "invalid mode",
			req:     apischema.FileChmodBatchRequest{Paths: []string{"/tmp/file"}, Mode: "rw-r--r--"},
			wantErr: true,
		},
		{
			name:      "paths and mode only",
			req:       apischema.FileChmodBatchRequest{Paths: []string{"/tmp/file"}, Mode: "0644"},
			wantPaths: []string{"/tmp/file"},
			wantMode:  0o644,
		},
		{
			name:      "owner only",
			req:       apischema.FileChmodBatchRequest{Paths: []string{"/tmp/file"}, Mode: "0644", Owner: "miguel"},
			wantPaths: []string{"/tmp/file"},
			wantMode:  0o644,
			wantOwner: "miguel",
		},
		{
			name:      "owner and group",
			req:       apischema.FileChmodBatchRequest{Paths: []string{"/tmp/file"}, Mode: "0644", Owner: "miguel", Group: "staff"},
			wantPaths: []string{"/tmp/file"},
			wantMode:  0o644,
			wantOwner: "miguel",
			wantGroup: "staff",
		},
		{
			name:          "many paths owner group recursive",
			req:           apischema.FileChmodBatchRequest{Paths: []string{"/tmp/file", "/tmp/dir"}, Mode: "0644", Owner: "miguel", Group: "staff", Recursive: new(true)},
			wantPaths:     []string{"/tmp/file", "/tmp/dir"},
			wantMode:      0o644,
			wantOwner:     "miguel",
			wantGroup:     "staff",
			wantRecursive: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths, mode, owner, group, recursive, err := parseChmodBatchRequest(tt.req)
			if tt.wantErr {
				if err == nil {
					t.Fatal("parseChmodBatchRequest returned nil error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseChmodBatchRequest returned error: %v", err)
			}
			if !slices.Equal(paths, tt.wantPaths) || mode != tt.wantMode || owner != tt.wantOwner || group != tt.wantGroup || recursive != tt.wantRecursive {
				t.Fatalf("parseChmodBatchRequest() = (%q, %v, %q, %q, %v), want (%q, %v, %q, %q, %v)", paths, mode, owner, group, recursive, tt.wantPaths, tt.wantMode, tt.wantOwner, tt.wantGroup, tt.wantRecursive)
			}
		})
	}
}
