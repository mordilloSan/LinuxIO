package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVerboseDropinState(t *testing.T) {
	original := verboseDropins
	t.Cleanup(func() { verboseDropins = original })
	dir := t.TempDir()
	verboseDropins = []struct {
		path    string
		content string
	}{
		{path: filepath.Join(dir, "webserver.conf")},
		{path: filepath.Join(dir, "indexer.conf")},
	}

	if verboseEnabled() || verbosePartiallyEnabled() {
		t.Fatal("missing drop-ins reported enabled")
	}
	if err := os.WriteFile(verboseDropins[0].path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if verboseEnabled() || !verbosePartiallyEnabled() {
		t.Fatal("one drop-in did not report partial state")
	}
	if err := os.WriteFile(verboseDropins[1].path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if !verboseEnabled() {
		t.Fatal("both drop-ins did not report enabled")
	}
}
