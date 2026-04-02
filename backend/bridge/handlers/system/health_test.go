package system

import (
	"testing"

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
