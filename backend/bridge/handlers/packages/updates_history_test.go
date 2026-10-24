package packages

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func TestApplyDpkgLogLineRecordsNewVersion(t *testing.T) {
	lines := []string{
		"2026-10-20 08:15:02 upgrade libc6:amd64 2.36-9 2.36-10",
		"2026-10-20 08:15:03 install foo:amd64 <none> 1.0-1",
		"2026-10-20 08:15:04 install bar:amd64 <none> 2.0-1",
		"2026-10-20 08:15:05 configure foo:amd64 1.0-1 <none>",
		"2026-10-20 08:15:06 status installed foo:amd64 1.0-1",
	}
	history := map[string][]apischema.UpgradeItem{}
	pending := map[string]string{}
	patterns := newDpkgLogPatterns()
	for _, line := range lines {
		applyDpkgLogLine(line, patterns, history, pending)
	}

	assert.Equal(t, []apischema.UpgradeItem{
		{Package: "libc6:amd64", Version: "2.36-10"},
		{Package: "foo:amd64", Version: "1.0-1"},
	}, history["2026-10-20"])
	assert.Equal(t, map[string]string{"bar:amd64": "2026-10-20"}, pending)
}

func TestParseDnfHistorySplitsNEVRAAndKeysByDate(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "dnf.log")
	content := "2026-10-20T08:15:00+0000 INFO --- logging initialized ---\n" +
		"2026-10-20T08:15:02+0000 INFO Upgrade: python3-requests-2.28.1-1.fc38.noarch\n" +
		"2026-10-20T09:01:11+0000 INFO Upgrade: bash-5.2.15-3.fc38.x86_64\n" +
		"2026-10-21T10:00:00+0000 INFO Upgrade: garbage\n"
	require.NoError(t, os.WriteFile(logPath, []byte(content), 0o600))

	rows := parseDnfHistory(context.Background(), logPath)

	require.Len(t, rows, 1)
	assert.Equal(t, "2026-10-20", rows[0].Date)
	assert.Equal(t, []apischema.UpgradeItem{
		{Package: "python3-requests", Version: "2.28.1-1.fc38"},
		{Package: "bash", Version: "5.2.15-3.fc38"},
	}, rows[0].Upgrades)
}
