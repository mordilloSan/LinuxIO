package loginhistory

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseLastOutputRemote(t *testing.T) {
	logins := ParseLastOutput("miguel", `miguel   pts/0        172.18.0.7       Tue Apr  1 15:04:00 2026 - still logged in

wtmp begins Tue Mar  1 10:00:00 2026
`)

	require.Len(t, logins, 1)
	require.Equal(t, "miguel", logins[0].Username)
	require.Equal(t, "pts/0", logins[0].Terminal)
	require.Equal(t, "172.18.0.7", logins[0].Source)
	require.Equal(t, "Tue Apr 1 15:04:00 2026", logins[0].Time)
	require.False(t, logins[0].StartedAt.IsZero())
}

func TestParseLastOutputLocal(t *testing.T) {
	logins := ParseLastOutput("miguel", `miguel   tty1                          Tue Apr  1 15:04:00 2026 - still logged in

wtmp begins Tue Mar  1 10:00:00 2026
`)

	require.Len(t, logins, 1)
	require.Equal(t, "miguel", logins[0].Username)
	require.Equal(t, "tty1", logins[0].Terminal)
	require.Empty(t, logins[0].Source)
	require.Equal(t, "Tue Apr 1 15:04:00 2026", logins[0].Time)
}

func TestParseLastOutputCompletedSession(t *testing.T) {
	logins := ParseLastOutput("miguel", `miguel   pts/0        172.18.0.7       Tue Apr  1 15:04:00 2026 - Tue Apr  1 16:04:00 2026  (01:00)

wtmp begins Tue Mar  1 10:00:00 2026
`)

	require.Len(t, logins, 1)
	require.Equal(t, "Tue Apr 1 15:04:00 2026", logins[0].Time)
}

func TestParseLastOutputWebTerminalWithSource(t *testing.T) {
	logins := ParseLastOutput("miguel", `miguel   web console  192.168.1.42    Tue Apr  1 15:04:00 2026 - still logged in

wtmp begins Tue Mar  1 10:00:00 2026
`)

	require.Len(t, logins, 1)
	require.Equal(t, "web console", logins[0].Terminal)
	require.Equal(t, "192.168.1.42", logins[0].Source)
}

func TestParseLastOutputNoEntries(t *testing.T) {
	logins := ParseLastOutput("miguel", `
wtmp begins Tue Mar  1 10:00:00 2026
`)

	require.Empty(t, logins)
}

func TestParseLastOutputSkipsSystemEntries(t *testing.T) {
	logins := ParseLastOutput("", `reboot   system boot  6.12.73+deb13-amd64 Mon May  4 15:31:24 2026 - still running
miguel   pts/0        172.18.0.7       Tue Apr  1 15:04:00 2026 - still logged in
`)

	require.Len(t, logins, 1)
	require.Equal(t, "miguel", logins[0].Username)
}

func TestParseWtmpdbOutput(t *testing.T) {
	logins, err := ParseWtmpdbOutput("", []byte(`{
  "entries": [
    {
      "user": "miguel",
      "tty": "pts/0",
      "hostname": "192.168.1.239",
      "login": "Mon May  4 19:11:47 2026"
    },
    {
      "user": "reboot",
      "tty": "system boot",
      "hostname": "6.12.73+deb13-amd64",
      "login": "Mon May  4 15:31:24 2026"
    }
  ]
}`))

	require.NoError(t, err)
	require.Len(t, logins, 1)
	require.Equal(t, "miguel", logins[0].Username)
	require.Equal(t, "pts/0", logins[0].Terminal)
	require.Equal(t, "192.168.1.239", logins[0].Source)
	require.Equal(t, "Mon May 4 19:11:47 2026", logins[0].Time)
	require.False(t, logins[0].StartedAt.IsZero())
}
