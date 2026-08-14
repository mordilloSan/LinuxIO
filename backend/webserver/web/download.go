package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/webserver/bridge"
)

const (
	downloadOpenTimeout   = 10 * time.Second
	downloadRefreshPeriod = 30 * time.Second
	downloadDefaultName   = "download"
	downloadMaxNameLength = 255
)

type nativeArchiveDownloadRequest struct {
	TaskID string `json:"taskId"`
}

type nativeFileDownloadRequest struct {
	Path string `json:"path"`
}

// nativeDownloadHandler serves either a direct file path or a prepared archive
// task through one browser-facing endpoint. Exactly one source is required.
func nativeDownloadHandler(sm *session.Manager) http.Handler {
	return sm.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := session.SessionFromContext(r.Context())
		if sess == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		query := r.URL.Query()
		filePath, taskID := query.Get("path"), query.Get("taskId")
		if (filePath == "") == (taskID == "") {
			http.Error(w, "provide exactly one of path or taskId", http.StatusBadRequest)
			return
		}
		// Treat starting a native download as explicit activity. This also
		// gives a transfer that begins near idle expiry a fresh idle window
		// before archive preparation can block its first data frame.
		if err := sm.Refresh(sess.SessionID); err != nil {
			http.Error(w, "session expired", http.StatusUnauthorized)
			return
		}
		route := "filebrowser.download_stream"
		var request any = nativeFileDownloadRequest{Path: filePath}
		if taskID != "" {
			route = "tasks.data"
			request = nativeArchiveDownloadRequest{TaskID: taskID}
		}
		serveNativeDownload(w, r, sm, sess, route, request)
	}))
}

func serveNativeDownload(
	w http.ResponseWriter,
	r *http.Request,
	sm *session.Manager,
	sess *session.Session,
	route string,
	request any,
) {
	yamuxSession, err := bridge.GetYamuxSession(sess.SessionID)
	if err != nil {
		http.Error(w, "bridge unavailable", http.StatusServiceUnavailable)
		return
	}

	openCtx, cancel := context.WithTimeout(r.Context(), downloadOpenTimeout)
	stream, err := yamuxSession.Open(openCtx)
	cancel()
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			http.Error(w, "download stream unavailable", http.StatusGatewayTimeout)
		} else {
			http.Error(w, "download stream unavailable", http.StatusServiceUnavailable)
		}
		return
	}
	defer stream.Close()

	// One owned monitor keeps the authenticated session alive and closes the
	// yamux stream when the request is canceled, unblocking either Read or
	// Write. Join it before returning so it cannot outlive the handler.
	monitorCtx, stopMonitor := context.WithCancel(r.Context())
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		monitorNativeDownload(monitorCtx, sm, sess.SessionID, stream)
	}()
	defer func() {
		stopMonitor()
		<-monitorDone
	}()

	if err := openNativeDownloadStream(stream, route, request); err != nil {
		http.Error(w, "failed to open download", http.StatusBadGateway)
		return
	}

	if err := streamNativeDownload(w, r, stream); err != nil {
		slog.Debug("native download stream ended", "route", route, "session_id", sess.SessionID, "error", err)
	}
}

func openNativeDownloadStream(w io.Writer, route string, request any) error {
	requestPayload, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("marshal download request: %w", err)
	}
	openPayload, err := json.Marshal(relay.StreamOpenEnvelope{Route: route, Request: requestPayload})
	if err != nil {
		return fmt.Errorf("marshal task data envelope: %w", err)
	}
	if err := relay.WriteRelayFrame(w, &relay.StreamFrame{Opcode: relay.OpStreamOpen, Payload: openPayload}); err != nil {
		return fmt.Errorf("write task data open frame: %w", err)
	}
	return nil
}

func monitorNativeDownload(ctx context.Context, sm *session.Manager, sessionID string, stream net.Conn) {
	ticker := time.NewTicker(downloadRefreshPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := sm.Refresh(sessionID); err != nil {
				_ = stream.Close()
				return
			}
		case <-ctx.Done():
			_ = stream.Close()
			return
		}
	}
}

type downloadProgress struct {
	Total    *int64 `json:"total"`
	FileName string `json:"fileName"`
}

