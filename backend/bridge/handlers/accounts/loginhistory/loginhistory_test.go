package loginhistory

import (
	"context"
	"encoding/binary"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseWtmpLoginsRemote(t *testing.T) {
	loginAt := time.Date(2026, time.April, 1, 15, 4, 0, 0, time.UTC)
	data := appendWtmpUserRecord(nil, "miguel", uint32(loginAt.Unix()), "pts/0", "172.18.0.7")
	logins := parseWtmpLogins("miguel", data, 1)

	require.Len(t, logins, 1)
	require.Equal(t, "miguel", logins[0].Username)
	require.Equal(t, "pts/0", logins[0].Terminal)
	require.Equal(t, "172.18.0.7", logins[0].Source)
	require.Equal(t, formatLoginTime(loginAt), logins[0].Time)
	require.Equal(t, loginAt.Unix(), logins[0].StartedAt.Unix())
	require.NotEmpty(t, logins[0].ID)
}

func TestParseWtmpLoginsLocal(t *testing.T) {
	loginAt := time.Date(2026, time.April, 1, 15, 4, 0, 0, time.UTC)
	data := appendWtmpUserRecord(nil, "miguel", uint32(loginAt.Unix()), "tty1", "")
	logins := parseWtmpLogins("miguel", data, 1)

	require.Len(t, logins, 1)
	require.Equal(t, "miguel", logins[0].Username)
	require.Equal(t, "tty1", logins[0].Terminal)
	require.Empty(t, logins[0].Source)
	require.Equal(t, formatLoginTime(loginAt), logins[0].Time)
}

func TestParseWtmpLoginsNoEntries(t *testing.T) {
	logins := parseWtmpLogins("miguel", nil, 1)

	require.Empty(t, logins)
}

func TestParseWtmpLoginsSkipsSystemEntries(t *testing.T) {
	loginAt := time.Date(2026, time.April, 1, 15, 4, 0, 0, time.UTC)
	data := appendWtmpUserRecord(nil, "reboot", uint32(loginAt.Unix()), "system boot", "6.12.73")
	data = appendWtmpUserRecord(data, "miguel", uint32(loginAt.Add(time.Minute).Unix()), "pts/0", "172.18.0.7")
	logins := parseWtmpLogins("", data, 10)

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
	require.Equal(t, LoginStatusSuccess, logins[0].Status)
	require.NotEmpty(t, logins[0].ID)
}

func TestParseWtmpdbPreviousBootStatusUnclean(t *testing.T) {
	status, err := parseWtmpdbPreviousBootStatus([]byte(`{
  "entries": [
    {
      "user": "reboot",
      "tty": "system boot",
      "hostname": "6.12.94",
      "login": "2026-06-26T05:02:06+0100",
      "logout": "still running "
    },
    {
      "user": "reboot",
      "tty": "system boot",
      "hostname": "6.12.94",
      "login": "2026-06-24T08:27:09+0100",
      "logout": "crash "
    }
  ]
}`))

	require.NoError(t, err)
	require.True(t, status.Found)
	require.True(t, status.Unclean)
	require.Equal(t, mustParseWtmpdbISOTime(t, "2026-06-24T08:27:09+0100").Unix(), status.StartedAt.Unix())
}

func TestParseWtmpdbPreviousBootStatusClean(t *testing.T) {
	status, err := parseWtmpdbPreviousBootStatus([]byte(`{
  "entries": [
    {
      "user": "reboot",
      "tty": "system boot",
      "hostname": "6.12.94",
      "login": "2026-06-26T05:02:06+0100",
      "logout": "still running "
    },
    {
      "user": "reboot",
      "tty": "system boot",
      "hostname": "6.12.94",
      "login": "2026-06-24T08:27:09+0100",
      "logout": "2026-06-26T05:01:17+0100"
    },
    {
      "user": "shutdown",
      "tty": "system down",
      "hostname": "6.12.94",
      "login": "2026-06-26T05:01:17+0100",
      "logout": "2026-06-26T05:02:06+0100"
    }
  ]
}`))

	require.NoError(t, err)
	require.True(t, status.Found)
	require.False(t, status.Unclean)
	require.Equal(t, mustParseWtmpdbISOTime(t, "2026-06-24T08:27:09+0100").Unix(), status.StartedAt.Unix())
}

func TestParseWtmpdbPreviousBootStatusCleanViaShutdown(t *testing.T) {
	// wtmpdb may return the current boot followed directly by the prior
	// shutdown (the prior reboot scrolled past the -n window). A shutdown
	// immediately before the current boot is a definitive clean result.
	status, err := parseWtmpdbPreviousBootStatus([]byte(`{
  "entries": [
    { "user": "reboot",   "login": "2026-06-26T05:02:06+0100", "logout": "still running " },
    { "user": "shutdown", "login": "2026-06-26T05:01:17+0100", "logout": "2026-06-26T05:02:06+0100" }
  ]
}`))

	require.NoError(t, err)
	require.True(t, status.Found)
	require.False(t, status.Unclean)
}

func TestParseWtmpdbPreviousBootStatusNonRebootHeadIsNotDefinitive(t *testing.T) {
	// If the most recent system event isn't the current boot, wtmpdb can't
	// answer definitively — fall through to the raw-wtmp path.
	status, err := parseWtmpdbPreviousBootStatus([]byte(`{
  "entries": [
    { "user": "shutdown", "login": "2026-06-26T05:01:17+0100", "logout": "2026-06-26T05:02:06+0100" }
  ]
}`))

	require.NoError(t, err)
	require.False(t, status.Found)
}

func TestFetchPreviousUncleanBootUsesWtmpdb(t *testing.T) {
	originalRunCommand := runCommand
	originalReadFile := readFile
	t.Cleanup(func() {
		runCommand = originalRunCommand
		readFile = originalReadFile
	})

	previous := mustParseWtmpdbISOTime(t, "2026-06-24T08:27:09+0100")

	runCommand = func(_ context.Context, name string, args ...string) ([]byte, error) {
		require.Equal(t, "wtmpdb", name)
		require.Equal(t, []string{"last", "-j", "--time-format", "iso", "--system", "-n", "2", "shutdown", "reboot"}, args)
		return []byte(`{
  "entries": [
    { "user": "reboot", "login": "2026-06-26T05:02:06+0100", "logout": "still running " },
    { "user": "reboot", "login": "2026-06-24T08:27:09+0100", "logout": "crash " }
  ]
}`), nil
	}
	readFile = func(string) ([]byte, error) {
		t.Fatal("raw wtmp should not be read when wtmpdb has boot status")
		return nil, nil
	}

	startedAt, unclean, err := FetchPreviousUncleanBoot(context.Background())

	require.NoError(t, err)
	require.True(t, unclean)
	require.Equal(t, previous.Unix(), startedAt.Unix())
}

func TestFetchRecentUsesWtmpWhenWtmpdbUnavailable(t *testing.T) {
	originalRunCommand := runCommand
	originalReadFile := readFile
	t.Cleanup(func() {
		runCommand = originalRunCommand
		readFile = originalReadFile
	})

	loginAt := time.Date(2026, time.May, 8, 12, 0, 0, 0, time.UTC)

	runCommand = func(_ context.Context, name string, _ ...string) ([]byte, error) {
		if name == "wtmpdb" {
			return nil, exec.ErrNotFound
		}
		t.Fatalf("unexpected command fallback to %q", name)
		return nil, nil
	}
	readFile = func(path string) ([]byte, error) {
		require.Equal(t, wtmpPath, path)
		return appendWtmpUserRecord(nil, "miguel", uint32(loginAt.Unix()), "pts/0", "192.168.1.20"), nil
	}

	logins, err := FetchRecent(context.Background(), "miguel", 1)

	require.NoError(t, err)
	require.Len(t, logins, 1)
	require.Equal(t, "miguel", logins[0].Username)
	require.Equal(t, "pts/0", logins[0].Terminal)
	require.Equal(t, "192.168.1.20", logins[0].Source)
	require.Equal(t, loginAt.Unix(), logins[0].StartedAt.Unix())
}

func TestFetchPreviousUncleanBootFallsBackToWtmp(t *testing.T) {
	originalRunCommand := runCommand
	originalReadFile := readFile
	t.Cleanup(func() {
		runCommand = originalRunCommand
		readFile = originalReadFile
	})

	previous := time.Date(2026, time.May, 8, 11, 0, 0, 0, time.UTC)
	current := time.Date(2026, time.May, 8, 12, 0, 0, 0, time.UTC)

	runCommand = func(_ context.Context, name string, _ ...string) ([]byte, error) {
		require.Equal(t, "wtmpdb", name)
		return nil, exec.ErrNotFound
	}
	readFile = func(path string) ([]byte, error) {
		require.Equal(t, wtmpPath, path)
		data := appendBtmpRecord(nil, "reboot", uint32(previous.Unix()))
		return appendBtmpRecord(data, "reboot", uint32(current.Unix())), nil
	}

	startedAt, unclean, err := FetchPreviousUncleanBoot(context.Background())

	require.NoError(t, err)
	require.True(t, unclean)
	require.Equal(t, previous.Unix(), startedAt.Unix())
}

func TestFetchRecentReturnsEmptyWhenHistorySourcesMissing(t *testing.T) {
	originalRunCommand := runCommand
	originalReadFile := readFile
	t.Cleanup(func() {
		runCommand = originalRunCommand
		readFile = originalReadFile
	})

	runCommand = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, exec.ErrNotFound
	}
	readFile = func(path string) ([]byte, error) {
		require.Equal(t, wtmpPath, path)
		return nil, os.ErrNotExist
	}

	logins, err := FetchRecent(context.Background(), "miguel", 1)

	require.NoError(t, err)
	require.Empty(t, logins)
}

