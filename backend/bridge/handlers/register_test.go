package handlers

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// registeredFamilyDir names the handlers/ directory that owns a family, taken
// from its Register function ("<module>/bridge/handlers/<dir>.RegisterHandlers").
func registeredFamilyDir(t *testing.T, f family) string {
	t.Helper()
	fn := runtime.FuncForPC(reflect.ValueOf(f.Register).Pointer())
	if fn == nil {
		t.Fatal("family Register has no function name")
	}
	_, after, ok := strings.Cut(fn.Name(), "/bridge/handlers/")
	if !ok {
		t.Fatalf("family Register %q is not a handlers/<family> function", fn.Name())
	}
	dir, _, _ := strings.Cut(after, ".")
	return dir
}

// Every handlers/<dir>/handlers.go declares routes the bridge must register.
func TestFamiliesListEveryHandlerDirectory(t *testing.T) {
	var onDisk []string
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(entry.Name(), "handlers.go")); err == nil {
			onDisk = append(onDisk, entry.Name())
		}
	}

	var listed []string
	for _, f := range families {
		if f.Register == nil {
			t.Error("family without a Register function")
			continue
		}
		listed = append(listed, registeredFamilyDir(t, f))
	}
	slices.Sort(onDisk)
	slices.Sort(listed)
	if !slices.Equal(onDisk, listed) {
		t.Fatalf("handlers directories with handlers.go = %v, families = %v", onDisk, listed)
	}
}
