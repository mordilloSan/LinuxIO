package osrelease

import (
	"slices"
	"testing"
)

func TestParse(t *testing.T) {
	info := Parse([]byte("# comment\nID=Rocky\nID_LIKE=\"RHEL centos fedora\"\nNAME='Rocky Linux'\n\nbroken\nVERSION_ID = \"9.4\"\n"))
	if info.ID() != "rocky" || !slices.Equal(info.IDLike(), []string{"rhel", "centos", "fedora"}) {
		t.Fatalf("ids = %q %q", info.ID(), info.IDLike())
	}
	if info["NAME"] != "Rocky Linux" || info["VERSION_ID"] != "9.4" || len(info) != 4 {
		t.Fatalf("info = %v", info)
	}
}
