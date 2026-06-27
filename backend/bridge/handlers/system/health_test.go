package system

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
	"github.com/stretchr/testify/require"
)

func TestDetectUncleanShutdownUsesPreviousUncleanBoot(t *testing.T) {
	originalFetchPreviousUncleanBoot := healthFetchPreviousUncleanBoot
	t.Cleanup(func() { healthFetchPreviousUncleanBoot = originalFetchPreviousUncleanBoot })

	previous := time.Date(2026, time.April, 1, 14, 0, 0, 0, time.UTC)

	healthFetchPreviousUncleanBoot = func(context.Context) (time.Time, bool, error) {
		return previous, true, nil
	}

	unclean, bootID, err := DetectUncleanShutdown(context.Background())

	require.NoError(t, err)
	require.True(t, unclean)
	require.Equal(t, strconv.FormatInt(previous.Unix(), 10), bootID)
}

func TestDetectUncleanShutdownClean(t *testing.T) {
	originalFetchPreviousUncleanBoot := healthFetchPreviousUncleanBoot
	t.Cleanup(func() { healthFetchPreviousUncleanBoot = originalFetchPreviousUncleanBoot })

	healthFetchPreviousUncleanBoot = func(context.Context) (time.Time, bool, error) {
		return time.Time{}, false, nil
	}

	unclean, bootID, err := DetectUncleanShutdown(context.Background())

	require.NoError(t, err)
	require.False(t, unclean)
	require.Empty(t, bootID)
}

func TestDetectUncleanShutdownTreatsMissingBootHistoryAsUnknown(t *testing.T) {
	originalFetchPreviousUncleanBoot := healthFetchPreviousUncleanBoot
	t.Cleanup(func() { healthFetchPreviousUncleanBoot = originalFetchPreviousUncleanBoot })

	healthFetchPreviousUncleanBoot = func(context.Context) (time.Time, bool, error) {
		return time.Time{}, false, errors.New("boot history unavailable")
	}

	unclean, bootID, err := DetectUncleanShutdown(context.Background())

	require.NoError(t, err)
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

	applyFailedLoginAlertDismissal(summary, &config.PersistedDismissals{FailedLoginAlertID: alertID})

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

	applyFailedLoginAlertDismissal(summary, &config.PersistedDismissals{FailedLoginAlertID: failedLoginAlertID("user", "miguel", "login_def")})

	require.NotNil(t, summary.FailedLoginAlert)
	require.Equal(t, alertID, summary.FailedLoginAlert.ID)
}
