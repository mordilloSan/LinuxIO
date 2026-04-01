package system

import (
	"context"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

type SystemHealthSummary struct {
	FailedServicesCount int              `json:"failedServicesCount"`
	FailedServices      []string         `json:"failedServices,omitempty"`
	FailedLoginAttempts int              `json:"failedLoginAttempts"`
	UpdatesAvailable    int              `json:"updatesAvailable"`
	UpToDate            bool             `json:"upToDate"`
	UncleanShutdown     bool             `json:"uncleanShutdown"`
	LastLogin           *SystemLastLogin `json:"lastLogin,omitempty"`
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

var isoLoginTimestampPattern = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:Z|[+-]\d{2}:\d{2})`)

func FetchSystemHealthSummary(username string, privileged bool) (*SystemHealthSummary, error) {
	summary := &SystemHealthSummary{
		UpToDate: true,
	}

	if services, err := FetchServices(); err == nil {
		for _, service := range services {
			if !service.Failed {
				continue
			}
			summary.FailedServicesCount++
			summary.FailedServices = append(summary.FailedServices, service.Name)
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

	if unclean, err := DetectUncleanShutdown(); err == nil {
		summary.UncleanShutdown = unclean
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

	output, err := healthRunCommand(ctx, "lastlog", "-u", username)
	if err != nil {
		return nil, err
	}

	return parseLastlogOutput(username, string(output)), nil
}

func FetchFailedLoginAttempts(username string, privileged bool) (int, error) {
	username = strings.TrimSpace(username)
	if username == "" || !privileged {
		return 0, nil
	}

	currentLogin, previousLogin, err := FetchRecentSuccessfulLoginTimes(username)
	if err != nil || currentLogin.IsZero() {
		return 0, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	output, err := healthRunCommand(ctx, "lastb", "--time-format", "iso", "-w", "-n", "100", username)
	if err != nil {
		return 0, nil
	}

	return countFailedLoginAttemptsBetween(string(output), previousLogin, currentLogin), nil
}

func FetchRecentSuccessfulLoginTimes(username string) (time.Time, *time.Time, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return time.Time{}, nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	output, err := healthRunCommand(ctx, "last", "--time-format", "iso", "-w", "-n", "20", username)
	if err != nil {
		return time.Time{}, nil, err
	}

	current, previous := parseRecentSuccessfulLoginTimes(string(output))
	return current, previous, nil
}

func DetectUncleanShutdown() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	output, err := healthRunCommand(ctx, "last", "-x", "-F", "-n", "6", "reboot", "shutdown")
	if err != nil {
		return false, err
	}

	return parseUncleanShutdownOutput(string(output)), nil
}

func parseLastlogOutput(username, output string) *SystemLastLogin {
	lines := strings.Split(output, "\n")
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Username") {
			continue
		}
		if strings.Contains(line, "**Never logged in**") {
			return nil
		}

		fields := strings.Fields(line)
		if len(fields) < 5 {
			return nil
		}

		timeStart := 3
		if fields[0] != username && len(fields) >= 6 {
			timeStart = 4
		}

		return &SystemLastLogin{
			Username: username,
			Terminal: fields[1],
			Source:   fields[2],
			Time:     strings.Join(fields[timeStart:], " "),
		}
	}

	return nil
}

func parseRecentSuccessfulLoginTimes(output string) (time.Time, *time.Time) {
	logins := extractISOLoginTimes(output)
	if len(logins) == 0 {
		return time.Time{}, nil
	}
	if len(logins) == 1 {
		return logins[0], nil
	}

	previous := logins[1]
	return logins[0], &previous
}

func countFailedLoginAttemptsBetween(output string, previous *time.Time, current time.Time) int {
	if current.IsZero() {
		return 0
	}

	count := 0
	for _, attempt := range extractISOLoginTimes(output) {
		if !attempt.Before(current) {
			continue
		}
		if previous != nil && !attempt.After(*previous) {
			continue
		}
		count++
	}

	return count
}

func extractISOLoginTimes(output string) []time.Time {
	lines := strings.Split(output, "\n")
	times := make([]time.Time, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" ||
			strings.HasPrefix(line, "wtmp begins") ||
			strings.HasPrefix(line, "btmp begins") {
			continue
		}

		match := isoLoginTimestampPattern.FindString(line)
		if match == "" {
			continue
		}

		parsed, err := time.Parse(time.RFC3339, match)
		if err != nil {
			continue
		}

		times = append(times, parsed)
	}

	return times
}

func parseUncleanShutdownOutput(output string) bool {
	lines := strings.Split(output, "\n")
	events := make([]string, 0, 4)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" ||
			strings.HasPrefix(line, "wtmp begins") ||
			strings.HasPrefix(line, "btmp begins") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		switch fields[0] {
		case "reboot", "shutdown":
			events = append(events, fields[0])
		}
	}

	if len(events) < 2 {
		return false
	}

	return events[0] == "reboot" && events[1] != "shutdown"
}
