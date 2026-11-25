package shares

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A purge of the DEB keeps the rsync module's configuration and secrets,
// since the rsync service that reads them stays after the package goes: both
// are in the postrm's admin_state list.
func TestPackagePurgeKeepsTheRsyncConfiguration(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "packaging", "debian", "postrm"))
	if err != nil {
		t.Fatal(err)
	}
	_, kept, _ := strings.Cut(string(data), "\nadmin_state=\"")
	kept, _, _ = strings.Cut(kept, "\"")
	for _, path := range []string{rsyncConfigFile, rsyncSecretsFile} {
		if !slices.Contains(strings.Fields(kept), path) {
			t.Errorf("packaging/debian/postrm purges %s, which the rsync service reads", path)
		}
	}
}
