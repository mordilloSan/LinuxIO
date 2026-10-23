package logs

import (
	"encoding/json"
	"strings"
)

// keepJournalFields is the allowlist of journald fields forwarded to the UI
// list; LINUXIO_* user fields are always kept. Everything else is stripped
// server-side — the full entry is available on demand via logs.general_entry.
var keepJournalFields = map[string]struct{}{
	"__CURSOR":             {},
	"__REALTIME_TIMESTAMP": {},
	"_BOOT_ID":             {},
	"MESSAGE":              {},
	"PRIORITY":             {},
	"SYSLOG_IDENTIFIER":    {},
	"_COMM":                {},
	"_SYSTEMD_UNIT":        {},
	"UNIT":                 {},
	"CODE_FUNC":            {},
}

// trimJournalLine strips a journalctl -o json line down to the fields the UI
// consumes (keepJournalFields + LINUXIO_*), returning the reduced JSON and the
// entry's cursor. Lines that fail to parse pass through unchanged.
func trimJournalLine(line string) (trimmed string, cursor string) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &fields); err != nil {
		return line, ""
	}
	if raw, ok := fields["__CURSOR"]; ok {
		_ = json.Unmarshal(raw, &cursor)
	}
	kept := make(map[string]json.RawMessage, len(keepJournalFields)+4)
	for key, value := range fields {
		if _, ok := keepJournalFields[key]; ok || strings.HasPrefix(key, "LINUXIO_") {
			kept[key] = value
		}
	}
	out, err := json.Marshal(kept)
	if err != nil {
		return line, cursor
	}
	return string(out), cursor
}
