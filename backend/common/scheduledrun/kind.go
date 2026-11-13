// Package scheduledrun records the runs of work that systemd timers start:
// user scripts, btrfs snapshots and replication, pool resyncs, Docker updates
// and the distribution's automatic system updates. A hook on each unit calls
// Begin when a run starts and End when it stops.
package scheduledrun

import (
	"encoding/json/v2"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
)

const (
	RouteScript       = "scheduled.script"
	RouteSnapshot     = "scheduled.btrfs_snapshot"
	RouteReplicate    = "scheduled.btrfs_replicate"
	RouteResync       = "scheduled.btrfs_resync"
	RouteDockerUpdate = "scheduled.docker_update"
	RouteSystemUpdate = "scheduled.system_update"
	// AlertSource is the source of the alerts a failed run raises.
	AlertSource = "jobs"
)

// The unit names belong to the packages that render those units. They are
// repeated here so this package does not import them; a test in each owning
// package fails when one drifts.
const (
	scriptUnitPrefix   = "linuxio-schedule-"
	snapshotUnit       = "linuxio-btrfs-snapshot.service"
	replicateUnit      = "linuxio-btrfs-replicate.service"
	resyncUnitPrefix   = "linuxio-btrfs-resync@"
	dockerUpdateUnit   = "linuxio-docker-update.service"
	unitSuffix         = ".service"
	scriptFallback     = "Scheduled task"
	maxDefinitionBytes = 512 << 10
)

// systemUpdateUnits are the services the distributions run their automatic
// updates in (APT, DNF 4 and 5, Linux Mint). They are not LinuxIO's units:
// the installer hooks them with the run-record drop-in, and the package's
// auto-update backends, which own these names, test them against this list.
var systemUpdateUnits = []string{
	"apt-daily-upgrade.service",
	"dnf-automatic.service",
	"dnf5-automatic.service",
	"mintupdate-automation-upgrade.service",
}

// Kind describes the scheduled work a unit runs.
type Kind struct {
	Route    string
	Label    string
	Instance string // schedule ID or pool UUID; "" for a single unit
	Link     string // the page that owns it
	Alerts   bool   // a failed or interrupted run raises a jobs alert, a completed one a notice when asked
}

// KindOf names the scheduled work unit runs. scheduleDir holds the schedule
// definitions a script's label is read from; with "" a script gets the
// fallback label and no file is read.
func KindOf(unit, scheduleDir string) (Kind, bool) {
	switch unit {
	case snapshotUnit:
		return Kind{Route: RouteSnapshot, Label: "Snapshots", Link: "/storage/subvolumes?view=snapshots"}, true
	case replicateUnit:
		return Kind{Route: RouteReplicate, Label: "Replication", Link: "/storage/subvolumes?view=replication"}, true
	case dockerUpdateUnit:
		return Kind{Route: RouteDockerUpdate, Label: "Container updates", Link: "/docker/containers"}, true
	}
	if slices.Contains(systemUpdateUnits, unit) {
		return Kind{Route: RouteSystemUpdate, Label: "System updates", Link: "/updates"}, true
	}
	if id, ok := instanceOf(unit, scriptUnitPrefix); ok {
		return Kind{
			Route:    RouteScript,
			Label:    scriptLabel(scheduleDir, id),
			Instance: id,
			Link:     "/services/schedules?schedule=" + id,
			Alerts:   true,
		}, true
	}
	if uuid, ok := instanceOf(unit, resyncUnitPrefix); ok {
		return Kind{Route: RouteResync, Label: "Pool resync", Instance: uuid, Link: "/storage?pool=" + uuid}, true
	}
	return Kind{}, false
}

// instanceOf returns the UUID between prefix and ".service".
func instanceOf(unit, prefix string) (string, bool) {
	id, ok := strings.CutPrefix(unit, prefix)
	if !ok {
		return "", false
	}
	id, ok = strings.CutSuffix(id, unitSuffix)
	if !ok || jobs.ValidateID(id) != nil {
		return "", false
	}
	return id, true
}

// scriptLabel is the name of the schedule, or a fallback when its definition
// cannot be read. An empty scheduleDir reads no file: the caller has the
// label elsewhere. A missing definition is expected and silent; any other
// reason is logged at debug level.
func scriptLabel(scheduleDir, id string) string {
	if scheduleDir == "" {
		return scriptFallback
	}
	path := filepath.Join(scheduleDir, "definitions", id+".json")
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return scriptFallback
	}
	if err != nil {
		slog.Debug("schedule definition is unreadable", "schedule", id, "error", err)
		return scriptFallback
	}
	if !info.Mode().IsRegular() {
		slog.Debug("schedule definition is not a regular file", "schedule", id)
		return scriptFallback
	}
	if info.Size() > maxDefinitionBytes {
		slog.Debug("schedule definition is too large", "schedule", id, "size", info.Size())
		return scriptFallback
	}
	data, err := os.ReadFile(path)
	if err != nil {
		slog.Debug("schedule definition is unreadable", "schedule", id, "error", err)
		return scriptFallback
	}
	var doc struct {
		Definition struct {
			Options struct {
				Name string `json:"name"`
			} `json:"options"`
		} `json:"definition"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		slog.Debug("schedule definition is invalid", "schedule", id, "error", err)
		return scriptFallback
	}
	if name := strings.TrimSpace(doc.Definition.Options.Name); name != "" {
		return name
	}
	return scriptFallback
}

// worker reports whether the unit runs one of LinuxIO's workers (snapshots,
// replication, resync, Docker updates), whose failed run the end hook reports with a run
// alert unless the worker reported everything itself. A script has its own
// alerts, and a system update its provider's.
func (k Kind) worker() bool {
	return k.Route == RouteSnapshot || k.Route == RouteReplicate || k.Route == RouteResync || k.Route == RouteDockerUpdate
}
