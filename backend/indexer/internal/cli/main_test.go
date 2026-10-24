package cli

import (
	"os"
	"testing"
)

func TestRunOnlyExposesProcessMetadata(t *testing.T) {
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
		{name: "index worker private flag removed", args: []string{"--index-mode", "--db-path", "/tmp/index.db"}, want: 2},
		{name: "timer trigger help", args: []string{"--trigger-index", "--help"}, want: 0},
		{name: "daemon command removed", args: []string{"daemon"}, want: 2},
		{name: "config removed", args: []string{"config"}, want: 2},
		{name: "setup removed", args: []string{"setup"}, want: 2},
		{name: "service removed", args: []string{"service"}, want: 2},
		{name: "status removed", args: []string{"status"}, want: 2},
		{name: "index removed", args: []string{"index"}, want: 2},
		{name: "serve removed", args: []string{"serve"}, want: 2},
		{name: "licenses removed", args: []string{"licenses"}, want: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := append([]string{"linuxio-indexer"}, test.args...)
			if got := Run(args); got != test.want {
				t.Fatalf("Run(%q) = %d, want %d", args, got, test.want)
			}
		})
	}
}
