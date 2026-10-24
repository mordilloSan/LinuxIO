package logs

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"strconv"
	"strings"
	"time"
)

func relativeJournalPeriod(value string) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}

	if before, ok := strings.CutSuffix(value, "d"); ok {
		days, err := strconv.ParseUint(before, 10, 16)
		if err != nil || days == 0 {
			return 0, false
		}
		return time.Duration(days) * 24 * time.Hour, true
	}

	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, false
	}
	return duration, true
}

func journalPeriodCutoff(value string, now time.Time) (int64, bool) {
	duration, ok := relativeJournalPeriod(value)
	if !ok {
		return 0, false
	}
	return now.Add(-duration).UnixMicro(), true
}

func journalLineOlderThan(line string, cutoffMicros int64) bool {
	var envelope struct {
		RealtimeTimestamp jsontext.Value `json:"__REALTIME_TIMESTAMP"`
	}
	if err := json.Unmarshal([]byte(line), &envelope, json.MatchCaseInsensitiveNames(true)); err != nil ||
		len(envelope.RealtimeTimestamp) == 0 {
		return false
	}

	rawTimestamp := strings.Trim(string(envelope.RealtimeTimestamp), `"`)
	timestamp, err := strconv.ParseInt(rawTimestamp, 10, 64)
	return err == nil && timestamp < cutoffMicros
}
