package btrfs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
)

// AlertSource is the alerts source of every btrfs health alert.
const AlertSource = "btrfs"

// alertKinds are the per-filesystem alert key prefixes CheckHealth owns.
var alertKinds = []string{"health", "scrub", "space", "offline"}

func poolName(label, uuid string) string {
	if label != "" {
		return label
	}
	return uuid
}

func raise(ctx context.Context, obs alerts.Observation) {
	obs.Source = AlertSource
	if err := alerts.Raise(ctx, obs); err != nil {
		slog.Warn("failed to raise btrfs alert", "key", obs.Key, "error", err)
	}
}

func resolve(ctx context.Context, key string) {
	if err := alerts.Resolve(ctx, AlertSource, key); err != nil {
		slog.Warn("failed to resolve btrfs alert", "key", key, "error", err)
	}
}

// RaiseAlerts raises or resolves the device, scrub and space alerts of a
// mounted filesystem and clears its offline alert. Unknown health neither
// raises nor resolves, so a failed read does not flap an alert.
func RaiseAlerts(ctx context.Context, fs BtrfsFilesystem) {
	name := poolName(fs.Label, fs.UUID)
	link := "/storage?pool=" + fs.UUID
	if fs.HealthKnown {
		if occurrence := errorOccurrence(fs); occurrence == "" {
			resolve(ctx, "health-"+fs.UUID)
		} else {
			raise(ctx, alerts.Observation{
				Key: "health-" + fs.UUID, Severity: alerts.SeverityError,
				Title:   "Btrfs filesystem " + name + " needs attention",
				Message: "Device errors or a missing device were reported: " + occurrence,
				Link:    link, OccurrenceID: occurrence,
			})
		}
	}
	switch {
	case fs.Scrub.Status == "none" || fs.Scrub.Status == "running":
	case fs.Scrub.UncorrectableErrors > 0:
		occurrence := strconv.FormatUint(fs.Scrub.UncorrectableErrors, 10)
		if fs.Scrub.FinishedAt != nil {
			occurrence += "@" + strconv.FormatInt(fs.Scrub.FinishedAt.Unix(), 10)
		}
		raise(ctx, alerts.Observation{
			Key: "scrub-" + fs.UUID, Severity: alerts.SeverityError,
			Title:   fmt.Sprintf("Scrub of %s found %d uncorrectable errors", name, fs.Scrub.UncorrectableErrors),
			Message: "Some data could not be repaired from another copy. Check the disks and restore the affected files from a backup or snapshot.",
			Link:    link, OccurrenceID: occurrence,
		})
	default:
		resolve(ctx, "scrub-"+fs.UUID)
	}
	if fs.LowMetadataSpace {
		raise(ctx, alerts.Observation{
			Key: "space-" + fs.UUID, Severity: alerts.SeverityWarning,
			Title:   name + " is nearly out of metadata space",
			Message: "Metadata is over 90% full and less than 1 GiB is unallocated, so writes will start failing. Balance data chunks to reclaim space.",
			Link:    link,
		})
	} else {
		resolve(ctx, "space-"+fs.UUID)
	}
	resolve(ctx, "offline-"+fs.UUID)
}

// RaiseOfflineAlert flags an unmounted pool: an error when disks are
// missing, a warning when it is merely unmounted.
func RaiseOfflineAlert(ctx context.Context, pool BtrfsOfflinePool) {
	name := poolName(pool.Label, pool.UUID)
	missing := pool.NumDevices - len(pool.Devices)
	obs := alerts.Observation{
		Key: "offline-" + pool.UUID, Severity: alerts.SeverityWarning,
		Title:   "Pool " + name + " is not mounted",
		Message: "The pool is complete but not mounted. Mount it from the Storage overview.",
		Link:    "/storage", OccurrenceID: strconv.Itoa(missing),
	}
	if missing > 0 {
		obs.Severity = alerts.SeverityError
		obs.Title = fmt.Sprintf("Pool %s is missing %d of %d disks", name, missing, pool.NumDevices)
		obs.Message = "The pool is not mounted because disks are missing. Reconnect them, or mount it degraded and replace the missing disk."
	}
	raise(ctx, obs)
}

// CheckHealth reads every mounted filesystem and unmounted pool, updates
// their alerts and drops the alerts of pools that no longer exist. The
// health timer runs it so problems surface without anyone logged in.
func CheckHealth(ctx context.Context) error {
	filesystems, err := ListFilesystems(ctx)
	if err != nil {
		return err
	}
	pools, err := FindOfflinePools(ctx, BlockDevices())
	if err != nil {
		return err
	}
	live := map[string]bool{}
	for _, fs := range filesystems {
		RaiseAlerts(ctx, fs)
		live[fs.UUID] = true
	}
	for _, pool := range pools {
		RaiseOfflineAlert(ctx, pool)
		live[pool.UUID] = true
	}
	resolveStale(ctx, live)
	return nil
}

// resolveStale resolves the alerts of filesystems that are neither mounted
// nor found on any disk, such as destroyed pools.
func resolveStale(ctx context.Context, live map[string]bool) {
	doc, err := alerts.Load()
	if err != nil {
		slog.Warn("failed to read alerts", "error", err)
		return
	}
	for _, a := range doc.Alerts {
		kind, uuid, ok := strings.Cut(a.Key, "-")
		if a.Source != AlertSource || !ok || live[uuid] {
			continue
		}
		for _, k := range alertKinds {
			if kind == k {
				resolve(ctx, a.Key)
			}
		}
	}
}

// maxOccurrenceLen mirrors the alerts package's occurrence ID limit.
const maxOccurrenceLen = 128

// errorOccurrence describes the non-zero error counters per devid (for
// example "2:w5,g1;missing"), hashed when too long for an alert occurrence
// ID. It is empty for a healthy filesystem.
func errorOccurrence(fs BtrfsFilesystem) string {
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
