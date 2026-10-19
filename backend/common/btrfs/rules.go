package btrfs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/filelock"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

// RulesPath is the root-owned automatic-snapshot rule file. Tests may override it.
var RulesPath = "/etc/linuxio/btrfs-snapshots.json"

const rulesLockWait = 10 * time.Second

// Rule cadences.
const (
	EveryHourly = "hourly"
	EveryDaily  = "daily"
	EveryWeekly = "weekly"
)

// dueTolerance lets a timer that fires slightly early still take its snapshot.
const dueTolerance = 5 * time.Minute

// BtrfsKeep is how many distinct hour/day/week/month buckets of timeline
// snapshots a rule retains.
type BtrfsKeep struct {
	Hourly  int `json:"hourly"`
	Daily   int `json:"daily"`
	Weekly  int `json:"weekly"`
	Monthly int `json:"monthly"`
}

// BtrfsSnapshotRule is the automatic-snapshot policy of one subvolume,
// identified by (UUID, Path).
type BtrfsSnapshotRule struct {
	UUID    string `json:"uuid"`
	Path    string `json:"path"`
	Enabled bool   `json:"enabled"`
	Every   string `json:"every"`
	// Hour (0-23, local time) is when daily and weekly rules run; Weekday
	// (0 = Sunday) picks the day of weekly ones. Hourly rules ignore both.
	Hour          int       `json:"hour"`
	Weekday       int       `json:"weekday"`
	Keep          BtrfsKeep `json:"keep"`
	BeforeUpdates bool      `json:"beforeUpdates"`
}

// BtrfsSnapshotRules is the API view of the rules plus the timer state.
type BtrfsSnapshotRules struct {
	Rules        []BtrfsSnapshotRule `json:"rules"`
	TimerEnabled bool                `json:"timerEnabled"`
}

type rulesDocument struct {
	Rules []BtrfsSnapshotRule `json:"rules"`
}

// LoadRules reads the rule file. A missing file is no rules.
func LoadRules() ([]BtrfsSnapshotRule, error) {
	data, err := os.ReadFile(RulesPath)
	if errors.Is(err, fs.ErrNotExist) {
		return []BtrfsSnapshotRule{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", RulesPath, err)
	}
	var doc rulesDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", RulesPath, err)
	}
	if doc.Rules == nil {
		doc.Rules = []BtrfsSnapshotRule{}
	}
	return doc.Rules, nil
}

// UpdateRules loads the rules, applies fn and writes the result, all under
// the rule file lock, and returns the rules written.
func UpdateRules(ctx context.Context, fn func(rules *[]BtrfsSnapshotRule) error) ([]BtrfsSnapshotRule, error) {
	return updateListFile(ctx, RulesPath, LoadRules, func(r []BtrfsSnapshotRule) any { return rulesDocument{Rules: r} }, fn)
}

// updateListFile loads a list with load, applies fn and writes doc(list) to
// path, all under path's lock, and returns the list written.
func updateListFile[T any](ctx context.Context, path string, load func() ([]T, error), doc func([]T) any, fn func(*[]T) error) ([]T, error) {
	var items []T
	err := filelock.RunExclusive(ctx, path+".lock", func() error {
		var err error
		if items, err = load(); err != nil {
			return err
		}
		if err = fn(&items); err != nil {
			return err
		}
		if items == nil {
			items = []T{}
		}
		data, err := json.MarshalIndent(doc(items), "", "  ")
		if err != nil {
			return fmt.Errorf("marshal %s: %w", path, err)
		}
		if err := utils.WriteFileAtomic(path, append(data, '\n'), 0o600); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		return nil
	}, filelock.WithTimeout(rulesLockWait))
	if err != nil {
		return nil, err
	}
	return items, nil
}

// ValidateRule checks a rule is complete and its path is a normal subvolume.
func ValidateRule(r BtrfsSnapshotRule) error {
	if r.UUID == "" {
		return errors.New("rule filesystem uuid is required")
	}
	if err := ValidateSubvolumePath(r.Path); err != nil {
		return err
	}
	if !slices.Contains([]string{EveryHourly, EveryDaily, EveryWeekly}, r.Every) {
		return fmt.Errorf("invalid rule cadence %q", r.Every)
	}
	if r.Hour < 0 || r.Hour > 23 {
		return fmt.Errorf("rule hour %d must be between 0 and 23", r.Hour)
	}
	if r.Weekday < 0 || r.Weekday > 6 {
		return fmt.Errorf("rule weekday %d must be between 0 and 6", r.Weekday)
	}
	if k := r.Keep; k.Hourly < 0 || k.Daily < 0 || k.Weekly < 0 || k.Monthly < 0 {
		return errors.New("keep counts must not be negative")
	}
	if k := r.Keep; r.Enabled && k.Hourly == 0 && k.Daily == 0 && k.Weekly == 0 && k.Monthly == 0 {
		return errors.New("an enabled rule must keep at least one snapshot")
	}
	return nil
}

// UpsertRule replaces the rule with the same (UUID, Path) or appends it.
func UpsertRule(rules []BtrfsSnapshotRule, r BtrfsSnapshotRule) []BtrfsSnapshotRule {
	for i := range rules {
		if rules[i].UUID == r.UUID && rules[i].Path == r.Path {
			rules[i] = r
			return rules
		}
	}
	return append(rules, r)
}

// DeleteRule removes the rule with the given (uuid, path), if any.
func DeleteRule(rules []BtrfsSnapshotRule, uuid, path string) []BtrfsSnapshotRule {
	return slices.DeleteFunc(rules, func(r BtrfsSnapshotRule) bool {
		return r.UUID == uuid && r.Path == path
	})
}

// Due reports whether a rule should take a snapshot now, given the newest
// timeline snapshot of its source (nil when there is none). Hourly rules are
// due an hour after the last snapshot; daily and weekly ones once their
// scheduled instant has passed without a snapshot since.
func Due(rule BtrfsSnapshotRule, newestTimeline *time.Time, now time.Time) bool {
	if newestTimeline == nil {
		return true
	}
	if rule.Every == EveryHourly {
		return now.Sub(*newestTimeline) >= time.Hour-dueTolerance
	}
	return newestTimeline.Before(lastScheduled(rule, now.Add(dueTolerance)))
}

// lastScheduled returns the rule's most recent scheduled instant at or before
// now, in now's location.
func lastScheduled(rule BtrfsSnapshotRule, now time.Time) time.Time {
	at := time.Date(now.Year(), now.Month(), now.Day(), rule.Hour, 0, 0, 0, now.Location())
	if at.After(now) {
		at = at.AddDate(0, 0, -1)
	}
	if rule.Every == EveryWeekly {
		back := (int(at.Weekday()) - rule.Weekday + 7) % 7
		at = at.AddDate(0, 0, -back)
	}
	return at
}
