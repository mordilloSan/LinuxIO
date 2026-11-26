package sharedfolders

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestLoadRecycleBins(t *testing.T) {
	dir := t.TempDir()
	old := RecordPath
	RecordPath = filepath.Join(dir, "shared-folders.json")
	t.Cleanup(func() { RecordPath = old })

	bins, err := LoadRecycleBins()
	if err != nil || len(bins) != 0 {
		t.Fatalf("no record = %v, %v, want none", bins, err)
	}
	record := `{"folders": [
		{"uuid": "u1", "path": "Media", "name": "Media", "recycleBin": true, "access": []},
		{"uuid": "u1", "path": "Backups", "name": "Backups", "recycleBin": false},
		{"uuid": "u2", "path": "Docs", "name": "Docs", "recycleBin": true}
	]}`
	if err = os.WriteFile(RecordPath, []byte(record), 0o600); err != nil {
		t.Fatal(err)
	}
	bins, err = LoadRecycleBins()
	if err != nil {
		t.Fatal(err)
	}
	want := []RecycleBin{{UUID: "u1", Path: "Media"}, {UUID: "u2", Path: "Docs"}}
	if !slices.Equal(bins, want) {
		t.Errorf("LoadRecycleBins() = %+v, want %+v", bins, want)
	}
	if err = os.WriteFile(RecordPath, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadRecycleBins(); err == nil {
		t.Error("a record that cannot be read gave no error")
	}
}

func TestRecordPathIsInTheConfigFolder(t *testing.T) {
	if RecordPath != "/etc/linuxio/shared-folders.json" {
		t.Errorf("RecordPath = %s", RecordPath)
	}
}
