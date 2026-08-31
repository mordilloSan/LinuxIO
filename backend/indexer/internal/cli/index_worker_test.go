package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/configfile"
)

func TestRunIndexModeReportsDatabaseOpenFailure(t *testing.T) {
	root := t.TempDir()
	stdout, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open output sink: %v", err)
	}
	originalStdout := os.Stdout
	os.Stdout = stdout
	t.Cleanup(func() {
		os.Stdout = originalStdout
		_ = stdout.Close()
	})

	dbDirectory := filepath.Join(root, "not-a-database")
	if err := os.Mkdir(dbDirectory, 0o755); err != nil {
		t.Fatalf("create database directory: %v", err)
	}

	configPath := filepath.Join(root, "config.yaml")
	cfg := configfile.Defaults()
	cfg.IndexPath = root
	cfg.IndexName = "test"
	cfg.DBPath = dbDirectory
	if err := configfile.Save(configPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	if got := runIndexMode([]string{"--config-file", configPath}); got != 1 {
		t.Fatalf("runIndexMode exit code = %d, want 1", got)
	}
}
