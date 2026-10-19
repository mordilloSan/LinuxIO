package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strconv"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

const btrfsAlertSource = "btrfs"

// raiseBtrfsHealthAlerts raises an error alert for a filesystem with a missing
// device or non-zero device error counters, and resolves it otherwise.
// Nothing is raised or resolved when the health data is incomplete, so a
// failed command neither clears nor re-opens an alert. Alert failures are
// logged, never returned.
func raiseBtrfsHealthAlerts(ctx context.Context, fs btrfs.BtrfsFilesystem) {
	if !fs.HealthKnown {
		return
	}
	key := "health-" + fs.UUID
	occurrence := btrfsErrorOccurrence(fs)
	if occurrence == "" {
		if err := alerts.Resolve(ctx, btrfsAlertSource, key); err != nil {
			slog.Warn("failed to resolve btrfs alert", "uuid", fs.UUID, "error", err)
		}
		return
	}
	name := fs.Label
	if name == "" {
		name = fs.UUID
	}
	err := alerts.Raise(ctx, alerts.Observation{
		Source:       btrfsAlertSource,
		Key:          key,
		Severity:     alerts.SeverityError,
		Title:        "Btrfs filesystem " + name + " needs attention",
		Message:      "Device errors or a missing device were reported: " + occurrence,
		Link:         "/storage/btrfs?fs=" + fs.UUID,
		OccurrenceID: occurrence,
	})
	if err != nil {
		slog.Warn("failed to raise btrfs alert", "uuid", fs.UUID, "error", err)
	}
}

// maxOccurrenceLen mirrors the alerts package's occurrence ID limit.
const maxOccurrenceLen = 128

// btrfsErrorOccurrence describes the non-zero error counters per devid (for
// example "2:w5,g1;missing"), hashed when too long for an alert occurrence
// ID. It is empty for a healthy filesystem.
func btrfsErrorOccurrence(fs btrfs.BtrfsFilesystem) string {
	var parts []string
	for _, d := range fs.Devices {
		var counters []string
		for _, c := range []struct {
			prefix string
			n      uint64
		}{{"w", d.Errors.Write}, {"r", d.Errors.Read}, {"f", d.Errors.Flush}, {"c", d.Errors.Corruption}, {"g", d.Errors.Generation}} {
			if c.n > 0 {
				counters = append(counters, c.prefix+strconv.FormatUint(c.n, 10))
			}
		}
		if len(counters) > 0 {
			parts = append(parts, strconv.Itoa(d.ID)+":"+strings.Join(counters, ","))
		}
	}
	if fs.Degraded {
		parts = append(parts, "missing")
	}
	id := strings.Join(parts, ";")
	if len(id) > maxOccurrenceLen {
		sum := sha256.Sum256([]byte(id))
		return hex.EncodeToString(sum[:])
	}
	return id
}
