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

type bootEvent struct {
	Kind      string
	StartedAt time.Time
}

var runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

var readFile = os.ReadFile

const (
	btmpPath         = "/var/log/btmp"
	wtmpPath         = "/var/log/wtmp"
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

	return fetchSuccessfulLogins(ctx, username, limit)
}

func fetchSuccessfulLogins(ctx context.Context, username string, limit int) ([]Login, error) {
	if logins, err := fetchWtmpdb(ctx, username, limit); err == nil {
		return logins, nil
	}

	if logins, err := fetchWtmp(ctx, username, limit); err == nil {
		return logins, nil
	}

	// Both sources were unavailable. A cancelled/expired context is the one
	// failure a caller must be able to tell apart from "no history"; anything
	// else (missing wtmpdb, unreadable wtmp) degrades gracefully to empty.
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	return nil, nil
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
	failures, since, until, err := fetchFailedAttemptEvents(ctx, username, username, sessionStartedAt, 0)
	if err != nil {
		return nil, err
	}
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

func FetchFailedAttemptBatchForAllUsers(ctx context.Context, boundaryUsername string, sessionStartedAt time.Time) (*FailedAttemptBatch, error) {
	failures, since, until, err := fetchFailedAttemptEvents(ctx, boundaryUsername, "", sessionStartedAt, 0)
	if err != nil {
		return nil, err
	}
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

func FetchFailedAttemptEventsForAllUsers(ctx context.Context, boundaryUsername string, sessionStartedAt time.Time, limit int) ([]Login, error) {
	failures, _, _, err := fetchFailedAttemptEvents(ctx, boundaryUsername, "", sessionStartedAt, limit)
	return failures, err
}

func FetchFailedAttempts(ctx context.Context, username string, sessionStartedAt time.Time) (int, error) {
	batch, err := FetchFailedAttemptBatch(ctx, username, sessionStartedAt)
	if err != nil || batch == nil {
		return 0, err
	}
	return batch.Count, nil
}

func FetchByUser(ctx context.Context) (map[string]Login, error) {
	logins, err := fetchSuccessfulLogins(ctx, "", 0)
	if err != nil {
		return nil, err
	}
	return firstLoginByUser(logins), nil
}

func fetchBootEvents(ctx context.Context, limit int) ([]bootEvent, error) {
	data, err := readFile(wtmpPath)
	if err != nil {
		return nil, err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	return parseWtmpBootEvents(data, limit), nil
}

func FetchPreviousUncleanBoot(ctx context.Context) (time.Time, bool, error) {
	status, err := fetchWtmpdbPreviousBootStatus(ctx)
	if err == nil && status.Found {
		return status.StartedAt, status.Unclean, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return time.Time{}, false, ctxErr
	}

	return fetchWtmpPreviousUncleanBoot(ctx)
}

func fetchFailedAttemptEvents(ctx context.Context, boundaryUsername, failedUsername string, sessionStartedAt time.Time, limit int) ([]Login, int64, int64, error) {
	boundaryUsername = strings.TrimSpace(boundaryUsername)
	if boundaryUsername == "" {
		return nil, 0, 0, nil
	}

	failedUsername = strings.TrimSpace(failedUsername)
	logins, err := FetchRecent(ctx, boundaryUsername, 50)
	if err != nil {
		return nil, 0, 0, err
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, 0, 0, ctxErr
	}

	data, err := readFile(btmpPath)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
		return nil, 0, 0, nil
	}
	if err != nil {
		return nil, 0, 0, err
	}

	since := previousSuccessfulLoginUnixBefore(logins, sessionStartedAt)
	until := int64(0)
	if !sessionStartedAt.IsZero() {
		until = sessionStartedAt.Unix() + 1
	}

	return parseBtmpFailuresBetween(failedUsername, data, since, until, limit), since, until, nil
}

type wtmpdbOutput struct {
	Entries []wtmpdbEntry `json:"entries"`
}

type wtmpdbEntry struct {
	User     string `json:"user"`
	TTY      string `json:"tty"`
	Hostname string `json:"hostname"`
	Login    string `json:"login"`
	Logout   string `json:"logout"`
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

type previousBootStatus struct {
	StartedAt time.Time
	Unclean   bool
	Found     bool // wtmpdb returned a definitive answer for the previous boot
}

func fetchWtmpdbPreviousBootStatus(ctx context.Context) (previousBootStatus, error) {
	output, err := runCommand(ctx, "wtmpdb", "last", "-j", "--time-format", "iso", "--system", "-n", "2", "shutdown", "reboot")
	if err != nil {
		return previousBootStatus{}, err
	}
	return parseWtmpdbPreviousBootStatus(output)
}

func parseWtmpdbPreviousBootStatus(output []byte) (previousBootStatus, error) {
	var decoded wtmpdbOutput
	if err := json.Unmarshal(output, &decoded); err != nil {
		return previousBootStatus{}, err
	}

	foundCurrent := false
	for _, entry := range decoded.Entries {
		kind := strings.TrimSpace(entry.User)
		if kind != "reboot" && kind != "shutdown" {
			continue
		}

		if !foundCurrent {
			// The most recent system event should be the current boot.
			if kind != "reboot" {
				return previousBootStatus{}, nil
			}
			foundCurrent = true
			continue
		}

		// First system event preceding the current boot decides the verdict.
		if kind == "shutdown" {
			// Orderly shutdown before the current boot — clean.
			return previousBootStatus{Found: true}, nil
		}

		// Previous boot with no shutdown between it and the current boot; its
		// logout field marks whether that session ended cleanly.
		startedAt := parseWtmpdbISOTime(entry.Login)
		if startedAt.IsZero() {
			return previousBootStatus{}, nil
		}
		return previousBootStatus{
			StartedAt: startedAt,
			Unclean:   isUnfinishedBootLogout(entry.Logout),
			Found:     true,
		}, nil
	}
	return previousBootStatus{}, nil
}

func parseWtmpdbISOTime(value string) time.Time {
	value = strings.TrimSpace(value)
	for _, layout := range []string{
		"2006-01-02T15:04:05-0700",
		time.RFC3339,
	} {
		t, err := time.Parse(layout, value)
		if err == nil {
			return t
		}
	}
	return time.Time{}
}

func isUnfinishedBootLogout(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.Contains(value, "crash") || strings.Contains(value, "still running")
}

func fetchWtmp(ctx context.Context, username string, limit int) ([]Login, error) {
	data, err := readFile(wtmpPath)
	if err != nil {
		return nil, err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	return parseWtmpLogins(username, data, limit), nil
}

func fetchWtmpPreviousUncleanBoot(ctx context.Context) (time.Time, bool, error) {
	events, err := fetchBootEvents(ctx, 2)
	if err != nil {
		return time.Time{}, false, err
	}
	startedAt, unclean := previousUncleanBootFromEvents(events)
	return startedAt, unclean, nil
}

func previousUncleanBootFromEvents(events []bootEvent) (time.Time, bool) {
	if len(events) < 2 || events[0].Kind != "reboot" || events[1].Kind != "reboot" {
		return time.Time{}, false
	}
	return events[1].StartedAt, true
}

func parseWtmpLogins(username string, data []byte, limit int) []Login {
	username = strings.TrimSpace(username)

	recordCount := len(data) / btmpRecordSize
	logins := make([]Login, 0)
	for i := recordCount - 1; i >= 0; i-- {
		record := data[i*btmpRecordSize : (i+1)*btmpRecordSize]
		if utmpRecordType(record) != utmpUserProcess {
			continue
		}

		recordUser := strings.TrimSpace(fixedCString(record[btmpUserOffset : btmpUserOffset+btmpUserSize]))
		if !matchesLoginUser(recordUser, username) {
			continue
		}

		recordTime := utmpRecordTime(record)
		if recordTime <= 0 {
			continue
		}

		startedAt := time.Unix(recordTime, 0)
		login := Login{
			Username:  recordUser,
			Terminal:  strings.TrimSpace(fixedCString(record[btmpLineOffset : btmpLineOffset+btmpLineSize])),
			Source:    strings.TrimSpace(fixedCString(record[btmpHostOffset : btmpHostOffset+btmpHostSize])),
			Time:      formatLoginTime(startedAt),
			StartedAt: startedAt,
			Status:    LoginStatusSuccess,
		}
		login.ID = StableLoginID(login)
		logins = append(logins, login)

		if limit > 0 && len(logins) >= limit {
			break
		}
	}
	return logins
}

func parseWtmpBootEvents(data []byte, limit int) []bootEvent {
	recordCount := len(data) / btmpRecordSize
	events := make([]bootEvent, 0)
	for i := recordCount - 1; i >= 0; i-- {
		record := data[i*btmpRecordSize : (i+1)*btmpRecordSize]
		kind := strings.TrimSpace(fixedCString(record[btmpUserOffset : btmpUserOffset+btmpUserSize]))
		if kind != "reboot" && kind != "shutdown" {
			continue
		}

		recordTime := utmpRecordTime(record)
		if recordTime <= 0 {
			continue
		}

		events = append(events, bootEvent{
			Kind:      kind,
			StartedAt: time.Unix(recordTime, 0),
		})
		if limit > 0 && len(events) >= limit {
			break
		}
	}
	return events
}

func parseBtmpFailures(username string, data []byte, limit int) []Login {
	return parseBtmpFailuresBetween(username, data, 0, 0, limit)
}

func parseBtmpFailuresBetween(username string, data []byte, since, until int64, limit int) []Login {
	username = strings.TrimSpace(username)

	recordCount := len(data) / btmpRecordSize
	failures := make([]Login, 0)
	for i := recordCount - 1; i >= 0; i-- {
		record := data[i*btmpRecordSize : (i+1)*btmpRecordSize]

		recordType := utmpRecordType(record)
		if recordType != utmpLoginProcess && recordType != utmpUserProcess {
			continue
		}

		recordUser := strings.TrimSpace(fixedCString(record[btmpUserOffset : btmpUserOffset+btmpUserSize]))
		if !matchesLoginUser(recordUser, username) {
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

	count := 0
	for len(data) >= btmpRecordSize {
		record := data[:btmpRecordSize]
		data = data[btmpRecordSize:]

		recordType := utmpRecordType(record)
		if recordType != utmpLoginProcess && recordType != utmpUserProcess {
			continue
		}
		recordUser := strings.TrimSpace(fixedCString(record[btmpUserOffset : btmpUserOffset+btmpUserSize]))
		if !matchesLoginUser(recordUser, username) {
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

func matchesLoginUser(recordUser, username string) bool {
	recordUser = strings.TrimSpace(recordUser)
	if !isLoginUser(recordUser) {
		return false
	}
	return username == "" || recordUser == username
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
