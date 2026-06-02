package system

import (
	"strconv"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
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

func TestFailedLoginAlertIDStableAndChanges(t *testing.T) {
	id := failedLoginAlertID("user", "miguel", "login_abc")

	require.Equal(t, id, failedLoginAlertID("user", "miguel", "login_abc"))
	require.NotEqual(t, id, failedLoginAlertID("system", "miguel", "login_abc"))
	require.NotEqual(t, id, failedLoginAlertID("user", "other", "login_abc"))
	require.NotEqual(t, id, failedLoginAlertID("user", "miguel", "login_def"))
	require.True(t, isValidFailedLoginAlertID(id))
}

func TestFailedLoginAlertIDSurvivesSessionWindowChanges(t *testing.T) {
	id := failedLoginAlertID("user", "miguel", "login_abc")

	// The alert ID intentionally excludes the current session boundary. Once the
	// user dismisses an exact failed-login event, logging out and back in must not
	// create a different alert ID for the same latest failed row.
	require.Equal(t, id, failedLoginAlertID("user", "miguel", "login_abc"))
}

func TestApplyFailedLoginAlertDismissal(t *testing.T) {
	alertID := failedLoginAlertID("user", "miguel", "login_abc")
	summary := &apischema.SystemHealthSummary{
		FailedLoginAlert: &apischema.SystemFailedLoginAlert{
			ID:            alertID,
			Scope:         utils.OptionalString("user"),
			Username:      "miguel",
			Count:         2,
			LatestEventID: "login_abc",
		},
	}

	applyFailedLoginAlertDismissal(summary, &config.Dismissals{FailedLoginAlertID: alertID})

	require.Nil(t, summary.FailedLoginAlert)
}

func TestApplyFailedLoginAlertDismissalKeepsNewAlert(t *testing.T) {
	alertID := failedLoginAlertID("user", "miguel", "login_abc")
	summary := &apischema.SystemHealthSummary{
		FailedLoginAlert: &apischema.SystemFailedLoginAlert{
			ID:            alertID,
			Scope:         utils.OptionalString("user"),
			Username:      "miguel",
			Count:         2,
			LatestEventID: "login_abc",
		},
	}

	applyFailedLoginAlertDismissal(summary, &config.Dismissals{FailedLoginAlertID: failedLoginAlertID("user", "miguel", "login_def")})

	require.NotNil(t, summary.FailedLoginAlert)
	require.Equal(t, alertID, summary.FailedLoginAlert.ID)
}
