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
	FailedServicesCount  int              `json:"failedServicesCount"`
	FailedServices       []string         `json:"failedServices,omitempty"`
	RunningServicesCount int              `json:"runningServicesCount"`
	FailedLoginAttempts  int              `json:"failedLoginAttempts"`
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

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	output, err := healthRunCommand(ctx, "journalctl", "SYSLOG_IDENTIFIER=linuxio-auth", "-o", "short-unix", "--no-pager", "-n", "400")
	if err != nil {
		return 0, nil
	}

	return countPamFailedLoginAttemptsBeforeCurrentSession(username, string(output)), nil
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
