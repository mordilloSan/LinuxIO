package logging

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/logguard"
)

// TestLogMessagesFollowTheContract enforces the message contract on every slog
// call in the backend: constant lowercase text, no tags or trailing
// punctuation, and the attribute key "error".
func TestLogMessagesFollowTheContract(t *testing.T) {
	checkBackend(t, logguard.MessageStyle)
}

// TestBackendReturnsOrLogsErrorsNotBoth extends "log or return, never both"
// from the bridge handlers to the whole backend.
func TestBackendReturnsOrLogsErrorsNotBoth(t *testing.T) {
	checkBackend(t, logguard.LogThenReturn)
}

func checkBackend(t *testing.T, check func(path string) ([]string, error)) {
	t.Helper()
	err := filepath.WalkDir("../..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" || d.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		violations, err := check(path)
		for _, line := range violations {
			t.Errorf("%s", line)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