func TestFetchRecentReturnsContextErrorWhenCancelled(t *testing.T) {
	originalRunCommand := runCommand
	originalReadFile := readFile
	t.Cleanup(func() {
		runCommand = originalRunCommand
		readFile = originalReadFile
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	runCommand = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, exec.ErrNotFound
	}
	readFile = func(path string) ([]byte, error) {
		require.Equal(t, wtmpPath, path)
		return nil, nil
	}

	_, err := FetchRecent(ctx, "miguel", 1)

	require.ErrorIs(t, err, context.Canceled)
}

func TestParseWtmpBootEvents(t *testing.T) {
	previousBoot := time.Date(2026, time.May, 8, 11, 0, 0, 0, time.UTC)
	shutdown := time.Date(2026, time.May, 8, 11, 30, 0, 0, time.UTC)
	currentBoot := time.Date(2026, time.May, 8, 12, 0, 0, 0, time.UTC)

	data := appendBtmpRecord(nil, "reboot", uint32(previousBoot.Unix()))
	data = appendBtmpRecord(data, "shutdown", uint32(shutdown.Unix()))
	data = appendBtmpRecord(data, "reboot", uint32(currentBoot.Unix()))

	events := parseWtmpBootEvents(data, 2)

	require.Len(t, events, 2)
	require.Equal(t, "reboot", events[0].Kind)
	require.Equal(t, currentBoot.Unix(), events[0].StartedAt.Unix())
	require.Equal(t, "shutdown", events[1].Kind)
	require.Equal(t, shutdown.Unix(), events[1].StartedAt.Unix())
}

func TestPreviousUncleanBootFromEventsClean(t *testing.T) {
	currentBoot := time.Date(2026, time.May, 8, 12, 0, 0, 0, time.UTC)
	shutdown := time.Date(2026, time.May, 8, 11, 30, 0, 0, time.UTC)

	startedAt, unclean := previousUncleanBootFromEvents([]bootEvent{
		{Kind: "reboot", StartedAt: currentBoot},
		{Kind: "shutdown", StartedAt: shutdown},
	})

	require.False(t, unclean)
	require.True(t, startedAt.IsZero())
}

func TestParseBtmpFailures(t *testing.T) {
	older := uint32(time.Date(2026, time.May, 7, 12, 0, 0, 0, time.UTC).Unix())
	newer := uint32(time.Date(2026, time.May, 8, 12, 0, 0, 0, time.UTC).Unix())
	data := appendBtmpRecordWithDetails(nil, "miguel", older, "ssh:notty", "192.168.1.10")
	data = appendBtmpRecordWithDetails(data, "other", newer, "ssh:notty", "192.168.1.20")
	data = appendBtmpRecordWithDetails(data, "miguel", newer, "ssh:notty", "192.168.1.30")

	logins := parseBtmpFailures("miguel", data, 1)

	require.Len(t, logins, 1)
	require.Equal(t, "miguel", logins[0].Username)
	require.Equal(t, "ssh:notty", logins[0].Terminal)
	require.Equal(t, "192.168.1.30", logins[0].Source)
	require.Equal(t, LoginStatusFailed, logins[0].Status)
	require.Equal(t, time.Unix(int64(newer), 0), logins[0].StartedAt)
	require.NotEmpty(t, logins[0].ID)
}

func TestParseBtmpFailuresWithoutUsernameIncludesAllLoginUsers(t *testing.T) {
	older := uint32(time.Date(2026, time.May, 7, 12, 0, 0, 0, time.UTC).Unix())
	newer := uint32(time.Date(2026, time.May, 8, 12, 0, 0, 0, time.UTC).Unix())
	data := appendBtmpRecordWithDetails(nil, "miguel", older, "ssh:notty", "192.168.1.10")
	data = appendBtmpRecordWithDetails(data, "badname", newer, "web", "192.168.1.20")
	data = appendBtmpRecordWithDetails(data, "reboot", newer+1, "system boot", "")

	logins := parseBtmpFailures("", data, 10)

	require.Len(t, logins, 2)
	require.Equal(t, "badname", logins[0].Username)
	require.Equal(t, "miguel", logins[1].Username)
}

func TestStableLoginIDDifferentForEventIdentityFields(t *testing.T) {
	base := Login{
		Username:  "miguel",
		Terminal:  "web console",
		Source:    "192.168.1.239",
		StartedAt: time.Date(2026, time.May, 8, 12, 0, 0, 0, time.UTC),
		Status:    LoginStatusFailed,
	}

	require.Equal(t, StableLoginID(base), StableLoginID(base))

	changedStatus := base
	changedStatus.Status = LoginStatusSuccess
	changedTime := base
	changedTime.StartedAt = changedTime.StartedAt.Add(time.Second)
	changedTerminal := base
	changedTerminal.Terminal = "pts/1"
	changedSource := base
	changedSource.Source = "127.0.0.1"

	ids := map[string]bool{
		StableLoginID(base):            true,
		StableLoginID(changedStatus):   true,
		StableLoginID(changedTime):     true,
		StableLoginID(changedTerminal): true,
		StableLoginID(changedSource):   true,
	}
	require.Len(t, ids, 5)
}

func TestFetchRecentEventsMergesSuccessesAndFailures(t *testing.T) {
	originalRunCommand := runCommand
	originalReadFile := readFile
	t.Cleanup(func() {
		runCommand = originalRunCommand
		readFile = originalReadFile
	})

	failedAt := time.Date(2026, time.May, 8, 12, 30, 0, 0, time.UTC)

	runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		require.Equal(t, "wtmpdb", name)
		return []byte(`{
  "entries": [
    {
      "user": "miguel",
      "tty": "web console",
      "hostname": "192.168.1.239",
      "login": "Fri May  8 12:00:00 2026"
    }
  ]
}`), nil
	}
	readFile = func(path string) ([]byte, error) {
		require.Equal(t, btmpPath, path)
		return appendBtmpRecordWithDetails(nil, "miguel", uint32(failedAt.Unix()), "ssh:notty", "192.168.1.30"), nil
	}

	logins, err := FetchRecentEvents(context.Background(), "miguel", 2)

	require.NoError(t, err)
	require.Len(t, logins, 2)
	require.Equal(t, LoginStatusFailed, logins[0].Status)
	require.Equal(t, LoginStatusSuccess, logins[1].Status)
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
	putUtmpType(record, utmpUserProcess)
	copy(record[btmpUserOffset:btmpUserOffset+btmpUserSize], "miguel")
	putUtmpTime(record, 120)

	require.Equal(t, 1, countBtmpFailuresSince("miguel", record, 0))
}

