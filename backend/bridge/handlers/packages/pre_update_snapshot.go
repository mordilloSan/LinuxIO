package packages

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

var (
	loadSnapshotRules = btrfs.LoadRules
	runSnapshots      = btrfs.Run
)

// preUpdateSnapshot takes a btrfs pre-update snapshot for every rule that
// opted in, enabled or not. Callers log the returned error when they carry on: a snapshot
// failure must never block the update.
func preUpdateSnapshot(ctx context.Context) (btrfs.RunReport, error) {
	rules, err := loadSnapshotRules()
	if err != nil {
		return btrfs.RunReport{}, fmt.Errorf("load btrfs snapshot rules: %w", err)
	}
	optedIn := false
	for _, rule := range rules {
		if rule.BeforeUpdates {
			optedIn = true
			break
		}
	}
	if !optedIn {
		return btrfs.RunReport{}, nil
	}
	report, err := runSnapshots(ctx, rules, time.Now(), btrfs.KindPreUpdate)
	if err != nil {
		return report, fmt.Errorf("btrfs pre-update snapshot: %w", err)
	}
	return report, nil
}

// preUpdateSnapshotProblems joins the hook's failures into one line, or "".
func preUpdateSnapshotProblems(report btrfs.RunReport, err error) string {
	problems := report.Errors
	if err != nil {
		problems = append(slices.Clip(problems), err.Error())
	}
	return strings.Join(problems, "; ")
}
