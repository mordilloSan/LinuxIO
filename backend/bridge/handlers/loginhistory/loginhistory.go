package loginhistory

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type Login struct {
	Username  string
	Terminal  string
	Source    string
	Time      string
	StartedAt time.Time
}

var runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

var readFile = os.ReadFile

var weekdayTokens = map[string]bool{
	"Mon": true, "Tue": true, "Wed": true,
	"Thu": true, "Fri": true, "Sat": true, "Sun": true,
}

const (
	// /var/log/btmp uses the Linux glibc utmp record layout.
	btmpPath         = "/var/log/btmp"
	btmpRecordSize   = 384
	btmpTypeOffset   = 0
	btmpUserOffset   = 44
	btmpUserSize     = 32
	btmpTimeOffset   = 340
	utmpLoginProcess = 6
	utmpUserProcess  = 7
	currentLoginSkew = 2 * time.Second
)

func FetchLast(ctx context.Context, username string) (*Login, error) {
	logins, err := FetchRecent(ctx, username, 1)
	if err != nil || len(logins) == 0 {
		return nil, err
	}
	return &logins[0], nil
}

func FetchRecent(ctx context.Context, username string, limit int) ([]Login, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, nil
	}

	if logins, err := fetchWtmpdb(ctx, username, limit); err == nil {
		return logins, nil
	}

	args := []string{"-F", "-w"}
	if limit > 0 {
		args = append(args, "-n", strconv.Itoa(limit))
	}
	args = append(args, username)

	output, err := runCommand(ctx, "last", args...)
	if err != nil {
		return nil, err
	}
	return ParseLastOutput(username, string(output)), nil
}

func FetchByUser(ctx context.Context) (map[string]Login, error) {
	logins, err := fetchWtmpdb(ctx, "", 0)
	if err != nil {
		output, lastErr := runCommand(ctx, "last", "-F", "-w")
		if lastErr != nil {
			return nil, lastErr
		}
		logins = ParseLastOutput("", string(output))
	}
	return firstLoginByUser(logins), nil
}

func FetchFailedAttempts(ctx context.Context, username string, sessionStartedAt time.Time) (int, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return 0, nil
	}

	logins, err := FetchRecent(ctx, username, 50)
	if err != nil {
		return 0, err
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return 0, ctxErr
	}

	data, err := readFile(btmpPath)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	since := previousSuccessfulLoginUnixBefore(logins, sessionStartedAt)
	if sessionStartedAt.IsZero() {
		return countBtmpFailuresBetween(username, data, since, 0), nil
	}
	return countBtmpFailuresBetween(username, data, since, sessionStartedAt.Unix()+1), nil
}

func ParseLastOutput(username, output string) []Login {
	username = strings.TrimSpace(username)
	logins := make([]Login, 0)

	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" ||
			strings.HasPrefix(line, "wtmp ") ||
			strings.HasPrefix(line, "wtmpdb ") ||
			strings.HasPrefix(line, "btmp ") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 7 {
			continue
		}
		if username != "" && fields[0] != username {
			continue
		}
		if !isLoginUser(fields[0]) {
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

		terminal, source := splitTerminalAndSource(fields[1:timeStart])
		timeText := normalizeLastTime(strings.Join(fields[timeStart:timeStart+5], " "))

		logins = append(logins, Login{
			Username:  fields[0],
			Terminal:  terminal,
			Source:    source,
			Time:      timeText,
			StartedAt: parseLastTime(timeText),
		})
	}

	return logins
}

type wtmpdbOutput struct {
	Entries []wtmpdbEntry `json:"entries"`
}

type wtmpdbEntry struct {
	User     string `json:"user"`
	TTY      string `json:"tty"`
	Hostname string `json:"hostname"`
	Login    string `json:"login"`
}

func fetchWtmpdb(ctx context.Context, username string, limit int) ([]Login, error) {
	args := []string{"last", "-j", "--time-format", "full", "-w"}
	if limit > 0 {
		args = append(args, "-n", strconv.Itoa(limit))
	}
	if username != "" {
		args = append(args, username)
	}

	output, err := runCommand(ctx, "wtmpdb", args...)
	if err != nil {
		return nil, err
	}

	return ParseWtmpdbOutput(username, output)
}

