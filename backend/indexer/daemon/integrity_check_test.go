package daemon

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/configfile"
	"github.com/mordilloSan/LinuxIO/backend/indexer/storage"
)

func TestIntegrityCheckQuery(t *testing.T) {
	tests := []struct {
		name      string
		mode      string
		wantQuery string
		wantName  string
		wantErr   bool
	}{
		{name: "default", wantQuery: "PRAGMA integrity_check;", wantName: "integrity_check"},
		{name: "full", mode: configfile.IntegrityCheckFull, wantQuery: "PRAGMA integrity_check;", wantName: "integrity_check"},
		{name: "quick", mode: configfile.IntegrityCheckQuick, wantQuery: "PRAGMA quick_check;", wantName: "quick_check"},
		{name: "off", mode: configfile.IntegrityCheckOff},
		{name: "invalid", mode: "fast", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query, checkName, err := integrityCheckQuery(test.mode)
			if test.wantErr {
				if err == nil {
					t.Fatalf("integrityCheckQuery(%q) returned nil error", test.mode)
				}
				return
			}
			if err != nil {
				t.Fatalf("integrityCheckQuery(%q): %v", test.mode, err)
			}
			if query != test.wantQuery || checkName != test.wantName {
				t.Fatalf("integrityCheckQuery(%q) = (%q, %q), want (%q, %q)", test.mode, query, checkName, test.wantQuery, test.wantName)
			}
		})
	}
}

func TestCheckDatabaseIntegrityModes(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "index.db")
	db, err := storage.Open(dbPath, storage.DefaultOpenOptions())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Errorf("close database: %v", closeErr)
		}
	})

	for _, mode := range []string{configfile.IntegrityCheckFull, configfile.IntegrityCheckQuick} {
		if err := checkDatabaseIntegrity(context.Background(), db, mode); err != nil {
			t.Errorf("checkDatabaseIntegrity(%q): %v", mode, err)
		}
	}

	// A nil DB is intentional: off must return before attempting any query.
	if err := checkDatabaseIntegrity(context.Background(), nil, configfile.IntegrityCheckOff); err != nil {
		t.Fatalf("checkDatabaseIntegrity(off): %v", err)
	}
}

func TestRunIndexModeOffSkipsIntegrityCheckPhase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "index.db")
	db, err := storage.Open(dbPath, storage.DefaultOpenOptions())
	if err != nil {
		t.Fatalf("create existing database: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close existing database: %v", err)
	}

	progress := &recordingIndexProgress{}
	if _, err := RunIndexMode("test", t.TempDir(), nil, true, false, true, dbPath, 1, configfile.IntegrityCheckOff, storage.DefaultOpenOptions(), progress); err != nil {
		t.Fatalf("RunIndexMode: %v", err)
	}
	if len(progress.steps) == 0 || progress.steps[0] != "Opening existing database" {
		t.Fatalf("first progress step = %q, want Opening existing database", progress.steps)
	}
	for _, step := range progress.steps {
		if step == "Checking database integrity" {
			t.Fatalf("off mode emitted integrity-check phase: %q", progress.steps)
		}
	}
}

type recordingIndexProgress struct {
	steps []string
}

func (p *recordingIndexProgress) Step(message string) {
	p.steps = append(p.steps, message)
}

func (*recordingIndexProgress) ScanProgress(_, _, _ uint64) {}
