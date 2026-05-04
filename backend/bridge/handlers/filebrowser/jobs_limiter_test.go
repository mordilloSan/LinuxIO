package filebrowser

import (
	"context"
	"errors"
	"testing"
	"time"
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

func TestParseChmodArgs(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantPath      string
		wantMode      string
		wantOwner     string
		wantGroup     string
		wantRecursive bool
		wantErr       bool
	}{
		{
			name:    "missing mode",
			args:    []string{"/tmp/file"},
			wantErr: true,
		},
		{
			name:     "path and mode only",
			args:     []string{"/tmp/file", "0644"},
			wantPath: "/tmp/file",
			wantMode: "0644",
		},
		{
			name:      "owner only",
			args:      []string{"/tmp/file", "0644", "miguel"},
			wantPath:  "/tmp/file",
			wantMode:  "0644",
			wantOwner: "miguel",
		},
		{
			name:          "recursive legacy third arg",
			args:          []string{"/tmp/file", "0644", "true"},
			wantPath:      "/tmp/file",
			wantMode:      "0644",
			wantRecursive: true,
		},
		{
			name:      "owner and group",
			args:      []string{"/tmp/file", "0644", "miguel", "staff"},
			wantPath:  "/tmp/file",
			wantMode:  "0644",
			wantOwner: "miguel",
			wantGroup: "staff",
		},
		{
			name:          "owner group recursive",
			args:          []string{"/tmp/file", "0644", "miguel", "staff", "true"},
			wantPath:      "/tmp/file",
			wantMode:      "0644",
			wantOwner:     "miguel",
			wantGroup:     "staff",
			wantRecursive: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, mode, owner, group, recursive, err := parseChmodArgs(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatal("parseChmodArgs returned nil error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseChmodArgs returned error: %v", err)
			}
			if path != tt.wantPath || mode != tt.wantMode || owner != tt.wantOwner || group != tt.wantGroup || recursive != tt.wantRecursive {
				t.Fatalf("parseChmodArgs() = (%q, %q, %q, %q, %v), want (%q, %q, %q, %q, %v)", path, mode, owner, group, recursive, tt.wantPath, tt.wantMode, tt.wantOwner, tt.wantGroup, tt.wantRecursive)
			}
		})
	}
}
