package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
)

func TestDismissalsStoreWhenHistoryWasSeen(t *testing.T) {
	cfg := userconfig.Settings{Dismissals: &userconfig.PersistedDismissals{FailedLoginAlertID: "login-id"}}
	set := func(value string) error {
		return applyConfigPayload(&cfg, &apischema.ConfigSetPayload{
			Dismissals: &apischema.ConfigDismissalsPayload{HistorySeenAt: &value},
		})
	}

	// The browser sends Date.toISOString(), which carries milliseconds.
	seen := "2026-11-10T09:30:00.123Z"
	require.NoError(t, set(" "+seen+" "))
	require.Equal(t, seen, cfg.Dismissals.HistorySeenAt)
	require.Equal(t, "login-id", cfg.Dismissals.FailedLoginAlertID, "setting one dismissal keeps the others")
	require.Equal(t, seen, apischema.StringValue(appConfigToAPI(cfg, userconfig.StorageModeHome).Dismissals.HistorySeenAt))

	boot := "boot-id"
	require.NoError(t, applyConfigPayload(&cfg, &apischema.ConfigSetPayload{
		Dismissals: &apischema.ConfigDismissalsPayload{UncleanShutdownBootID: &boot},
	}))
	require.Equal(t, seen, cfg.Dismissals.HistorySeenAt, "setting another dismissal keeps the history time")

	for _, bad := range []string{"yesterday", "2026-11-10", "2026-11-10 09:30:00", "2026-13-10T09:30:00Z"} {
		require.ErrorIs(t, set(bad), bridgeipc.ErrInvalidArgs, bad)
		require.Equal(t, seen, cfg.Dismissals.HistorySeenAt, "a refused time leaves the stored one: %s", bad)
	}

	require.NoError(t, set(""))
	require.Empty(t, cfg.Dismissals.HistorySeenAt, "an empty time clears it")
	require.Equal(t, "login-id", cfg.Dismissals.FailedLoginAlertID)
	require.Equal(t, "boot-id", cfg.Dismissals.UncleanShutdownBootID)

	empty := ""
	require.NoError(t, applyConfigPayload(&cfg, &apischema.ConfigSetPayload{
		Dismissals: &apischema.ConfigDismissalsPayload{FailedLoginAlertID: &empty, UncleanShutdownBootID: &empty},
	}))
	require.Nil(t, cfg.Dismissals, "clearing every dismissal drops the block")

	require.NoError(t, set(seen))
	require.Equal(t, &userconfig.PersistedDismissals{HistorySeenAt: seen}, cfg.Dismissals, "the history time alone keeps the block")
}

// The time is stored as the server writes it, in UTC, and never later than
// the server's now: a browser clock running ahead would otherwise hide the
// failures that come after it from the next sign-in notice.
func TestDismissalsStoreTheHistoryTimeNormalisedAndNotAhead(t *testing.T) {
	serverNow := time.Date(2026, 11, 11, 8, 0, 0, 0, time.UTC)
	previous := now
	now = func() time.Time { return serverNow }
	t.Cleanup(func() { now = previous })

	for sent, want := range map[string]string{
		"2026-11-11T09:30:00.123+02:00":  "2026-11-11T07:30:00.123Z",
		"2026-11-11T07:30:00.000000000Z": "2026-11-11T07:30:00Z",
		"2026-11-11T08:00:00Z":           "2026-11-11T08:00:00Z",
		"2026-11-11T08:00:00.001Z":       "2026-11-11T08:00:00Z",
		"9999-12-31T23:59:59Z":           "2026-11-11T08:00:00Z",
	} {
		cfg := userconfig.Settings{}
		require.NoError(t, applyConfigPayload(&cfg, &apischema.ConfigSetPayload{
			Dismissals: &apischema.ConfigDismissalsPayload{HistorySeenAt: &sent},
		}), sent)
		require.Equal(t, want, cfg.Dismissals.HistorySeenAt, sent)
	}
}
