// Package alerts is the root-owned alert store: one JSON file written under a
// file lock by root processes and read by privileged bridges.
package alerts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
	"github.com/mordilloSan/LinuxIO/backend/common/filelock"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

const (
	SeverityInfo    = "info"
	SeverityWarning = "warning"
	SeverityError   = "error"

	MaxMessageLen = 4000

	documentVersion  = 1
	maxAlerts        = 200
	maxTitleLen      = 200
	maxKeyLen        = 64
	maxLinkLen       = 200
	maxOccurrenceLen = 128
	lockWait         = 10 * time.Second
)

var (
	// Path is the alert store. Tests and callers may override it.
	Path = filepath.Join(version.DataDir, "alerts.json")

	ErrNotFound = errors.New("alert not found")

	now     = time.Now
	identRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
)

type Alert struct {
	ID              string         `json:"id"`
	Source          string         `json:"source"`
	Key             string         `json:"key"`
	Severity        string         `json:"severity"`
	Title           string         `json:"title"`
	Message         string         `json:"message,omitempty"`
	Link            string         `json:"link,omitempty"`
	OccurrenceID    string         `json:"occurrence_id,omitempty"`
	OccurrenceCount int            `json:"occurrence_count"`
	FirstOccurrence time.Time      `json:"first_occurrence"`
	LastOccurrence  time.Time      `json:"last_occurrence"`
	LastObservedAt  time.Time      `json:"last_observed_at"`
	DismissedAt     *time.Time     `json:"dismissed_at,omitempty"`
	Seen            map[string]int `json:"seen,omitempty"`
}

type Observation struct {
	Source       string
	Key          string
	Severity     string
	Title        string
	Message      string
	Link         string
	OccurrenceID string
}

type Document struct {
	Version int     `json:"version"`
	Alerts  []Alert `json:"alerts"`
}

func ID(source, key string) string { return source + "/" + key }

func validSeverity(s string) bool {
	return s == SeverityInfo || s == SeverityWarning || s == SeverityError
}

// normalize makes obs storable so no caller has to: the key is mapped onto
// the identifier alphabet and title and message are cut to their limits.
func (o Observation) normalize() Observation {
	o.Key = normalizeKey(o.Key)
	o.Title = truncate(o.Title, maxTitleLen)
	o.Message = truncate(o.Message, MaxMessageLen)
	return o
}

// normalizeKey lower-cases key, maps characters outside [a-z0-9._-] to '-',
// makes it start with [a-z0-9] and cuts it to 64 characters. An empty key
// stays empty and fails validation.
func normalizeKey(key string) string {
	if key == "" {
		return ""
	}
	key = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			return r
		}
		return '-'
	}, strings.ToLower(key))
	if c := key[0]; c == '.' || c == '_' || c == '-' {
		key = "x" + key
	}
	return key[:min(len(key), maxKeyLen)]
}

// truncate cuts s to at most n bytes on a rune boundary, ending in an ellipsis.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n - len("…")
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

func (o Observation) validate() error {
	switch {
	case !identRe.MatchString(o.Source):
		return fmt.Errorf("invalid alert source %q", o.Source)
	case !identRe.MatchString(o.Key):
		return fmt.Errorf("invalid alert key %q", o.Key)
	case !validSeverity(o.Severity):
		return fmt.Errorf("invalid alert severity %q", o.Severity)
	case strings.TrimSpace(o.Title) == "" || len(o.Title) > maxTitleLen:
		return errors.New("alert title must be 1-200 characters")
	case len(o.Message) > MaxMessageLen:
		return fmt.Errorf("alert message exceeds %d characters", MaxMessageLen)
	case len(o.OccurrenceID) > maxOccurrenceLen:
		return fmt.Errorf("alert occurrence id exceeds %d characters", maxOccurrenceLen)
	case o.Link != "" && (!strings.HasPrefix(o.Link, "/") || strings.HasPrefix(o.Link, "//") || len(o.Link) > maxLinkLen):
		return fmt.Errorf("alert link must be an in-app path: %q", o.Link)
	}
	return nil
}

// Load reads the store. A missing file is an empty document.
func Load() (Document, error) {
	data, err := os.ReadFile(Path)
	if errors.Is(err, fs.ErrNotExist) {
		return Document{Version: documentVersion, Alerts: []Alert{}}, nil
	}
	if err != nil {
		return Document{}, fmt.Errorf("read %s: %w", Path, err)
	}
	var doc Document
	if err := json.Unmarshal(data, &doc); err != nil {
		return Document{}, fmt.Errorf("parse %s: %w", Path, err)
	}
	if doc.Version != documentVersion {
		return Document{}, fmt.Errorf("parse %s: unsupported version %d", Path, doc.Version)
	}
	if doc.Alerts == nil {
		doc.Alerts = []Alert{}
	}
	return doc, nil
}

