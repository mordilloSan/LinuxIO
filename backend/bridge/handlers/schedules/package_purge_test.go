package schedules

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A purge of the DEB keeps the folder whose scripts the scheduled scripts'
// units run, since those units stay after the package goes: it is in the
// postrm's admin_state list.
func TestPackagePurgeKeepsTheScripts(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "packaging", "debian", "postrm"))
	if err != nil {
		t.Fatal(err)
	}
	_, kept, _ := strings.Cut(string(data), "\nadmin_state=\"")
	kept, _, _ = strings.Cut(kept, "\"")
	if !slices.Contains(strings.Fields(kept), defaultStore.configDir) {
		t.Errorf("packaging/debian/postrm purges %s, which the scheduled scripts' units run from", defaultStore.configDir)
	}
}
