package indexer

import "testing"

func TestEntryForPathNormalizesCanonicalPath(t *testing.T) {
	got := EntryForPath("/tmp/../.hidden-file")
	if got.Path != "/.hidden-file" {
		t.Fatalf("entry path = %q, want /.hidden-file", got.Path)
	}
}
