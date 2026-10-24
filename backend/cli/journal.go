package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/hostcmd"
)

func journalctlCommand(args []string, pendingGroup string) *exec.Cmd {
	if pendingGroup == "" {
		return exec.Command("journalctl", args...)
	}
	return exec.Command("sg", pendingGroup, "-c", journalctlShellCommand(args))
}

func journalctlShellCommand(args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, "journalctl")
	for _, arg := range args {
		parts = append(parts, hostcmd.ShellQuote(arg))
	}
	return strings.Join(parts, " ")
}

func journalTermsForMode(mode string) []string {
	journalTerms := []string{
		"SYSLOG_IDENTIFIER=linuxio-webserver",
		"SYSLOG_IDENTIFIER=linuxio-bridge",
		"SYSLOG_IDENTIFIER=linuxio-auth",
		"SYSLOG_IDENTIFIER=linuxio-indexer",
		"SYSLOG_IDENTIFIER=linuxio-monitoring",
		"_SYSTEMD_UNIT=linuxio.target",
		"_SYSTEMD_UNIT=linuxio-webserver.service",
		"_SYSTEMD_UNIT=linuxio-webserver.socket",
		"_SYSTEMD_UNIT=linuxio-bridge-socket-user.service",
		"_SYSTEMD_UNIT=linuxio-auth.socket",
		"_SYSTEMD_UNIT=linuxio-auth@.service",
		"_SYSTEMD_UNIT=linuxio-issue.service",
		"_SYSTEMD_UNIT=linuxio-indexer.service",
		"_SYSTEMD_UNIT=linuxio-indexer.socket",
		"_SYSTEMD_UNIT=linuxio-indexer-index.service",
		"_SYSTEMD_UNIT=linuxio-indexer-index.timer",
		"_SYSTEMD_UNIT=linuxio-monitoring.service",
	}

	switch mode {
	case "webserver":
		journalTerms = []string{
			"SYSLOG_IDENTIFIER=linuxio-webserver",
			"_SYSTEMD_UNIT=linuxio-webserver.service",
			"_SYSTEMD_UNIT=linuxio-webserver.socket",
		}
	case "bridge":
		journalTerms = []string{"SYSLOG_IDENTIFIER=linuxio-bridge"}
	case "auth":
		journalTerms = []string{
			"SYSLOG_IDENTIFIER=linuxio-auth",
			"_SYSTEMD_UNIT=linuxio-auth.socket",
			"_SYSTEMD_UNIT=linuxio-auth@.service",
		}
	case "indexer":
		journalTerms = []string{
			"SYSLOG_IDENTIFIER=linuxio-indexer",
			"_SYSTEMD_UNIT=linuxio-indexer.service",
			"_SYSTEMD_UNIT=linuxio-indexer.socket",
			"_SYSTEMD_UNIT=linuxio-indexer-index.service",
			"_SYSTEMD_UNIT=linuxio-indexer-index.timer",
		}
	case "monitoring":
		journalTerms = []string{
			"SYSLOG_IDENTIFIER=linuxio-monitoring",
			"_SYSTEMD_UNIT=linuxio-monitoring.service",
		}
	}
	return journalTerms
}

type journalEntry struct {
	Timestamp string `json:"__REALTIME_TIMESTAMP"`
	Unit      string `json:"_SYSTEMD_UNIT"`
	SyslogID  string `json:"SYSLOG_IDENTIFIER"`
	PID       string `json:"_PID"`
	SyslogPID string `json:"SYSLOG_PID"`
	Priority  string `json:"PRIORITY"`
	Message   string `json:"MESSAGE"`
	Fields    map[string]string
}

var visibleJournalFields = map[string]struct{}{
	"LINUXIO_ARG_COUNT":  {},
	"LINUXIO_DURATION":   {},
	"LINUXIO_ERROR":      {},
	"LINUXIO_GID":        {},
	"LINUXIO_MODE":       {},
	"LINUXIO_OUTCOME":    {},
	"LINUXIO_PRIVILEGED": {},
	"LINUXIO_ROUTE":      {},
	"LINUXIO_UID":        {},
	"LINUXIO_USER":       {},
	"LINUXIO_VERBOSE":    {},
}

