package filebrowser

const (
	// progressReportIntervalBytes is how often file operations report progress.
	progressReportIntervalBytes = 2 * 1024 * 1024
	// uploadProgressAckIntervalBytes is more frequent because upload progress acts as a client-side flow-control ACK.
	uploadProgressAckIntervalBytes = 512 * 1024
)

// FileProgress represents progress for file transfer and file job operations.
type FileProgress struct {
	Bytes int64  `json:"bytes"`           // Bytes transferred so far
	Total int64  `json:"total"`           // Total bytes (0 if unknown)
	Pct   int    `json:"pct"`             // Percentage (0-100)
	Phase string `json:"phase,omitempty"` // Optional phase description
}
