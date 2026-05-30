package filebrowser

import (
	"context"
	"errors"
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

func TestParseChmodRequest(t *testing.T) {
	tests := []struct {
		name          string
		req           apischema.FileChmodRequest
		wantPath      string
		wantMode      string
		wantOwner     string
		wantGroup     string
		wantRecursive bool
		wantErr       bool
	}{
		{
			name:    "missing mode",
			req:     apischema.FileChmodRequest{Path: "/tmp/file"},
			wantErr: true,
		},
		{
			name:     "path and mode only",
			req:      apischema.FileChmodRequest{Path: "/tmp/file", Mode: "0644"},
			wantPath: "/tmp/file",
			wantMode: "0644",
		},
		{
			name:      "owner only",
			req:       apischema.FileChmodRequest{Path: "/tmp/file", Mode: "0644", Owner: "miguel"},
			wantPath:  "/tmp/file",
			wantMode:  "0644",
			wantOwner: "miguel",
		},
		{
			name:      "owner and group",
			req:       apischema.FileChmodRequest{Path: "/tmp/file", Mode: "0644", Owner: "miguel", Group: "staff"},
			wantPath:  "/tmp/file",
			wantMode:  "0644",
			wantOwner: "miguel",
			wantGroup: "staff",
		},
		{
			name:          "owner group recursive",
			req:           apischema.FileChmodRequest{Path: "/tmp/file", Mode: "0644", Owner: "miguel", Group: "staff", Recursive: new(true)},
			wantPath:      "/tmp/file",
			wantMode:      "0644",
			wantOwner:     "miguel",
			wantGroup:     "staff",
			wantRecursive: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, mode, owner, group, recursive, err := parseChmodRequest(tt.req)
			if tt.wantErr {
				if err == nil {
					t.Fatal("parseChmodRequest returned nil error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseChmodRequest returned error: %v", err)
			}
			if path != tt.wantPath || mode != tt.wantMode || owner != tt.wantOwner || group != tt.wantGroup || recursive != tt.wantRecursive {
				t.Fatalf("parseChmodRequest() = (%q, %q, %q, %q, %v), want (%q, %q, %q, %q, %v)", path, mode, owner, group, recursive, tt.wantPath, tt.wantMode, tt.wantOwner, tt.wantGroup, tt.wantRecursive)
			}
		})
	}
}
