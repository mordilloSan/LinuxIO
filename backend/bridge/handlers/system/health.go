package system

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/accounts/loginhistory"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

var healthFetchPreviousUncleanBoot = loginhistory.FetchPreviousUncleanBoot

func FetchSystemHealthSummary(ctx context.Context, username string, privileged bool, sessionStartedAt time.Time) (*apischema.SystemHealthSummary, error) {
	summary := &apischema.SystemHealthSummary{
		UpToDate: true,
	}

	if services, err := FetchServices(ctx); err == nil {
		for _, service := range services {
			if service.Failed {
				summary.FailedServicesCount++
				summary.FailedServices = append(summary.FailedServices, service.Name)
			}
			if service.SubState == "running" {
				summary.RunningServicesCount++
			}
		}
	}

	if updates, err := GetUpdatesFast(ctx); err == nil && updates != nil {
		summary.UpdatesAvailable = len(updates.Updates)
		summary.UpToDate = summary.UpdatesAvailable == 0
	}

	if login, err := FetchLastSuccessfulLogin(ctx, username); err == nil {
		summary.LastLogin = login
	}

	if privileged {
		alert, err := FetchSystemFailedLoginAlert(ctx, username, sessionStartedAt)
		if err == nil {
			summary.FailedLoginAlert = alert
		}
	}

	if unclean, bootID, err := DetectUncleanShutdown(ctx); err == nil {
		summary.UncleanShutdown = unclean
		summary.UncleanShutdownBootID = utils.OptionalString(bootID)
	}

	return summary, nil
}

func FetchLastSuccessfulLogin(parent context.Context, username string) (*apischema.SystemLastLogin, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()

	login, err := loginhistory.FetchLast(ctx, username)
	if err != nil || login == nil {
		return nil, err
	}

	return &apischema.SystemLastLogin{
		Username: login.Username,
		Terminal: utils.OptionalString(login.Terminal),
		Source:   utils.OptionalString(login.Source),
		Time:     login.Time,
	}, nil
}

func FetchSystemFailedLoginAlert(parent context.Context, boundaryUsername string, sessionStartedAt time.Time) (*apischema.SystemFailedLoginAlert, error) {
	boundaryUsername = strings.TrimSpace(boundaryUsername)
	if boundaryUsername == "" {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()

	batch, err := loginhistory.FetchFailedAttemptBatchForAllUsers(ctx, boundaryUsername, sessionStartedAt)
	if err != nil || batch == nil {
		return nil, err
	}

	latestEvent := systemLoginEventFromLogin(batch.Latest)
	alert := &apischema.SystemFailedLoginAlert{
		ID:            failedLoginAlertID("system", boundaryUsername, batch.Latest.ID),
		Scope:         utils.OptionalString("system"),
		Username:      batch.Latest.Username,
		Count:         batch.Count,
		LatestEventID: batch.Latest.ID,
		LatestEvent:   latestEvent,
	}
	return alert, nil
}

func FetchFailedLoginEvents(ctx context.Context, boundaryUsername string, sessionStartedAt time.Time, limit int) ([]apischema.AccountUserLogin, error) {
	boundaryUsername = strings.TrimSpace(boundaryUsername)
	if boundaryUsername == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 24
	}

	logins, err := loginhistory.FetchFailedAttemptEventsForAllUsers(ctx, boundaryUsername, sessionStartedAt, limit)
	if err != nil {
		return nil, err
	}

	events := make([]apischema.AccountUserLogin, 0, len(logins))
	for _, login := range logins {
		events = append(events, systemLoginEventFromLogin(login))
	}
	return events, nil
}

func systemLoginEventFromLogin(login loginhistory.Login) apischema.AccountUserLogin {
	startedAt := ""
	if !login.StartedAt.IsZero() {
		startedAt = login.StartedAt.Format(time.RFC3339)
	}
	return apischema.AccountUserLogin{
		ID:        login.ID,
		Username:  login.Username,
		Terminal:  login.Terminal,
		Source:    login.Source,
		Time:      login.Time,
		StartedAt: utils.OptionalString(startedAt),
		Status:    login.Status,
	}
}

func failedLoginAlertID(scope, username, latestEventID string) string {
	payload := strings.Join([]string{
		strings.TrimSpace(scope),
		strings.TrimSpace(username),
		strings.TrimSpace(latestEventID),
	}, "\x1f")
	sum := sha256.Sum256([]byte(payload))
	return "failed_login_" + hex.EncodeToString(sum[:])
}

func DetectUncleanShutdown(parent context.Context) (bool, string, error) {
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()

	startedAt, unclean, err := healthFetchPreviousUncleanBoot(ctx)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return false, "", ctxErr
		}
		return false, "", nil
	}
	if !unclean {
		return false, "", nil
	}
	if startedAt.IsZero() {
		return true, "", nil
	}
	return true, strconv.FormatInt(startedAt.Unix(), 10), nil
}
