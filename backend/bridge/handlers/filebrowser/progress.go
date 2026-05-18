package filebrowser

import "time"

const (
	// progressReportIntervalBytes is how often file operations report progress.
	progressReportIntervalBytes = 2 * 1024 * 1024
	// uploadProgressAckIntervalBytes is more frequent because upload progress acts as a client-side flow-control ACK.
	uploadProgressAckIntervalBytes = 512 * 1024
	// transferProgressMaxInterval keeps visible transfer feedback cadence-based instead of purely byte-based.
	transferProgressMaxInterval = 250 * time.Millisecond
	// transferProgressMaxBytes caps silence on very fast transfers without forcing fixed tiny byte steps.
	transferProgressMaxBytes = 32 * 1024 * 1024
)

type transferProgressGate struct {
	maxInterval time.Duration
	maxBytes    int64
	lastBytes   int64
	lastAt      time.Time
}

func newTransferProgressGate(maxBytes int64) *transferProgressGate {
	if maxBytes <= 0 {
		maxBytes = transferProgressMaxBytes
	}
	return &transferProgressGate{
		maxInterval: transferProgressMaxInterval,
		maxBytes:    maxBytes,
	}
}

func (g *transferProgressGate) ShouldReport(bytes, total int64) bool {
	return g.ShouldReportAt(bytes, total, time.Now())
}

func (g *transferProgressGate) ShouldReportAt(bytes, total int64, now time.Time) bool {
	if bytes <= g.lastBytes {
		return false
	}
	if total > 0 && bytes >= total {
		g.record(bytes, now)
		return true
	}
	if g.lastAt.IsZero() {
		g.record(bytes, now)
		return true
	}
	if bytes-g.lastBytes >= g.maxBytes || now.Sub(g.lastAt) >= g.maxInterval {
		g.record(bytes, now)
		return true
	}
	return false
}

func (g *transferProgressGate) record(bytes int64, at time.Time) {
	g.lastBytes = bytes
	g.lastAt = at
}

// FileProgress represents progress for file transfer and file job operations.
type FileProgress struct {
	Bytes int64  `json:"bytes"`           // Bytes transferred so far
	Total int64  `json:"total"`           // Total bytes (0 if unknown)
	Pct   int    `json:"pct"`             // Percentage (0-100)
	Phase string `json:"phase,omitempty"` // Optional phase description
}

// DeleteProgress represents item-count progress for delete jobs.
type DeleteProgress struct {
	Processed     int64  `json:"processed"`
	Total         int64  `json:"total"`
	Pct           int    `json:"pct"`
	Phase         string `json:"phase,omitempty"`
	Indeterminate bool   `json:"indeterminate,omitempty"`
}
