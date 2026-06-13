package watchtower

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRunArgs(t *testing.T) {
	tests := []struct {
		name    string
		target  Target
		opts    Options
		want    []string
		wantErr bool
	}{
		{
			name:   "all containers monitor only",
			target: Target{All: true},
			opts:   Options{MonitorOnly: true},
			want:   []string{"--run-once", "--porcelain", "v1", "--monitor-only"},
		},
		{
			name:   "named containers update with cleanup",
			target: Target{Names: []string{"nginx", "redis"}},
			opts:   Options{Cleanup: true},
			want:   []string{"--run-once", "--porcelain", "v1", "--cleanup", "nginx", "redis"},
		},
		{
			name:   "names are regex-quoted and slash-trimmed",
			target: Target{Names: []string{"/app.service", " spaced "}},
			want:   []string{"--run-once", "--porcelain", "v1", `app\.service`, "spaced"},
		},
		{
			name:    "empty selection is rejected",
			target:  Target{},
			wantErr: true,
		},
		{
			name:    "blank names are rejected",
			target:  Target{Names: []string{"", "  ", "/"}},
			wantErr: true,
		},
		{
			name:    "all combined with names is rejected",
			target:  Target{All: true, Names: []string{"nginx"}},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := runArgs(tc.target, tc.opts)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("runArgs = %v, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("runArgs: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("runArgs = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestAcquireLockExcludesSecondHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "watchtower.lock")

	release, err := acquireLock(context.Background(), path)
	if err != nil {
		t.Fatalf("first acquireLock: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, lockErr := acquireLock(ctx, path); lockErr == nil {
		t.Fatal("second acquireLock succeeded while lock was held")
	}

	release()
	release2, err := acquireLock(context.Background(), path)
	if err != nil {
		t.Fatalf("acquireLock after release: %v", err)
	}
	release2()
}

func TestLastLine(t *testing.T) {
	if got := lastLine("a\nb\n\n"); got != "b" {
		t.Fatalf("lastLine = %q, want %q", got, "b")
	}
	if got := lastLine("  \n"); got != "no error output" {
		t.Fatalf("lastLine = %q, want %q", got, "no error output")
	}
}
