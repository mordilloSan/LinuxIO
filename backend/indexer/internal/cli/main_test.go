package cli

import (
	"flag"
	"os"
	"slices"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/configfile"
)

func TestMainOnlyExposesProcessMetadata(t *testing.T) {
	stdout, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open output sink: %v", err)
	}
	stderr, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		_ = stdout.Close()
		t.Fatalf("open error sink: %v", err)
	}

	originalStdout, originalStderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = stdout, stderr
	t.Cleanup(func() {
		os.Stdout, os.Stderr = originalStdout, originalStderr
		_ = stdout.Close()
		_ = stderr.Close()
	})

	for _, test := range []struct {
		name string
		args []string
		want int
	}{
		{name: "help", args: []string{"--help"}, want: 0},
		{name: "version", args: []string{"--version"}, want: 0},
		{name: "index worker help", args: []string{"--index-mode", "--help"}, want: 0},
		{name: "timer trigger help", args: []string{"--trigger-index", "--help"}, want: 0},
		{name: "daemon command removed", args: []string{"daemon"}, want: 1},
		{name: "config removed", args: []string{"config"}, want: 1},
		{name: "setup removed", args: []string{"setup"}, want: 1},
		{name: "service removed", args: []string{"service"}, want: 1},
		{name: "status removed", args: []string{"status"}, want: 1},
		{name: "index removed", args: []string{"index"}, want: 1},
		{name: "serve removed", args: []string{"serve"}, want: 1},
		{name: "licenses removed", args: []string{"licenses"}, want: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := Main(test.args); got != test.want {
				t.Fatalf("Main(%q) = %d, want %d", test.args, got, test.want)
			}
		})
	}
}

func TestApplyIndexFlagOverridesUsesConfigAndExplicitFlags(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	values := registerIndexFlags(fs)
	if err := fs.Parse([]string{"--path=/media", "--exclude-path=/media/cache", "--exclude-path=/media/tmp", "--include-hidden=false", "--include-network-mounts=true", "--keep-indexes=3", "--integrity-check=quick"}); err != nil {
		t.Fatalf("parse flags: %v", err)
	}

	base := configfile.Defaults()
	base.IndexPath = "/data"
	base.IndexName = "data"
	base.IncludeHidden = true
	next, err := applyIndexFlagOverrides(fs, base, values)
	if err != nil {
		t.Fatalf("apply index overrides: %v", err)
	}

	if next.IndexPath != "/media" {
		t.Fatalf("IndexPath = %q, want /media", next.IndexPath)
	}
	if !slices.Equal(next.ExcludePaths, []string{"/media/cache", "/media/tmp"}) {
		t.Fatalf("ExcludePaths = %#v", next.ExcludePaths)
	}
	if next.IncludeHidden {
		t.Fatal("IncludeHidden = true, want false")
	}
	if !next.IncludeNetworkMounts {
		t.Fatal("IncludeNetworkMounts = false, want true")
	}
	if next.KeepIndexes != 3 {
		t.Fatalf("KeepIndexes = %d, want 3", next.KeepIndexes)
	}
	if next.IntegrityCheck != configfile.IntegrityCheckQuick {
		t.Fatalf("IntegrityCheck = %q, want quick", next.IntegrityCheck)
	}
	if next.IndexName != "data" {
		t.Fatalf("IndexName = %q, want existing config name", next.IndexName)
	}
}
