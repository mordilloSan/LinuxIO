package system

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseLastlogOutput(t *testing.T) {
	login := parseLastlogOutput("miguel", `Username         Port     From             Latest
miguel           pts/0    172.18.0.7       Tue Apr  1 15:04:00 +0000 2026
`)

	require.NotNil(t, login)
	require.Equal(t, "miguel", login.Username)
	require.Equal(t, "pts/0", login.Terminal)
	require.Equal(t, "172.18.0.7", login.Source)
	require.Equal(t, "Tue Apr 1 15:04:00 +0000 2026", login.Time)
}

func TestParseLastlogOutputNeverLoggedIn(t *testing.T) {
	login := parseLastlogOutput("miguel", `Username         Port     From             Latest
miguel                                      **Never logged in**
`)

	require.Nil(t, login)
}

func TestParseUncleanShutdownOutputClean(t *testing.T) {
	unclean := parseUncleanShutdownOutput(`reboot   system boot  6.8.0-58-generic Tue Apr  1 15:04:00 2026   still running
shutdown system down  6.8.0-58-generic Tue Apr  1 15:03:00 2026 - Tue Apr  1 15:04:00 2026  (00:01)
wtmp begins Tue Mar  1 10:00:00 2026
`)

	require.False(t, unclean)
}

func TestParseUncleanShutdownOutputUnclean(t *testing.T) {
	unclean := parseUncleanShutdownOutput(`reboot   system boot  6.8.0-58-generic Tue Apr  1 15:04:00 2026   still running
reboot   system boot  6.8.0-58-generic Tue Apr  1 14:00:00 2026 - crash      (01:04)
wtmp begins Tue Mar  1 10:00:00 2026
`)

	require.True(t, unclean)
}

func TestParseRecentSuccessfulLoginTimes(t *testing.T) {
	current, previous := parseRecentSuccessfulLoginTimes(`miguelma web console  ::ffff:172.18.0. 2026-04-01T19:23:21+01:00   still logged in
miguelma web console  ::ffff:172.18.0. 2026-04-01T19:14:05+01:00 - 2026-04-01T19:23:20+01:00  (00:09)
wtmp begins 2025-01-03T12:23:07+00:00
`)

	require.True(t, current.Equal(time.Date(2026, 4, 1, 19, 23, 21, 0, time.FixedZone("", 3600))))
	require.NotNil(t, previous)
	require.True(t, previous.Equal(time.Date(2026, 4, 1, 19, 14, 5, 0, time.FixedZone("", 3600))))
}

func TestCountFailedLoginAttemptsBetween(t *testing.T) {
	previous := time.Date(2026, 4, 1, 19, 14, 5, 0, time.FixedZone("", 3600))
	current := time.Date(2026, 4, 1, 19, 23, 21, 0, time.FixedZone("", 3600))

	count := countFailedLoginAttemptsBetween(`miguelma ssh:notty    ::ffff:172.18.0. 2026-04-01T19:22:10+01:00 - 2026-04-01T19:22:10+01:00  (00:00)
miguelma ssh:notty    ::ffff:172.18.0. 2026-04-01T19:20:45+01:00 - 2026-04-01T19:20:45+01:00  (00:00)
miguelma ssh:notty    ::ffff:172.18.0. 2026-04-01T19:10:00+01:00 - 2026-04-01T19:10:00+01:00  (00:00)
btmp begins 2025-01-03T12:23:07+00:00
`, &previous, current)

	require.Equal(t, 2, count)
}

func TestFetchFailedLoginAttemptsRequiresPrivilege(t *testing.T) {
	count, err := FetchFailedLoginAttempts("miguel", false)

	require.NoError(t, err)
	require.Zero(t, count)
}
