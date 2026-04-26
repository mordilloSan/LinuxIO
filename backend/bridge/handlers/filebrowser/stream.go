package filebrowser

import (
	"log/slog"
	"net"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/config"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// Filebrowser stream types that carry browser-owned bytes.
const (
	StreamTypeFBDownload = "fb-download" // Single file download
	StreamTypeFBUpload   = "fb-upload"   // Single file upload
	StreamTypeFBArchive  = "fb-archive"  // Multi-file archive download
)

// chunkSizeFromSess returns the configured file-transfer chunk size in bytes.
// Falls back to 1 MiB when the config is unavailable or unset (ChunkSizeMB == 0).
func chunkSizeFromSess(sess *session.Session) int {
	const defaultChunkSize = 1 * 1024 * 1024
	cfg, _, err := config.Load(sess.User.Username)
	if err != nil || cfg.AppSettings.ChunkSizeMB <= 0 {
		return defaultChunkSize
	}
	return cfg.AppSettings.ChunkSizeMB * 1024 * 1024
}

// HandleDownloadStream handles a download stream for a single file.
func HandleDownloadStream(sess *session.Session, stream net.Conn, args []string) error {
	return handleDownload(stream, args, chunkSizeFromSess(sess))
}

// HandleUploadStream handles an upload stream for a single file.
func HandleUploadStream(sess *session.Session, stream net.Conn, args []string) error {
	return handleUpload(stream, args)
}

// HandleArchiveStream handles an archive download stream for multiple files.
func HandleArchiveStream(sess *session.Session, stream net.Conn, args []string) error {
	return handleArchiveDownload(stream, args, chunkSizeFromSess(sess))
}

// RegisterStreamHandlers registers all filebrowser byte-stream handlers.
func RegisterStreamHandlers(handlers map[string]func(*session.Session, net.Conn, []string) error) {
	handlers[StreamTypeFBDownload] = HandleDownloadStream
	handlers[StreamTypeFBUpload] = HandleUploadStream
	handlers[StreamTypeFBArchive] = HandleArchiveStream
}

func logWriteErr(action string, err error) {
	if err != nil {
		slog.Debug("failed to write filebrowser stream frame", "action", action, "error", err)
	}
}