func TestCountBtmpFailuresSkipsDeadProcess(t *testing.T) {
	record := make([]byte, btmpRecordSize)
	putUtmpType(record, 8)
	copy(record[btmpUserOffset:btmpUserOffset+btmpUserSize], "miguel")
	putUtmpTime(record, 120)

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

func TestFetchFailedAttemptBatchReturnsLatestFailedEventInWindow(t *testing.T) {
	originalRunCommand := runCommand
	originalReadFile := readFile
	t.Cleanup(func() {
		runCommand = originalRunCommand
		readFile = originalReadFile
	})

	sessionStartedAt := time.Date(2026, time.May, 8, 12, 0, 0, 500, time.UTC)
	previous := time.Date(2026, time.May, 7, 12, 0, 0, 0, time.UTC)
	firstFailed := previous.Add(time.Minute)
	latestFailed := sessionStartedAt.Add(-time.Minute)

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
		data := appendBtmpRecordWithDetails(nil, "miguel", uint32(previous.Unix()-1), "ssh:notty", "192.168.1.10")
		data = appendBtmpRecordWithDetails(data, "miguel", uint32(firstFailed.Unix()), "ssh:notty", "192.168.1.20")
		data = appendBtmpRecordWithDetails(data, "miguel", uint32(latestFailed.Unix()), "ssh:notty", "192.168.1.30")
		data = appendBtmpRecordWithDetails(data, "miguel", uint32(sessionStartedAt.Unix()+30), "ssh:notty", "192.168.1.40")
		return data, nil
	}

	batch, err := FetchFailedAttemptBatch(context.Background(), "miguel", sessionStartedAt)

	require.NoError(t, err)
	require.NotNil(t, batch)
	require.Equal(t, previous.Unix(), batch.SinceUnix)
	require.Equal(t, sessionStartedAt.Unix()+1, batch.UntilUnix)
	require.Equal(t, 2, batch.Count)
	require.Equal(t, "192.168.1.30", batch.Latest.Source)
	require.Equal(t, StableLoginID(batch.Latest), batch.Latest.ID)
}

