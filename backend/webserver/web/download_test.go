package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

func TestOpenNativeDownloadStreamRequestsArchiveTaskData(t *testing.T) {
	var wire bytes.Buffer
	if err := openNativeDownloadStream(&wire, "tasks.data", nativeArchiveDownloadRequest{TaskID: "task-42"}); err != nil {
		t.Fatalf("openNativeDownloadStream: %v", err)
	}
	frame, err := relay.ReadRelayFrame(&wire)
	if err != nil {
		t.Fatalf("ReadRelayFrame: %v", err)
	}
	if frame.Opcode != relay.OpStreamOpen {
		t.Fatalf("opcode = 0x%02x, want OpStreamOpen", frame.Opcode)
	}
	envelope, err := relay.ParseStreamOpenPayload(frame.Payload)
	if err != nil {
		t.Fatalf("ParseStreamOpenPayload: %v", err)
	}
	if envelope.Route != "tasks.data" {
		t.Fatalf("route = %q, want tasks.data", envelope.Route)
	}
	var request nativeArchiveDownloadRequest
	if err := json.Unmarshal(envelope.Request, &request); err != nil {
		t.Fatalf("json.Unmarshal(request): %v", err)
	}
	if request.TaskID != "task-42" {
		t.Fatalf("task id = %q, want task-42", request.TaskID)
	}
}

func TestOpenNativeDownloadStreamRequestsDirectFile(t *testing.T) {
	var wire bytes.Buffer
	if err := openNativeDownloadStream(&wire, "filebrowser.download_stream", nativeFileDownloadRequest{Path: "/tmp/file.iso"}); err != nil {
		t.Fatalf("openNativeDownloadStream: %v", err)
	}
	frame, err := relay.ReadRelayFrame(&wire)
	if err != nil {
		t.Fatalf("ReadRelayFrame: %v", err)
	}
	envelope, err := relay.ParseStreamOpenPayload(frame.Payload)
	if err != nil {
		t.Fatalf("ParseStreamOpenPayload: %v", err)
	}
	if envelope.Route != "filebrowser.download_stream" {
		t.Fatalf("route = %q, want filebrowser.download_stream", envelope.Route)
	}
	var request nativeFileDownloadRequest
	if err := json.Unmarshal(envelope.Request, &request); err != nil {
		t.Fatalf("json.Unmarshal(request): %v", err)
	}
	if request.Path != "/tmp/file.iso" {
		t.Fatalf("path = %q, want /tmp/file.iso", request.Path)
	}
}

func TestStreamNativeDownloadStreamsFramesAndCommitsHeadersAfterProgress(t *testing.T) {
	server, client := net.Pipe()
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
	})

	writeErr := make(chan error, 1)
	go func() {
		progress := int64(5)
		if err := relay.WriteProgress(client, 0, struct {
			Total    int64  `json:"total"`
			FileName string `json:"fileName"`
		}{Total: progress, FileName: "report.txt"}); err != nil {
			writeErr <- err
			return
		}
		if err := relay.WriteRelayFrame(client, &relay.StreamFrame{Opcode: relay.OpStreamData, Payload: []byte("hello")}); err != nil {
			writeErr <- err
			return
		}
		writeErr <- relay.WriteRelayFrame(client, &relay.StreamFrame{
			Opcode:  relay.OpStreamResult,
			Payload: []byte(`{"status":"ok"}`),
		})
	}()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/download?taskId=task-1", nil)
	response := httptest.NewRecorder()
	if err := streamNativeDownload(response, req, server); err != nil {
		t.Fatalf("streamNativeDownload: %v", err)
	}
	if err := <-writeErr; err != nil {
		t.Fatalf("write frames: %v", err)
	}

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Body.String(); got != "hello" {
		t.Fatalf("body = %q, want hello", got)
	}
	if got := response.Header().Get("Content-Length"); got != "5" {
		t.Fatalf("content length = %q, want 5", got)
	}
	if got := response.Header().Get("Content-Disposition"); !strings.Contains(got, "report.txt") {
		t.Fatalf("content disposition = %q, want report.txt", got)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("cache control = %q, want no-store", got)
	}
	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("nosniff = %q, want nosniff", got)
	}
}

func TestSanitizeDownloadNameRejectsPathAndControlCharacters(t *testing.T) {
	if got := sanitizeDownloadName("../../secret\r\n.txt"); got != "secret__.txt" {
		t.Fatalf("sanitized name = %q, want secret__.txt", got)
	}
	if got := sanitizeDownloadName(""); got != downloadDefaultName {
		t.Fatalf("empty name = %q, want %q", got, downloadDefaultName)
	}
	got := sanitizeDownloadName(strings.Repeat("é", downloadMaxNameLength))
	if len(got) > downloadMaxNameLength || !utf8.ValidString(got) {
		t.Fatalf("long unicode name is not valid bounded UTF-8: len=%d valid=%t", len(got), utf8.ValidString(got))
	}
}

