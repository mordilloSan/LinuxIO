package version

import (
	"maps"
	"os"
	"path/filepath"
	"testing"
)

func TestComponentsAreUniqueAndIncludeMonitoring(t *testing.T) {
	names := map[string]bool{}
	binaries := map[string]bool{}
	for _, c := range Components {
		if c.Name == "" || c.Binary == "" {
			t.Errorf("incomplete component %+v", c)
		}
		if names[c.Name] || binaries[c.Binary] {
			t.Errorf("duplicate component %+v", c)
		}
		names[c.Name], binaries[c.Binary] = true, true
	}
	if !binaries["linuxio-monitoring"] {
		t.Error("Components leaves out linuxio-monitoring")
	}
}

func TestReadComponentVersions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "components")
	content := "LinuxIO CLI v1.2.3\n\nLinuxIO Web Server v1.2.4\nLinuxIO Auth (version unknown)\nLinuxIO Later v9\nLinuxIO Bridge\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadComponentVersions(path)
	if err != nil {
		t.Fatal(err)
	}
	// A line of a program this build does not know and a line without a
	// version are skipped.
	want := map[string]string{"LinuxIO CLI": "v1.2.3", "LinuxIO Web Server": "v1.2.4", "LinuxIO Auth": "(version unknown)"}
	if !maps.Equal(got, want) {
		t.Fatalf("versions = %v, want %v", got, want)
	}
	if _, err := ReadComponentVersions(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("a missing file read without an error")
	}
}