func TestFetchFailedAttemptBatchForAllUsersCountsOtherAndUnknownUsers(t *testing.T) {
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
      "user": "admin",
      "tty": "web console",
      "hostname": "192.168.1.239",
      "login": "Fri May  8 12:00:00 2026"
    },
    {
      "user": "admin",
      "tty": "pts/0",
      "hostname": "192.168.1.239",
      "login": "Thu May  7 12:00:00 2026"
    }
  ]
}`), nil
	}
	readFile = func(path string) ([]byte, error) {
		require.Equal(t, btmpPath, path)
		data := appendBtmpRecordWithDetails(nil, "admin", uint32(previous.Unix()-1), "web", "192.168.1.10")
		data = appendBtmpRecordWithDetails(data, "other", uint32(previous.Unix()+1), "ssh:notty", "192.168.1.20")
		data = appendBtmpRecordWithDetails(data, "wrongname", uint32(sessionStartedAt.Unix()-30), "web", "192.168.1.30")
		data = appendBtmpRecordWithDetails(data, "admin", uint32(sessionStartedAt.Unix()+30), "web", "192.168.1.40")
		return data, nil
	}

	batch, err := FetchFailedAttemptBatchForAllUsers(context.Background(), "admin", sessionStartedAt)

	require.NoError(t, err)
	require.NotNil(t, batch)
	require.Equal(t, 2, batch.Count)
	require.Equal(t, "wrongname", batch.Latest.Username)
	require.Equal(t, "192.168.1.30", batch.Latest.Source)
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
	return appendBtmpRecordWithDetails(data, username, sec, "", "")
}

func appendBtmpRecordWithDetails(data []byte, username string, sec uint32, line, host string) []byte {
	record := make([]byte, btmpRecordSize)
	putUtmpType(record, utmpLoginProcess)
	copy(record[btmpLineOffset:btmpLineOffset+btmpLineSize], line)
	copy(record[btmpUserOffset:btmpUserOffset+btmpUserSize], username)
	copy(record[btmpHostOffset:btmpHostOffset+btmpHostSize], host)
	putUtmpTime(record, sec)
	return append(data, record...)
}

func appendWtmpUserRecord(data []byte, username string, sec uint32, line, host string) []byte {
	record := make([]byte, btmpRecordSize)
	putUtmpType(record, utmpUserProcess)
	copy(record[btmpLineOffset:btmpLineOffset+btmpLineSize], line)
	copy(record[btmpUserOffset:btmpUserOffset+btmpUserSize], username)
	copy(record[btmpHostOffset:btmpHostOffset+btmpHostSize], host)
	putUtmpTime(record, sec)
	return append(data, record...)
}

func mustParseWtmpdbISOTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed := parseWtmpdbISOTime(value)
	require.False(t, parsed.IsZero())
	return parsed
}

func putUtmpType(record []byte, value int) {
	field := record[btmpTypeOffset:]
	switch btmpTypeSize {
	case 1:
		field[0] = byte(value)
	case 2:
		binary.NativeEndian.PutUint16(field, uint16(value))
	case 4:
		binary.NativeEndian.PutUint32(field, uint32(value))
	case 8:
		binary.NativeEndian.PutUint64(field, uint64(value))
	}
}

func putUtmpTime(record []byte, sec uint32) {
	field := record[btmpTimeOffset:]
	switch btmpTimeSize {
	case 1:
		field[0] = byte(sec)
	case 2:
		binary.NativeEndian.PutUint16(field, uint16(sec))
	case 4:
		binary.NativeEndian.PutUint32(field, sec)
	case 8:
		binary.NativeEndian.PutUint64(field, uint64(sec))
	}
}
