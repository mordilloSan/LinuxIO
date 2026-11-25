package memory

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A purge of the DEB keeps the swap file and the temporary file a resize
// makes beside it, since /etc/fstab names the swap file and the kernel
// refuses to delete one that is on: their folder is in the postrm's
// admin_state list.
func TestPackagePurgeKeepsTheSwapFile(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "packaging", "debian", "postrm"))
	if err != nil {
		t.Fatal(err)
	}
	_, kept, _ := strings.Cut(string(data), "\nadmin_state=\"")
	kept, _, _ = strings.Cut(kept, "\"")
	if !slices.Contains(strings.Fields(kept), ownSwapDir) {
		t.Errorf("packaging/debian/postrm purges %s, which /etc/fstab names", ownSwapDir)
	}
}
