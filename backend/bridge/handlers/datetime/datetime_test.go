package datetime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

func TestSetNTPServersRejectsInvalidEntries(t *testing.T) {
	for _, server := range []string{
		"",
		"x\nmailonchange root a@b",
		"pool.ntp.org iburst",
		"a b",
		"host\r",
		"host_name",
		"host/path",
		strings.Repeat("a", 254),
	} {
		err := SetNTPServers(t.Context(), []string{"pool.ntp.org", server})
		assert.Equal(t, 400, bridgeipc.StatusCode(err), "server %q", server)
	}
}

func TestValidNTPServerAcceptsHostsAndIPLiterals(t *testing.T) {
	for _, server := range []string{"pool.ntp.org", "0.pool-1.ntp.org", "192.0.2.1", "2001:db8::1", "[2001:db8::1]", strings.Repeat("a", 253)} {
		assert.True(t, validNTPServer.MatchString(server), server)
	}
}

func TestFindManagedBlock(t *testing.T) {
	const start, end = "# start", "# end"
	tests := []struct {
		name         string
		lines        []string
		wantS, wantE int
	}{
		{"missing", []string{"a", "b"}, -1, -1},
		{"present", []string{"a", "# start", "x", "# end", "b"}, 1, 3},
		{"padded markers", []string{"  # start  ", "\t# end"}, 0, 1},
		{"unterminated", []string{"# start", "x"}, -1, -1},
		{"end before start", []string{"# end", "# start"}, -1, -1},
		{"second start wins", []string{"# start", "x", "# start", "y", "# end"}, 2, 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, e := findManagedBlock(tc.lines, start, end)
			assert.Equal(t, tc.wantS, s)
			assert.Equal(t, tc.wantE, e)
		})
	}
}

func TestReplaceManagedBlock(t *testing.T) {
	const start, end = "# start", "# end"
	block := []string{start, "server a iburst", end}
	tests := []struct {
		name     string
		lines    []string
		newBlock []string
		want     []string
	}{
		{"append to file without block", []string{"a", "b"}, block, []string{"a", "b", "", start, "server a iburst", end}},
		{"append to empty file", []string{""}, block, []string{start, "server a iburst", end}},
		{"replace existing block", []string{"a", start, "old", end, "b"}, block, []string{"a", start, "server a iburst", end, "b"}},
		{"remove existing block", []string{"a", start, "old", end}, nil, []string{"a"}},
		{"remove from file without block", []string{"a", "", ""}, nil, []string{"a"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, replaceManagedBlock(tc.lines, start, end, tc.newBlock))
		})
	}
}

func TestParseTimesyncdServers(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		want      []string
		wantFound bool
	}{
		{"servers in Time section", "[Time]\nNTP=a.example b.example\n", []string{"a.example", "b.example"}, true},
		{"empty value", "[Time]\nNTP=\n", []string{}, true},
		{"other section ignored", "[Other]\nNTP=a.example\n", nil, false},
		{"commented out", "[Time]\n#NTP=a.example\n", nil, false},
		{"section ends", "[Time]\nFallbackNTP=x\n[Other]\nNTP=a.example\n", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "timesyncd.conf")
			require.NoError(t, os.WriteFile(path, []byte(tc.content), 0o600))
			got, found, err := parseTimesyncdServers(path)
			require.NoError(t, err)
			assert.Equal(t, tc.wantFound, found)
			assert.Equal(t, tc.want, got)
		})
	}

	t.Run("missing file", func(t *testing.T) {
		got, found, err := parseTimesyncdServers(filepath.Join(t.TempDir(), "absent"))
		require.NoError(t, err)
		assert.False(t, found)
		assert.Nil(t, got)
	})
}

func TestHandleSetNTPRejectsUnparsableFlag(t *testing.T) {
	err := handleSetNTP(context.Background(), apischema.EnabledRequest{Enabled: "maybe"})
	require.ErrorIs(t, err, bridgeipc.ErrInvalidArgs)
}

// Removing the package deletes the time server file LinuxIO wrote, for
// timesyncd and for chrony where it reads sources from /etc/chrony/sources.d
// (Debian and Ubuntu), so the service's own servers return at its next
// start: each path is in the removal lists of both packages. The block it
// writes inside a chrony.conf that names no folder under /etc (Fedora's)
// stays, and so does its file in a folder a hand-edited chrony.conf names.
func TestPackageRemovalDeletesTheTimeServerFiles(t *testing.T) {
	chrony, ok := chronyIncludedManagedPath("sourcedir /run/chrony-dhcp\nsourcedir /etc/chrony/sources.d\n")
	require.True(t, ok)
	for _, script := range []string{"debian/postrm", "rpm/linuxio.spec"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "packaging", script))
		require.NoError(t, err)
		words := strings.Fields(string(data))
		for _, path := range []string{timesyncdManagedConf, chrony} {
			assert.Contains(t, words, path, "packaging/%s does not delete it", script)
		}
	}
}
