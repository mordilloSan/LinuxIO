package filebrowser

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

func TestStreamFileDownload(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "image.iso")
	contents := []byte("native download contents")
	if err := os.WriteFile(filePath, contents, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	server, client := net.Pipe()
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
	})
	done := make(chan error, 1)
	go func() {
		done <- streamFileDownload(context.Background(), server, apischema.PathRequest{Path: filePath})
	}()

	progress := readFileDownloadFrame(t, client, relay.OpStreamProgress)
	var detail downloadStreamProgress
	if err := json.Unmarshal(progress.Payload, &detail); err != nil {
		t.Fatalf("decode progress: %v", err)
	}
	if detail.Total != int64(len(contents)) {
		t.Fatalf("total = %d, want %d", detail.Total, len(contents))
	}
	if detail.FileName != "image.iso" {
		t.Fatalf("filename = %q, want image.iso", detail.FileName)
	}

	var body bytes.Buffer
	for {
		frame, err := relay.ReadRelayFrame(client)
		if err != nil {
			t.Fatalf("read stream frame: %v", err)
		}
		switch frame.Opcode {
		case relay.OpStreamData:
			body.Write(frame.Payload)
		case relay.OpStreamResult:
			readFileDownloadFrame(t, client, relay.OpStreamClose)
			if err := <-done; err != nil {
				t.Fatalf("streamFileDownload: %v", err)
			}
			if !bytes.Equal(body.Bytes(), contents) {
				t.Fatalf("body = %q, want %q", body.Bytes(), contents)
			}
			return
		default:
			t.Fatalf("opcode = 0x%02x, want data or result", frame.Opcode)
		}
	}
}

func TestStreamFileDownloadRejectsDirectory(t *testing.T) {
	directory := t.TempDir()
	server, client := net.Pipe()
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
	})
	done := make(chan error, 1)
	go func() {
		done <- streamFileDownload(context.Background(), server, apischema.PathRequest{Path: directory})
	}()

	frame := readFileDownloadFrame(t, client, relay.OpStreamResult)
	var result relay.ResultFrame
	if err := json.Unmarshal(frame.Payload, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Status != "error" || result.Code != 400 {
		t.Fatalf("result = %#v, want status error code 400", result)
	}
	readFileDownloadFrame(t, client, relay.OpStreamClose)
	if err := <-done; err != nil {
		t.Fatalf("streamFileDownload: %v", err)
	}
}

func readFileDownloadFrame(t *testing.T, stream net.Conn, wantOpcode byte) *relay.StreamFrame {
	t.Helper()
	frame, err := relay.ReadRelayFrame(stream)
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	if frame.Opcode != wantOpcode {
		t.Fatalf("opcode = 0x%02x, want 0x%02x", frame.Opcode, wantOpcode)
	}
	return frame
}
