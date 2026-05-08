package loginhistory

import (
	"context"
	"encoding/binary"
	"os"
	"testing"
	"time"

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

func TestCountBtmpFailuresSince(t *testing.T) {
	data := appendBtmpRecord(nil, "miguel", 100)
	data = appendBtmpRecord(data, "other", 110)
	data = appendBtmpRecord(data, "miguel", 120)
	data = append(data, []byte("partial")...)

	require.Equal(t, 1, countBtmpFailuresSince("miguel", data, 110))
}

func TestCountBtmpFailuresAcceptsUserProcess(t *testing.T) {
	record := make([]byte, btmpRecordSize)
	binary.LittleEndian.PutUint16(record[btmpTypeOffset:], utmpUserProcess)
	copy(record[btmpUserOffset:btmpUserOffset+btmpUserSize], "miguel")
	binary.LittleEndian.PutUint32(record[btmpTimeOffset:], 120)

	require.Equal(t, 1, countBtmpFailuresSince("miguel", record, 0))
}

func TestCountBtmpFailuresSkipsDeadProcess(t *testing.T) {
	record := make([]byte, btmpRecordSize)
	binary.LittleEndian.PutUint16(record[btmpTypeOffset:], 8)
	copy(record[btmpUserOffset:btmpUserOffset+btmpUserSize], "miguel")
	binary.LittleEndian.PutUint32(record[btmpTimeOffset:], 120)

	require.Zero(t, countBtmpFailuresSince("miguel", record, 0))
}

func TestPreviousSuccessfulLoginUnix(t *testing.T) {
	current := time.Date(2026, time.May, 8, 12, 0, 0, 0, time.UTC)
	previous := time.Date(2026, time.May, 7, 12, 0, 0, 0, time.UTC)

	require.Equal(t, previous.Unix(), previousSuccessfulLoginUnix([]Login{
		{StartedAt: current},
		{StartedAt: previous},
	}))
}

func TestPreviousSuccessfulLoginUnixBeforeSkipsCurrentAndLaterLogins(t *testing.T) {
	later := time.Date(2026, time.May, 8, 12, 5, 0, 0, time.UTC)
	current := time.Date(2026, time.May, 8, 12, 0, 0, 0, time.UTC)
	previous := time.Date(2026, time.May, 7, 12, 0, 0, 0, time.UTC)

	require.Equal(t, previous.Unix(), previousSuccessfulLoginUnixBefore([]Login{
		{StartedAt: later},
		{StartedAt: current},
		{StartedAt: previous},
	}, current.Add(500*time.Millisecond)))
}

func TestFetchFailedAttemptsCountsSincePreviousSuccess(t *testing.T) {
	originalRunCommand := runCommand
	originalReadFile := readFile
	t.Cleanup(func() {
		runCommand = originalRunCommand
		readFile = originalReadFile
	})

	runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		require.Equal(t, "wtmpdb", name)
		return []byte(`{
  "entries": [
    {
      "user": "miguel",
      "tty": "web console",
      "hostname": "192.168.1.239",
      "login": "Fri May  8 12:00:00 2026"
    },
    {
      "user": "miguel",
      "tty": "pts/0",
      "hostname": "192.168.1.239",
      "login": "Thu May  7 12:00:00 2026"
    }
  ]
}`), nil
	}
	readFile = func(path string) ([]byte, error) {
		require.Equal(t, btmpPath, path)
		previous := uint32(time.Date(2026, time.May, 7, 12, 0, 0, 0, time.UTC).Unix())
		data := appendBtmpRecord(nil, "miguel", previous-1)
		data = appendBtmpRecord(data, "miguel", previous+1)
		return data, nil
	}

	count, err := FetchFailedAttempts(context.Background(), "miguel", time.Time{})

	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestFetchFailedAttemptsCountsBeforeCurrentSession(t *testing.T) {
	originalRunCommand := runCommand
	originalReadFile := readFile
	t.Cleanup(func() {
		runCommand = originalRunCommand
		readFile = originalReadFile
	})

	sessionStartedAt := time.Date(2026, time.May, 8, 12, 0, 0, 500, time.UTC)
	previous := time.Date(2026, time.May, 7, 12, 0, 0, 0, time.UTC)

	runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		require.Equal(t, "wtmpdb", name)
		return []byte(`{
  "entries": [
    {
      "user": "miguel",
      "tty": "web console",
      "hostname": "192.168.1.239",
      "login": "Fri May  8 12:05:00 2026"
    },
    {
      "user": "miguel",
      "tty": "web console",
      "hostname": "192.168.1.239",
      "login": "Fri May  8 12:00:00 2026"
    },
    {
      "user": "miguel",
      "tty": "pts/0",
      "hostname": "192.168.1.239",
      "login": "Thu May  7 12:00:00 2026"
    }
  ]
}`), nil
	}
	readFile = func(path string) ([]byte, error) {
		require.Equal(t, btmpPath, path)
		data := appendBtmpRecord(nil, "miguel", uint32(previous.Unix()-1))
		data = appendBtmpRecord(data, "miguel", uint32(previous.Unix()+1))
		data = appendBtmpRecord(data, "miguel", uint32(sessionStartedAt.Unix()+30))
		return data, nil
	}

	count, err := FetchFailedAttempts(context.Background(), "miguel", sessionStartedAt)

	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestFetchFailedAttemptsReturnsZeroWhenBtmpPermissionDenied(t *testing.T) {
	originalRunCommand := runCommand
	originalReadFile := readFile
	t.Cleanup(func() {
		runCommand = originalRunCommand
		readFile = originalReadFile
	})

	runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		require.Equal(t, "wtmpdb", name)
		return []byte(`{"entries":[]}`), nil
	}
	readFile = func(path string) ([]byte, error) {
		require.Equal(t, btmpPath, path)
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrPermission}
	}

	count, err := FetchFailedAttempts(context.Background(), "miguel", time.Time{})

	require.NoError(t, err)
	require.Zero(t, count)
}

func appendBtmpRecord(data []byte, username string, sec uint32) []byte {
	record := make([]byte, btmpRecordSize)
	binary.LittleEndian.PutUint16(record[btmpTypeOffset:], utmpLoginProcess)
	copy(record[btmpUserOffset:btmpUserOffset+btmpUserSize], username)
	binary.LittleEndian.PutUint32(record[btmpTimeOffset:], sec)
	return append(data, record...)
}
