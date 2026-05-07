package system

import (
	"strconv"
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

func TestCountPamFailedLoginAttemptsBeforeCurrentSession(t *testing.T) {
	count := countPamFailedLoginAttemptsBeforeCurrentSession("miguelmariz", `1775067814.674371 ubuntuserver linuxio-auth[47426]: pam_unix(linuxio:auth): authentication failure; logname= uid=0 euid=0 tty= ruser= rhost=web  user=miguelmariz
1775067819.609136 ubuntuserver linuxio-auth[47553]: pam_unix(linuxio:session): session opened for user miguelmariz(uid=1000) by miguelmariz(uid=0)
1775073909.483616 ubuntuserver linuxio-auth[194517]: pam_unix(linuxio:auth): authentication failure; logname= uid=0 euid=0 tty= ruser= rhost=web  user=miguelmariz
1775073912.696423 ubuntuserver linuxio-auth[194610]: pam_unix(linuxio:session): session opened for user miguelmariz(uid=1000) by miguelmariz(uid=0)
`)

	require.Equal(t, 1, count)
}

func TestCountPamFailedLoginAttemptsIgnoresOtherUsers(t *testing.T) {
	count := countPamFailedLoginAttemptsBeforeCurrentSession("miguelmariz", `1775073909.483616 ubuntuserver linuxio-auth[194517]: pam_unix(linuxio:auth): authentication failure; logname= uid=0 euid=0 tty= ruser= rhost=web  user=alice
1775073912.696423 ubuntuserver linuxio-auth[194610]: pam_unix(linuxio:session): session opened for user miguelmariz(uid=1000) by miguelmariz(uid=0)
`)

	require.Zero(t, count)
}

func TestFetchFailedLoginAttemptsRequiresPrivilege(t *testing.T) {
	count, err := FetchFailedLoginAttempts("miguel", false)

	require.NoError(t, err)
	require.Zero(t, count)
}
