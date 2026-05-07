package system

import (
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type SystemHealthSummary struct {
	FailedServicesCount   int              `json:"failedServicesCount"`
	FailedServices        []string         `json:"failedServices,omitempty"`
	RunningServicesCount  int              `json:"runningServicesCount"`
	FailedLoginAttempts   int              `json:"failedLoginAttempts"`
	UpdatesAvailable      int              `json:"updatesAvailable"`
	UpToDate              bool             `json:"upToDate"`
	UncleanShutdown       bool             `json:"uncleanShutdown"`
	UncleanShutdownBootID string           `json:"uncleanShutdownBootId,omitempty"`
	LastLogin             *SystemLastLogin `json:"lastLogin,omitempty"`
}

type SystemLastLogin struct {
	Username string `json:"username"`
	Terminal string `json:"terminal,omitempty"`
	Source   string `json:"source,omitempty"`
	Time     string `json:"time"`
}

var healthRunCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

var (
	shortUnixLinePattern = regexp.MustCompile(`^(\d+\.\d+)\s+\S+\s+\S+\[\d+\]:\s+(.*)$`)
)

func FetchSystemHealthSummary(username string, privileged bool) (*SystemHealthSummary, error) {
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

	if count, err := FetchFailedLoginAttempts(username, privileged); err == nil {
		summary.FailedLoginAttempts = count
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

	output, err := healthRunCommand(ctx, "last", "-F", "-w", "-n", "1", username)
	if err != nil {
		return nil, err
	}

	return parseLastOutput(username, string(output)), nil
}

func FetchFailedLoginAttempts(username string, privileged bool) (int, error) {
	username = strings.TrimSpace(username)
	if username == "" || !privileged {
		return 0, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	output, err := healthRunCommand(ctx, "journalctl", "SYSLOG_IDENTIFIER=linuxio-auth", "-o", "short-unix", "--no-pager", "-n", "400")
	if err != nil {
		return 0, nil
	}

	return countPamFailedLoginAttemptsBeforeCurrentSession(username, string(output)), nil
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

var weekdayTokens = map[string]bool{
	"Mon": true, "Tue": true, "Wed": true,
	"Thu": true, "Fri": true, "Sat": true, "Sun": true,
}

func parseLastOutput(username, output string) *SystemLastLogin {
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" ||
			strings.HasPrefix(line, "wtmp ") ||
			strings.HasPrefix(line, "btmp ") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 7 || fields[0] != username {
			continue
		}

		timeStart := -1
		for i := 2; i < len(fields); i++ {
			if weekdayTokens[fields[i]] {
				timeStart = i
				break
			}
		}
		if timeStart < 0 || len(fields) < timeStart+5 {
			continue
		}

		source := ""
		if timeStart > 2 {
			source = fields[2]
		}

		return &SystemLastLogin{
			Username: username,
			Terminal: fields[1],
			Source:   source,
			Time:     strings.Join(fields[timeStart:timeStart+5], " "),
		}
	}

	return nil
}

type pamAuthEvent struct {
	time    time.Time
	success bool
	failure bool
}

func countPamFailedLoginAttemptsBeforeCurrentSession(username, output string) int {
	events := parsePamAuthEvents(username, output)
	if len(events) == 0 {
		return 0
	}

	successTimes := make([]time.Time, 0, 2)
	for _, event := range events {
		if event.success {
			successTimes = append(successTimes, event.time)
		}
	}
	if len(successTimes) == 0 {
		return 0
	}

	current := successTimes[len(successTimes)-1]
	var previous *time.Time
	if len(successTimes) >= 2 {
		prev := successTimes[len(successTimes)-2]
		previous = &prev
	}

	count := 0
	for _, event := range events {
		if !event.failure || !event.time.Before(current) {
			continue
		}
		if previous != nil && !event.time.After(*previous) {
			continue
		}
		count++
	}

	return count
}

func parsePamAuthEvents(username, output string) []pamAuthEvent {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil
	}

	lines := strings.Split(output, "\n")
	events := make([]pamAuthEvent, 0, len(lines))
	successMarker := "pam_unix(linuxio:session): session opened for user " + username + "("
	failureMarker := "pam_unix(linuxio:auth): authentication failure"
	failureUserMarker := "user=" + username

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		match := shortUnixLinePattern.FindStringSubmatch(line)
		if len(match) != 3 {
			continue
		}

		seconds, err := strconv.ParseFloat(match[1], 64)
		if err != nil {
			continue
		}
		timestamp := time.Unix(int64(seconds), int64((seconds-float64(int64(seconds)))*float64(time.Second)))
		message := match[2]

		switch {
		case strings.Contains(message, successMarker):
			events = append(events, pamAuthEvent{time: timestamp, success: true})
		case strings.Contains(message, failureMarker) && strings.Contains(message, failureUserMarker):
			events = append(events, pamAuthEvent{time: timestamp, failure: true})
		}
	}

	return events
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
