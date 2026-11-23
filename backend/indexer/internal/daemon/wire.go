package daemon

// scanEvent is what the scanner reports to the daemon over its socket
// (POST /scan/event): the daemon passes progress on to the clients that
// follow the index, as api.WorkProgressEvent and api.WorkCompleteEvent.
type scanEvent struct {
	Type    string `json:"type"`
	Message string `json:"message,omitempty"` // step and error

	// progress and summary counters
	Dirs  uint64 `json:"dirs,omitzero"`
	Files uint64 `json:"files,omitzero"`
	Size  uint64 `json:"size,omitzero"`

	// summary only
	DeletedEntries int64  `json:"deleted_entries,omitzero"`
	SkippedDirs    uint64 `json:"skipped_dirs,omitzero"`
}

const (
	scanEventStep     = "step"
	scanEventProgress = "scan"
	scanEventSummary  = "summary"
	scanEventError    = "error"
)