func streamNativeDownload(w http.ResponseWriter, r *http.Request, stream net.Conn) error {
	defer stream.Close()
	state := nativeDownloadStreamState{
		ctx:    r.Context(),
		writer: w,
	}

	for {
		frame, err := relay.ReadRelayFrame(stream)
		if err != nil {
			return state.readError(err)
		}
		done, err := state.consume(frame)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
}

type nativeDownloadStreamState struct {
	ctx              context.Context
	writer           http.ResponseWriter
	name             string
	total            int64
	written          int64
	headersCommitted bool
}

func (s *nativeDownloadStreamState) readError(err error) error {
	if s.ctx.Err() != nil {
		return s.ctx.Err()
	}
	if !s.headersCommitted {
		return downloadHTTPError(s.writer, http.StatusBadGateway, "download stream failed")
	}
	return fmt.Errorf("read download frame: %w", err)
}

func (s *nativeDownloadStreamState) consume(frame *relay.StreamFrame) (bool, error) {
	switch frame.Opcode {
	case relay.OpStreamProgress:
		return false, s.consumeProgress(frame.Payload)
	case relay.OpStreamData:
		return false, s.consumeData(frame.Payload)
	case relay.OpStreamResult:
		return s.consumeResult(frame.Payload)
	case relay.OpStreamClose:
		return false, s.closeBeforeResult()
	default:
		return false, s.unexpectedFrame(frame.Opcode)
	}
}

func (s *nativeDownloadStreamState) consumeProgress(payload []byte) error {
	total, name, err := decodeDownloadProgress(payload)
	if err != nil {
		if !s.headersCommitted {
			return downloadHTTPError(s.writer, http.StatusBadGateway, "invalid download progress")
		}
		return fmt.Errorf("invalid download progress: %w", err)
	}
	if !s.headersCommitted {
		s.total = total
		s.name = name
		s.commitHeaders()
		return nil
	}
	if total != s.total {
		return fmt.Errorf("download total changed from %d to %d", s.total, total)
	}
	if name != s.name {
		return fmt.Errorf("download filename changed from %q to %q", s.name, name)
	}
	return nil
}

func decodeDownloadProgress(payload []byte) (int64, string, error) {
	var progress downloadProgress
	if err := json.Unmarshal(payload, &progress); err != nil {
		return 0, "", err
	}
	if progress.Total == nil {
		return 0, "", errors.New("missing total")
	}
	if *progress.Total < 0 {
		return 0, "", errors.New("negative total")
	}
	if strings.TrimSpace(progress.FileName) == "" {
		return 0, "", errors.New("missing filename")
	}
	return *progress.Total, sanitizeDownloadName(progress.FileName), nil
}

func (s *nativeDownloadStreamState) consumeData(payload []byte) error {
	if !s.headersCommitted {
		return downloadHTTPError(s.writer, http.StatusBadGateway, "download data arrived before size")
	}
	if int64(len(payload)) > s.total-s.written {
		return errors.New("download data exceeds declared size")
	}
	if len(payload) == 0 {
		return nil
	}

	n, err := s.writer.Write(payload)
	s.written += int64(n)
	if err != nil {
		return fmt.Errorf("write download response: %w", err)
	}
	if n != len(payload) {
		return io.ErrShortWrite
	}
	return nil
}

func (s *nativeDownloadStreamState) consumeResult(payload []byte) (bool, error) {
	var result relay.ResultFrame
	if err := json.Unmarshal(payload, &result); err != nil {
		return false, s.invalidResult(err)
	}
	if result.Status != "ok" {
		return false, s.taskError(result)
	}
	if !s.headersCommitted {
		return false, downloadHTTPError(s.writer, http.StatusBadGateway, "download result arrived before size")
	}
	if s.written != s.total {
		return false, fmt.Errorf("download ended at %d bytes, declared %d", s.written, s.total)
	}
	return true, nil
}

func (s *nativeDownloadStreamState) invalidResult(err error) error {
	if !s.headersCommitted {
		return downloadHTTPError(s.writer, http.StatusBadGateway, "invalid download result")
	}
	return fmt.Errorf("invalid download result: %w", err)
}

func (s *nativeDownloadStreamState) taskError(result relay.ResultFrame) error {
	message := result.Error
	if message == "" {
		message = "download failed"
	}
	if !s.headersCommitted {
		return downloadHTTPError(s.writer, downloadResultStatus(result.Code), message)
	}
	return errors.New(message)
}

func (s *nativeDownloadStreamState) closeBeforeResult() error {
	if !s.headersCommitted {
		return downloadHTTPError(s.writer, http.StatusBadGateway, "download stream closed before result")
	}
	return errors.New("download stream closed before result")
}

func (s *nativeDownloadStreamState) unexpectedFrame(opcode byte) error {
	if !s.headersCommitted {
		return downloadHTTPError(s.writer, http.StatusBadGateway, "unexpected download frame")
	}
	return fmt.Errorf("unexpected download frame opcode 0x%02x", opcode)
}

func (s *nativeDownloadStreamState) commitHeaders() {
	if s.name == "" {
		s.name = downloadDefaultName
	}
	s.writer.Header().Set("Content-Disposition", formatDownloadDisposition(s.name))
	s.writer.Header().Set("Content-Type", downloadContentType(s.name))
	s.writer.Header().Set("Content-Length", strconv.FormatInt(s.total, 10))
	s.writer.Header().Set("Cache-Control", "no-store")
	s.writer.Header().Set("X-Content-Type-Options", "nosniff")
	s.writer.WriteHeader(http.StatusOK)
	s.headersCommitted = true
}

func downloadHTTPError(w http.ResponseWriter, status int, message string) error {
	http.Error(w, message, status)
	return errors.New(message)
}

func downloadResultStatus(code int) int {
	if code >= 400 && code <= 599 {
		return code
	}
	return http.StatusBadGateway
}

func sanitizeDownloadName(name string) string {
	name = strings.TrimSpace(strings.ToValidUTF8(name, "_"))
	if name == "" {
		return downloadDefaultName
	}
	// Bridge-provided names are metadata only. Strip path-like components and
	// controls so they cannot become response-header injection or filesystem
	// paths.
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if r == '/' || unicode.IsControl(r) {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return downloadDefaultName
	}
	if len(name) > downloadMaxNameLength {
		name = name[:downloadMaxNameLength]
		for !utf8.ValidString(name) {
			name = name[:len(name)-1]
		}
	}
	return name
}

func formatDownloadDisposition(name string) string {
	return mime.FormatMediaType("attachment", map[string]string{"filename": name})
}

func downloadContentType(name string) string {
	if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
		return contentType
	}
	return "application/octet-stream"
}