func TestStreamNativeDownloadServesEmptyFile(t *testing.T) {
	server, client := net.Pipe()
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
	})

	writeErr := make(chan error, 1)
	go func() {
		if err := relay.WriteProgress(client, 0, struct {
			Total    int64  `json:"total"`
			FileName string `json:"fileName"`
		}{Total: 0, FileName: "empty.bin"}); err != nil {
			writeErr <- err
			return
		}
		writeErr <- relay.WriteRelayFrame(client, &relay.StreamFrame{
			Opcode:  relay.OpStreamResult,
			Payload: []byte(`{"status":"ok"}`),
		})
	}()

	req := httptest.NewRequest(http.MethodGet, "/api/download?taskId=task-empty", nil)
	response := httptest.NewRecorder()
	if err := streamNativeDownload(response, req, server); err != nil {
		t.Fatalf("streamNativeDownload: %v", err)
	}
	if err := <-writeErr; err != nil {
		t.Fatalf("write frames: %v", err)
	}
	if response.Code != http.StatusOK || response.Body.Len() != 0 {
		t.Fatalf("empty response = status %d, body %q", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Length"); got != "0" {
		t.Fatalf("content length = %q, want 0", got)
	}
}

func TestStreamNativeDownloadRejectsDataBeforeProgress(t *testing.T) {
	server, client := net.Pipe()
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
	})

	writeErr := make(chan error, 1)
	go func() {
		writeErr <- relay.WriteRelayFrame(client, &relay.StreamFrame{
			Opcode:  relay.OpStreamData,
			Payload: []byte("unexpected"),
		})
	}()

	req := httptest.NewRequest(http.MethodGet, "/api/download?taskId=task-1", nil)
	response := httptest.NewRecorder()
	if err := streamNativeDownload(response, req, server); err == nil {
		t.Fatal("streamNativeDownload error = nil, want protocol error")
	}
	if err := <-writeErr; err != nil {
		t.Fatalf("write frame: %v", err)
	}
	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadGateway)
	}
}

func TestStreamNativeDownloadMapsTaskErrorBeforeHeaders(t *testing.T) {
	server, client := net.Pipe()
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
	})

	writeErr := make(chan error, 1)
	go func() {
		writeErr <- relay.WriteRelayFrame(client, &relay.StreamFrame{
			Opcode:  relay.OpStreamResult,
			Payload: []byte(`{"status":"error","error":"task not found","code":404}`),
		})
	}()

	req := httptest.NewRequest(http.MethodGet, "/api/download/missing", nil)
	response := httptest.NewRecorder()
	if err := streamNativeDownload(response, req, server); err == nil {
		t.Fatal("streamNativeDownload error = nil, want task error")
	}
	if err := <-writeErr; err != nil {
		t.Fatalf("write frame: %v", err)
	}
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestStreamNativeDownloadDetectsTruncatedBody(t *testing.T) {
	server, client := net.Pipe()
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
	})

	writeErr := make(chan error, 1)
	go func() {
		if err := relay.WriteProgress(client, 0, struct {
			Total    int64  `json:"total"`
			FileName string `json:"fileName"`
		}{Total: 5, FileName: "file.bin"}); err != nil {
			writeErr <- err
			return
		}
		if err := relay.WriteRelayFrame(client, &relay.StreamFrame{Opcode: relay.OpStreamData, Payload: []byte("hey")}); err != nil {
			writeErr <- err
			return
		}
		writeErr <- relay.WriteRelayFrame(client, &relay.StreamFrame{
			Opcode:  relay.OpStreamResult,
			Payload: []byte(`{"status":"ok"}`),
		})
	}()

	req := httptest.NewRequest(http.MethodGet, "/api/download?taskId=task-1", nil)
	response := httptest.NewRecorder()
	if err := streamNativeDownload(response, req, server); err == nil {
		t.Fatal("streamNativeDownload error = nil, want truncated-body error")
	}
	if err := <-writeErr; err != nil {
		t.Fatalf("write frames: %v", err)
	}
	if got := response.Body.String(); got != "hey" {
		t.Fatalf("body = %q, want hey", got)
	}
}

func TestNativeDownloadHandlerRequiresSession(t *testing.T) {
	store := session.NewWithCleanupInterval(0)
	cfg := session.DefaultConfig
	cfg.GCInterval = 0
	manager := session.NewManager(store, cfg)
	t.Cleanup(manager.Close)

	req := httptest.NewRequest(http.MethodGet, "/api/download?taskId=task-1", nil)
	response := httptest.NewRecorder()
	nativeDownloadHandler(manager).ServeHTTP(response, req)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestNativeDownloadHandlerRequiresExactlyOneSource(t *testing.T) {
	store := session.NewWithCleanupInterval(0)
	cfg := session.DefaultConfig
	cfg.GCInterval = 0
	manager := session.NewManager(store, cfg)
	t.Cleanup(manager.Close)
	sess, err := manager.CreateSession("download-source", session.User{Username: "test", UID: 1000, GID: 1000}, false)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	for _, target := range []string{
		"/api/download",
		"/api/download?path=%2Ftmp%2Ffile.iso&taskId=task-1",
	} {
		t.Run(target, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, target, nil)
			req.AddCookie(&http.Cookie{Name: manager.CookieName(), Value: sess.SessionID})
			response := httptest.NewRecorder()
			nativeDownloadHandler(manager).ServeHTTP(response, req)

			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestMonitorNativeDownloadClosesStreamOnCancellation(t *testing.T) {
	server, client := net.Pipe()
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		monitorNativeDownload(ctx, nil, "", server)
	}()

	cancel()
	<-done
	if _, err := client.Write([]byte("closed")); err == nil {
		t.Fatal("write after cancellation succeeded, want closed stream")
	}
}
