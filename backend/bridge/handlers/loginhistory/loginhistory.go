//go:generate go run ./gen_utmp_layout.go

package loginhistory

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	LoginStatusSuccess = "success"
	LoginStatusFailed  = "failed"
)

type Login struct {
	ID        string
	Username  string
	Terminal  string
	Source    string
	Time      string
	StartedAt time.Time
	Status    string
}

type FailedAttemptBatch struct {
	SinceUnix int64
	UntilUnix int64
	Count     int
	Latest    Login
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
	btmpPath         = "/var/log/btmp"
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

func FetchRecentEvents(ctx context.Context, username string, limit int) ([]Login, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 12
	}

	successes, err := FetchRecent(ctx, username, limit)
	if err != nil {
		return nil, err
	}

	failures, err := FetchRecentFailures(ctx, username, limit)
	if err != nil {
		return nil, err
	}

	logins := make([]Login, 0, len(successes)+len(failures))
	logins = append(logins, successes...)
	logins = append(logins, failures...)
	sortLoginsNewestFirst(logins)
	if len(logins) > limit {
		logins = logins[:limit]
	}
	return logins, nil
}

func FetchRecentFailures(ctx context.Context, username string, limit int) ([]Login, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, nil
	}

	data, err := readFile(btmpPath)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}

	return parseBtmpFailures(username, data, limit), nil
}

func FetchFailedAttemptBatch(ctx context.Context, username string, sessionStartedAt time.Time) (*FailedAttemptBatch, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, nil
	}

	logins, err := FetchRecent(ctx, username, 50)
	if err != nil {
		return nil, err
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}

	data, err := readFile(btmpPath)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	since := previousSuccessfulLoginUnixBefore(logins, sessionStartedAt)
	until := int64(0)
	if !sessionStartedAt.IsZero() {
		until = sessionStartedAt.Unix() + 1
	}

	failures := parseBtmpFailuresBetween(username, data, since, until, 0)
	if len(failures) == 0 {
		return nil, nil
	}

	return &FailedAttemptBatch{
		SinceUnix: since,
		UntilUnix: until,
		Count:     len(failures),
		Latest:    failures[0],
	}, nil
}

func FetchFailedAttempts(ctx context.Context, username string, sessionStartedAt time.Time) (int, error) {
	batch, err := FetchFailedAttemptBatch(ctx, username, sessionStartedAt)
	if err != nil || batch == nil {
		return 0, err
	}
	return batch.Count, nil
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

		login := Login{
			Username:  fields[0],
			Terminal:  terminal,
			Source:    source,
			Time:      timeText,
			StartedAt: parseLastTime(timeText),
			Status:    LoginStatusSuccess,
		}
		login.ID = StableLoginID(login)
		logins = append(logins, login)
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
		login := Login{
			Username:  entry.User,
			Terminal:  strings.TrimSpace(entry.TTY),
			Source:    strings.TrimSpace(entry.Hostname),
			Time:      timeText,
			StartedAt: parseLastTime(timeText),
			Status:    LoginStatusSuccess,
		}
		login.ID = StableLoginID(login)
		logins = append(logins, login)
	}

	return logins, nil
}

func parseBtmpFailures(username string, data []byte, limit int) []Login {
	return parseBtmpFailuresBetween(username, data, 0, 0, limit)
}

func parseBtmpFailuresBetween(username string, data []byte, since, until int64, limit int) []Login {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil
	}

	recordCount := len(data) / btmpRecordSize
	failures := make([]Login, 0)
	for i := recordCount - 1; i >= 0; i-- {
		record := data[i*btmpRecordSize : (i+1)*btmpRecordSize]

		recordType := utmpRecordType(record)
		if recordType != utmpLoginProcess && recordType != utmpUserProcess {
			continue
		}

		recordUser := strings.TrimSpace(fixedCString(record[btmpUserOffset : btmpUserOffset+btmpUserSize]))
		if recordUser != username || !isLoginUser(recordUser) {
			continue
		}

		recordTime := utmpRecordTime(record)
		if recordTime <= 0 {
			continue
		}
		if recordTime <= since {
			continue
		}
		if until > 0 && recordTime > until {
			continue
		}

		startedAt := time.Unix(recordTime, 0)
		login := Login{
			Username:  recordUser,
			Terminal:  strings.TrimSpace(fixedCString(record[btmpLineOffset : btmpLineOffset+btmpLineSize])),
			Source:    strings.TrimSpace(fixedCString(record[btmpHostOffset : btmpHostOffset+btmpHostSize])),
			Time:      formatLoginTime(startedAt),
			StartedAt: startedAt,
			Status:    LoginStatusFailed,
		}
		login.ID = StableLoginID(login)
		failures = append(failures, login)

		if limit > 0 && len(failures) >= limit {
			break
		}
	}
	return failures
}

func StableLoginID(login Login) string {
	startedAt := "0"
	if !login.StartedAt.IsZero() {
		startedAt = strconv.FormatInt(login.StartedAt.Unix(), 10)
	}

	payload := strings.Join([]string{
		strings.TrimSpace(login.Status),
		strings.TrimSpace(login.Username),
		startedAt,
		strings.TrimSpace(login.Terminal),
		strings.TrimSpace(login.Source),
	}, "\x1f")
	sum := sha256.Sum256([]byte(payload))
	return "login_" + hex.EncodeToString(sum[:])
}

func sortLoginsNewestFirst(logins []Login) {
	sort.SliceStable(logins, func(i, j int) bool {
		left := logins[i].StartedAt
		right := logins[j].StartedAt
		if !left.IsZero() && !right.IsZero() && !left.Equal(right) {
			return left.After(right)
		}
		if !left.IsZero() != !right.IsZero() {
			return !left.IsZero()
		}
		return logins[i].Time > logins[j].Time
	})
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

		recordType := utmpRecordType(record)
		if recordType != utmpLoginProcess && recordType != utmpUserProcess {
			continue
		}
		if fixedCString(record[btmpUserOffset:btmpUserOffset+btmpUserSize]) != username {
			continue
		}
		recordTime := utmpRecordTime(record)
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

func utmpRecordType(record []byte) uint16 {
	if !hasUtmpField(record, btmpTypeOffset, btmpTypeSize) {
		return 0
	}
	field := record[btmpTypeOffset:]
	switch btmpTypeSize {
	case 1:
		return uint16(field[0])
	case 2:
		return binary.NativeEndian.Uint16(field)
	case 4:
		return uint16(binary.NativeEndian.Uint32(field))
	case 8:
		return uint16(binary.NativeEndian.Uint64(field))
	default:
		return 0
	}
}

func utmpRecordTime(record []byte) int64 {
	if !hasUtmpField(record, btmpTimeOffset, btmpTimeSize) {
		return 0
	}
	field := record[btmpTimeOffset:]
	switch btmpTimeSize {
	case 1:
		return int64(int8(field[0]))
	case 2:
		return int64(int16(binary.NativeEndian.Uint16(field)))
	case 4:
		return int64(int32(binary.NativeEndian.Uint32(field)))
	case 8:
		return int64(binary.NativeEndian.Uint64(field))
	default:
		return 0
	}
}

func hasUtmpField(record []byte, offset, size int) bool {
	return offset >= 0 && size > 0 && offset <= len(record)-size
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

func formatLoginTime(value time.Time) string {
	return normalizeLastTime(value.Local().Format("Mon Jan _2 15:04:05 2006"))
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