func ParseWtmpdbOutput(username string, output []byte) ([]Login, error) {
	username = strings.TrimSpace(username)

	var decoded wtmpdbOutput
	if err := json.Unmarshal(output, &decoded); err != nil {
		return nil, err
	}

	logins := make([]Login, 0, len(decoded.Entries))
	for _, entry := range decoded.Entries {
		entry.User = strings.TrimSpace(entry.User)
		if username != "" && entry.User != username {
			continue
		}
		if !isLoginUser(entry.User) {
			continue
		}

		timeText := normalizeLastTime(entry.Login)
		logins = append(logins, Login{
			Username:  entry.User,
			Terminal:  strings.TrimSpace(entry.TTY),
			Source:    strings.TrimSpace(entry.Hostname),
			Time:      timeText,
			StartedAt: parseLastTime(timeText),
		})
	}

	return logins, nil
}

func firstLoginByUser(logins []Login) map[string]Login {
	byUser := make(map[string]Login)
	for _, login := range logins {
		if _, ok := byUser[login.Username]; ok {
			continue
		}
		byUser[login.Username] = login
	}
	return byUser
}

func previousSuccessfulLoginUnix(logins []Login) int64 {
	if len(logins) < 2 || logins[1].StartedAt.IsZero() {
		return 0
	}
	return logins[1].StartedAt.Unix()
}

func previousSuccessfulLoginUnixBefore(logins []Login, sessionStartedAt time.Time) int64 {
	if sessionStartedAt.IsZero() {
		return previousSuccessfulLoginUnix(logins)
	}

	cutoff := sessionStartedAt.Add(-currentLoginSkew)
	for _, login := range logins {
		if login.StartedAt.IsZero() || !login.StartedAt.Before(cutoff) {
			continue
		}
		return login.StartedAt.Unix()
	}
	return 0
}

func countBtmpFailuresSince(username string, data []byte, since int64) int {
	return countBtmpFailuresBetween(username, data, since, 0)
}

func countBtmpFailuresBetween(username string, data []byte, since, until int64) int {
	username = strings.TrimSpace(username)
	if username == "" {
		return 0
	}

	count := 0
	for len(data) >= btmpRecordSize {
		record := data[:btmpRecordSize]
		data = data[btmpRecordSize:]

		recordType := binary.LittleEndian.Uint16(record[btmpTypeOffset:])
		if recordType != utmpLoginProcess && recordType != utmpUserProcess {
			continue
		}
		if fixedCString(record[btmpUserOffset:btmpUserOffset+btmpUserSize]) != username {
			continue
		}
		recordTime := int64(binary.LittleEndian.Uint32(record[btmpTimeOffset:]))
		if recordTime <= since {
			continue
		}
		if until > 0 && recordTime > until {
			continue
		}
		count++
	}
	return count
}

func fixedCString(value []byte) string {
	for i, b := range value {
		if b == 0 {
			return string(value[:i])
		}
	}
	return string(value)
}

func splitTerminalAndSource(fields []string) (terminal string, source string) {
	switch len(fields) {
	case 0:
		return "", ""
	case 1:
		return fields[0], ""
	default:
		source = fields[len(fields)-1]
		terminal = strings.Join(fields[:len(fields)-1], " ")
		return terminal, source
	}
}

func normalizeLastTime(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func parseLastTime(value string) time.Time {
	value = normalizeLastTime(value)
	for _, layout := range []string{
		"Mon Jan 2 15:04:05 2006",
		"Mon Jan _2 15:04:05 2006",
	} {
		t, err := time.Parse(layout, value)
		if err == nil {
			return t
		}
	}
	return time.Time{}
}

func isLoginUser(username string) bool {
	switch username {
	case "", "reboot", "shutdown", "runlevel", "wtmp", "btmp":
		return false
	default:
		return true
	}
}
