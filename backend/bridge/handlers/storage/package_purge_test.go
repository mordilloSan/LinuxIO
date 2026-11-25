package storage

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A purge of the DEB keeps the CIFS credentials files, since the /etc/fstab
// lines that name them stay after the package goes: their folder is in the
// postrm's admin_state list.
func TestPackagePurgeKeepsTheCIFSCredentials(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "packaging", "debian", "postrm"))
	if err != nil {
		t.Fatal(err)
	}
	_, kept, _ := strings.Cut(string(data), "\nadmin_state=\"")
	kept, _, _ = strings.Cut(kept, "\"")
	if !slices.Contains(strings.Fields(kept), cifsCredentialsDir) {
		t.Errorf("packaging/debian/postrm purges %s, which /etc/fstab names", cifsCredentialsDir)
	}
}
