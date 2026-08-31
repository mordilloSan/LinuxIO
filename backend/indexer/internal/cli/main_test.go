package cli

import (
	"os"
	"testing"
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
		{name: "index worker private flag removed", args: []string{"--index-mode", "--db-path", "/tmp/index.db"}, want: 1},
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
