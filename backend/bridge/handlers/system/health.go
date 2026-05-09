package system

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/loginhistory"
)

type SystemHealthSummary struct {
	FailedServicesCount   int               `json:"failedServicesCount"`
	FailedServices        []string          `json:"failedServices,omitempty"`
	RunningServicesCount  int               `json:"runningServicesCount"`
	FailedLoginAlert      *FailedLoginAlert `json:"failedLoginAlert,omitempty"`
	UpdatesAvailable      int               `json:"updatesAvailable"`
	UpToDate              bool              `json:"upToDate"`
	UncleanShutdown       bool              `json:"uncleanShutdown"`
	UncleanShutdownBootID string            `json:"uncleanShutdownBootId,omitempty"`
	LastLogin             *SystemLastLogin  `json:"lastLogin,omitempty"`
}

type SystemLastLogin struct {
	Username string `json:"username"`
	Terminal string `json:"terminal,omitempty"`
	Source   string `json:"source,omitempty"`
	Time     string `json:"time"`
}

type FailedLoginAlert struct {
	ID            string           `json:"id"`
	Username      string           `json:"username"`
	Count         int              `json:"count"`
	LatestEventID string           `json:"latestEventId"`
	LatestEvent   SystemLoginEvent `json:"latestEvent"`
}

type SystemLoginEvent struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Terminal  string `json:"terminal"`
	Source    string `json:"source"`
	Time      string `json:"time"`
	StartedAt string `json:"startedAt,omitempty"`
	Status    string `json:"status"`
}

var healthRunCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

func FetchSystemHealthSummary(username string, privileged bool, sessionStartedAt time.Time) (*SystemHealthSummary, error) {
	summary := &SystemHealthSummary{
		UpToDate: true,
	}

	if services, err := FetchServices(); err == nil {
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

	if updates, err := GetUpdatesFast(); err == nil && updates != nil {
		summary.UpdatesAvailable = len(updates.Updates)
		summary.UpToDate = summary.UpdatesAvailable == 0
	}

	if login, err := FetchLastSuccessfulLogin(username); err == nil {
		summary.LastLogin = login
	}

	if privileged {
		alert, err := FetchFailedLoginAlert(username, sessionStartedAt)
		if err == nil {
			summary.FailedLoginAlert = alert
		}
	}

	if unclean, bootID, err := DetectUncleanShutdown(); err == nil {
		summary.UncleanShutdown = unclean
		summary.UncleanShutdownBootID = bootID
	}

	return summary, nil
}

func FetchLastSuccessfulLogin(username string) (*SystemLastLogin, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	login, err := loginhistory.FetchLast(ctx, username)
	if err != nil || login == nil {
		return nil, err
	}

	return &SystemLastLogin{
		Username: login.Username,
		Terminal: login.Terminal,
		Source:   login.Source,
		Time:     login.Time,
	}, nil
}

func FetchFailedLoginAttempts(username string, sessionStartedAt time.Time) (int, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return 0, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	return loginhistory.FetchFailedAttempts(ctx, username, sessionStartedAt)
}

func FetchFailedLoginAlert(username string, sessionStartedAt time.Time) (*FailedLoginAlert, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	batch, err := loginhistory.FetchFailedAttemptBatch(ctx, username, sessionStartedAt)
	if err != nil || batch == nil {
		return nil, err
	}

	latestEvent := systemLoginEventFromLogin(batch.Latest)
	alert := &FailedLoginAlert{
		ID:            failedLoginAlertID(username, batch.Latest.ID),
		Username:      username,
		Count:         batch.Count,
		LatestEventID: batch.Latest.ID,
		LatestEvent:   latestEvent,
	}
	return alert, nil
}

func systemLoginEventFromLogin(login loginhistory.Login) SystemLoginEvent {
	startedAt := ""
	if !login.StartedAt.IsZero() {
		startedAt = login.StartedAt.Format(time.RFC3339)
	}
	return SystemLoginEvent{
		ID:        login.ID,
		Username:  login.Username,
		Terminal:  login.Terminal,
		Source:    login.Source,
		Time:      login.Time,
		StartedAt: startedAt,
		Status:    login.Status,
	}
}

func failedLoginAlertID(username, latestEventID string) string {
	payload := strings.Join([]string{
		strings.TrimSpace(username),
		strings.TrimSpace(latestEventID),
	}, "\x1f")
	sum := sha256.Sum256([]byte(payload))
	return "failed_login_" + hex.EncodeToString(sum[:])
}

func DetectUncleanShutdown() (bool, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	output, err := healthRunCommand(ctx, "last", "-x", "-F", "-n", "6", "reboot", "shutdown")
	if err != nil {
		return false, "", err
	}

	unclean, bootID := parseUncleanShutdownOutput(string(output))
	return unclean, bootID, nil
}

// parseUncleanShutdownOutput inspects `last -x -F` output and reports whether
// the previous boot ended uncleanly. When unclean, it also returns a stable
// identifier derived from the previous boot's start timestamp so that a
// dismissal can be scoped to that specific event.
//
// The check matches Cockpit's: a previous boot is unclean when its line in
// `last` is marked "still running" (util-linux convention for an unfinished
// session) or "crash" (wtmpdb convention). Anything else — including a clean
// shutdown record between the two reboots — counts as clean.
func parseUncleanShutdownOutput(output string) (bool, string) {
	lines := strings.Split(output, "\n")
	foundCurrent := false
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" ||
			strings.HasPrefix(line, "wtmp begins") ||
			strings.HasPrefix(line, "btmp begins") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		kind := fields[0]
		if kind != "reboot" && kind != "shutdown" {
			continue
		}

		if !foundCurrent {
			// First valid event = most recent. On any running system this
			// should be the current boot; bail out otherwise.
			if kind != "reboot" {
				return false, ""
			}
			foundCurrent = true
			continue
		}

		// Second valid event = what came before the current boot.
		if kind == "shutdown" {
			return false, ""
		}
		// Previous reboot with no shutdown between it and the current boot.
		// Only flag when `last` itself marks the session as unfinished, to
		// avoid false positives from rotated/truncated wtmp files.
		if !strings.Contains(line, "crash") && !strings.Contains(line, "still running") {
			return false, ""
		}
		return true, extractBootTimestamp(line)
	}
	return false, ""
}

var lastFullTimePattern = regexp.MustCompile(`\b([A-Z][a-z]{2})\s+([A-Z][a-z]{2})\s+(\d{1,2})\s+(\d{2}:\d{2}:\d{2})\s+(\d{4})\b`)

// extractBootTimestamp returns the unix-epoch seconds of the boot start time
// found in a `last -F` line. Empty when the line can't be parsed — callers
// treat that as "no dismissal possible" and keep showing the warning.
func extractBootTimestamp(line string) string {
	m := lastFullTimePattern.FindStringSubmatch(line)
	if len(m) != 6 {
		return ""
	}
	t, err := time.Parse("Mon Jan 2 15:04:05 2006",
		strings.Join([]string{m[1], m[2], m[3], m[4], m[5]}, " "))
	if err != nil {
		return ""
	}
	return strconv.FormatInt(t.Unix(), 10)
}
