package scheduledrun

import (
	"context"
	"encoding/json/v2"
	"errors"
	"log/slog"
	"os"

	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
)

// Reporter writes the progress of the scheduled run its process belongs to.
type Reporter struct {
	store *jobs.Store
	id    string
}

// progress is the JSON the job list reads. It carries the percentage and
// message keys of the bridge's TaskProgress; scheduledrun cannot import the
// bridge, so it writes the two keys itself.
type progress struct {
	Percentage *int   `json:"percentage,omitzero"`
	Message    string `json:"message,omitempty"`
}

// ReporterFromEnv is the reporter of the scheduled run this process belongs
// to, found through $INVOCATION_ID; nil when there is none. A record that
// exists but cannot be read is logged at Debug.
func ReporterFromEnv(store *jobs.Store) *Reporter {
	id, err := jobs.UnitRunID(os.Getenv("INVOCATION_ID"))
	if err != nil {
		return nil
	}
	if _, err := store.Get(context.Background(), id); err != nil {
		if !errors.Is(err, jobs.ErrNotFound) {
			slog.Debug("scheduled run record unreadable", "run", id, "error", err)
		}
		return nil
	}
	return &Reporter{store: store, id: id}
}

// Report writes the run's progress. A nil reporter does nothing. A negative
// percentage means unknown. A failed write is logged and the run goes on.
func (r *Reporter) Report(percentage int, message string) {
	if r == nil {
		return
	}
	p := progress{Message: message}
	if percentage >= 0 {
		p.Percentage = &percentage
	}
	data, err := json.Marshal(p)
	if err == nil {
		err = r.store.WriteProgress(r.id, data)
	}
	if err != nil {
		slog.Debug("failed to write scheduled run progress", "run", r.id, "error", err)
	}
}
