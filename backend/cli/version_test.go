package main

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

func versionOutput(t *testing.T, recorded map[string]string, readErr error) string {
	t.Helper()
	orig := readComponentVersions
	t.Cleanup(func() { readComponentVersions = orig })
	readComponentVersions = func() (map[string]string, error) { return recorded, readErr }

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	showVersion(nil)
	os.Stdout = stdout
	_ = w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// The CLI prints its own version from this build and the other programs'
// from the installer's record, and runs none of them.
func TestShowVersionPrintsTheRecordedVersions(t *testing.T) {
	out := versionOutput(t, map[string]string{"LinuxIO CLI": "v0.0.1", "LinuxIO Bridge": "v1.2.3"}, nil)

	for _, want := range []string{"\n  " + cliVersionLine() + "\n", "\n  LinuxIO Bridge v1.2.3\n", "\n  linuxio-auth: version not recorded\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "v0.0.1") {
		t.Errorf("the CLI printed its recorded version, not its own:\n%s", out)
	}
}

func TestShowVersionWithoutARecordNamesEveryOtherProgram(t *testing.T) {
	out := versionOutput(t, nil, errors.New("no such file"))

	for _, c := range version.Components {
		if c.Binary == "linuxio" {
			continue
		}
		want := "\n  " + c.Binary + ": version not recorded\n"
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}
