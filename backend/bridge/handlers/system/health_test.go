package system

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseUncleanShutdownOutputClean(t *testing.T) {
	unclean, bootID := parseUncleanShutdownOutput(`reboot   system boot  6.8.0-58-generic Tue Apr  1 15:04:00 2026   still running
shutdown system down  6.8.0-58-generic Tue Apr  1 15:03:00 2026 - Tue Apr  1 15:04:00 2026  (00:01)
wtmp begins Tue Mar  1 10:00:00 2026
`)

	require.False(t, unclean)
	require.Empty(t, bootID)
}

func TestParseUncleanShutdownOutputUncleanCrash(t *testing.T) {
	unclean, bootID := parseUncleanShutdownOutput(`reboot   system boot  6.8.0-58-generic Tue Apr  1 15:04:00 2026   still running
reboot   system boot  6.8.0-58-generic Tue Apr  1 14:00:00 2026 - crash      (01:04)
wtmp begins Tue Mar  1 10:00:00 2026
`)

	require.True(t, unclean)
	// Identifier is the unix-epoch seconds of the unclean boot's start.
	expected := time.Date(2026, time.April, 1, 14, 0, 0, 0, time.UTC).Unix()
	require.Equal(t, strconv.FormatInt(expected, 10), bootID)
}

func TestParseUncleanShutdownOutputUncleanStillRunning(t *testing.T) {
	// util-linux shows an unfinished previous session as "still running".
	unclean, bootID := parseUncleanShutdownOutput(`reboot   system boot  6.8.0-58-generic Tue Apr  1 15:04:00 2026   still running
reboot   system boot  6.8.0-58-generic Tue Apr  1 14:00:00 2026   still running
wtmp begins Tue Mar  1 10:00:00 2026
`)

	require.True(t, unclean)
	require.NotEmpty(t, bootID)
}

func TestParseUncleanShutdownOutputAmbiguous(t *testing.T) {
	// Two reboots back-to-back but no "crash" / "still running" marker on the
	// previous one — treat as clean to avoid false positives.
	unclean, bootID := parseUncleanShutdownOutput(`reboot   system boot  6.8.0-58-generic Tue Apr  1 15:04:00 2026   still running
reboot   system boot  6.8.0-58-generic Tue Apr  1 14:00:00 2026 - Tue Apr  1 14:30:00 2026  (00:30)
wtmp begins Tue Mar  1 10:00:00 2026
`)

	require.False(t, unclean)
	require.Empty(t, bootID)
}

func TestParseUncleanShutdownOutputSingleBoot(t *testing.T) {
	unclean, bootID := parseUncleanShutdownOutput(`reboot   system boot  6.8.0-58-generic Tue Apr  1 15:04:00 2026   still running
wtmp begins Tue Apr  1 15:04:00 2026
`)

	require.False(t, unclean)
	require.Empty(t, bootID)
}