func update(ctx context.Context, fn func(doc *Document) error) error {
	return filelock.RunExclusive(ctx, Path+".lock", func() error {
		doc, err := Load()
		if err != nil {
			return err
		}
		if err := fn(&doc); err != nil {
			return err
		}
		return write(doc)
	}, filelock.WithTimeout(lockWait))
}

func write(doc Document) error {
	sort.SliceStable(doc.Alerts, func(i, j int) bool {
		return doc.Alerts[i].LastOccurrence.After(doc.Alerts[j].LastOccurrence)
	})
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal alerts: %w", err)
	}
	if err := atomicfile.Write(Path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", Path, err)
	}
	return nil
}

// Raise records an observation. The same occurrence id only refreshes
// last_observed_at; a new one is a material change, which is also delivered
// to the configured notification channels after the store is written. The key,
// title and message are normalised here (see Observation.normalize).
func Raise(ctx context.Context, obs Observation) error {
	obs = obs.normalize()
	if err := obs.validate(); err != nil {
		return err
	}
	var changed *Alert
	err := update(ctx, func(doc *Document) error {
		t := now()
		id := ID(obs.Source, obs.Key)
		for i := range doc.Alerts {
			a := &doc.Alerts[i]
			if a.ID != id {
				continue
			}
			if obs.OccurrenceID != "" && a.OccurrenceID == obs.OccurrenceID {
				a.LastObservedAt = t
				return nil
			}
			a.Severity, a.Title, a.Message, a.Link = obs.Severity, obs.Title, obs.Message, obs.Link
			a.OccurrenceID = obs.OccurrenceID
			a.OccurrenceCount++
			a.LastOccurrence, a.LastObservedAt = t, t
			a.DismissedAt = nil
			changed = new(*a)
			return nil
		}
		if len(doc.Alerts) >= maxAlerts {
			i := evictIndex(doc.Alerts)
			doc.Alerts = slices.Delete(doc.Alerts, i, i+1)
		}
		doc.Alerts = append(doc.Alerts, Alert{
			ID: id, Source: obs.Source, Key: obs.Key,
			Severity: obs.Severity, Title: obs.Title, Message: obs.Message, Link: obs.Link,
			OccurrenceID: obs.OccurrenceID, OccurrenceCount: 1,
			FirstOccurrence: t, LastOccurrence: t, LastObservedAt: t,
		})
		changed = new(doc.Alerts[len(doc.Alerts)-1])
		return nil
	})
	if err == nil && changed != nil {
		notify(ctx, *changed)
	}
	return err
}

// evictIndex picks the oldest dismissed alert, else the oldest alert. The
// slice is sorted newest-first, so both are found from the end.
func evictIndex(alerts []Alert) int {
	for i := len(alerts) - 1; i >= 0; i-- {
		if alerts[i].DismissedAt != nil {
			return i
		}
	}
	return len(alerts) - 1
}

// Resolve removes the record, finding it by the key as Raise normalises it. Resolving an absent alert is not an error.
func Resolve(ctx context.Context, source, key string) error {
	id := ID(source, normalizeKey(key))
	return update(ctx, func(doc *Document) error {
		kept := doc.Alerts[:0]
		for _, a := range doc.Alerts {
			if a.ID != id {
				kept = append(kept, a)
			}
		}
		doc.Alerts = kept
		return nil
	})
}

func Dismiss(ctx context.Context, id string) error {
	return update(ctx, func(doc *Document) error {
		for i := range doc.Alerts {
			if doc.Alerts[i].ID == id {
				t := now()
				doc.Alerts[i].DismissedAt = &t
				return nil
			}
		}
		return fmt.Errorf("dismiss %s: %w", id, ErrNotFound)
	})
}

// MarkSeen records that uid has seen the current occurrence of each id, or of
// every alert when ids is nil.
func MarkSeen(ctx context.Context, uid string, ids []string) error {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	return update(ctx, func(doc *Document) error {
		for i := range doc.Alerts {
			a := &doc.Alerts[i]
			if ids != nil && !want[a.ID] {
				continue
			}
			if a.Seen == nil {
				a.Seen = map[string]int{}
			}
			a.Seen[uid] = a.OccurrenceCount
		}
		return nil
	})
}