// formatJournalEntry parses a journalctl JSON line and formats it with colors
// PRIORITY levels: 7=DEBUG(cyan), 6,5=INFO(green), 4=WARNING(yellow), 3,2,1,0=ERROR(red)
func formatJournalEntry(jsonLine string) string {
	entry, err := parseJournalEntry(jsonLine)
	if err != nil {
		return ""
	}

	timestamp := journalTimestamp(entry)
	source := journalSource(entry)
	level := journalPriorityLevel(entry)
	message := journalMessage(entry)

	return fmt.Sprintf("%s %s %s %s", timestamp, level, source, message)
}

func parseJournalEntry(jsonLine string) (journalEntry, error) {
	var entry journalEntry
	if err := json.Unmarshal([]byte(jsonLine), &entry); err != nil {
		return journalEntry{}, err
	}

	var rawFields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonLine), &rawFields); err != nil {
		return journalEntry{}, err
	}

	fields := make(map[string]string)
	for name, raw := range rawFields {
		if !strings.HasPrefix(name, "LINUXIO_") {
			continue
		}
		value := journalRawValue(raw)
		if value == "" {
			continue
		}
		fields[name] = value
	}
	entry.Fields = fields
	return entry, nil
}

func journalTimestamp(entry journalEntry) string {
	if usec, err := strconv.ParseInt(entry.Timestamp, 10, 64); err == nil {
		return time.Unix(0, usec*1000).Format("2006/01/02 15:04:05")
	}
	return time.Now().Format("2006/01/02 15:04:05")
}

func journalSource(entry journalEntry) string {
	unit := "unknown"
	if entry.SyslogID != "" {
		unit = entry.SyslogID
	} else if entry.Unit != "" {
		unit = entry.Unit
	}
	if at := strings.Index(unit, "@"); at >= 0 {
		unit = unit[:at]
	}
	unit = strings.TrimSuffix(unit, ".service")
	unit = strings.TrimSuffix(unit, ".socket")
	unit = strings.TrimPrefix(unit, "linuxio-")

	switch unit {
	case "webserver":
		return "web"
	case "":
		return "unknown"
	default:
		return unit
	}
}

func journalMessage(entry journalEntry) string {
	keys := make([]string, 0, len(entry.Fields))
	for name := range entry.Fields {
		if _, ok := visibleJournalFields[name]; !ok {
			continue
		}
		keys = append(keys, name)
	}
	if len(keys) == 0 {
		return entry.Message
	}

	sort.Strings(keys)

	var builder strings.Builder
	builder.WriteString(entry.Message)
	builder.WriteString(" [")
	for i, name := range keys {
		if i > 0 {
			builder.WriteByte(' ')
		}
		builder.WriteString(strings.ToLower(strings.TrimPrefix(name, "LINUXIO_")))
		builder.WriteByte('=')
		builder.WriteString(journalFieldValue(entry.Fields[name]))
	}
	builder.WriteByte(']')
	return builder.String()
}

func journalRawValue(raw json.RawMessage) string {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return journalAnyValue(value)
}

func journalAnyValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			part := journalAnyValue(item)
			if part == "" {
				continue
			}
			parts = append(parts, part)
		}
		return strings.Join(parts, ",")
	default:
		return fmt.Sprintf("%v", value)
	}
}

func journalFieldValue(value string) string {
	if strings.ContainsAny(value, " \t") {
		return strconv.Quote(value)
	}
	return value
}

func journalPriorityLevel(entry journalEntry) string {
	switch entry.Priority {
	case "7":
		return "\033[36m[DEBUG]\033[0m"
	case "6", "5":
		return "\033[32m[INFO]\033[0m"
	case "4":
		return "\033[33m[WARN]\033[0m"
	case "3", "2", "1", "0":
		return "\033[31m[ERROR]\033[0m"
	default:
		return "\033[32m[INFO]\033[0m"
	}
}
