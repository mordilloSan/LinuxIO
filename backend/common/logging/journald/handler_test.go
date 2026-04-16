package journald

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

type mockSender struct {
	fields []Field
}

func (m *mockSender) Send(fields []Field) error {
	m.fields = append([]Field(nil), fields...)
	return nil
}

func TestHandlerMapsLevelIdentifierAndAppFields(t *testing.T) {
	sender := &mockSender{}
	handler, err := NewHandler(Options{
		Identifier: "linuxio-bridge",
		Level:      slog.LevelDebug,
		Sender:     sender,
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	logger := slog.New(handler)
	logger.Warn("bridge start timeout", "session_id", "abc", "privileged", true)

	got := fieldMap(sender.fields)
	if got["SYSLOG_IDENTIFIER"] != "linuxio-bridge" {
		t.Fatalf("identifier = %q", got["SYSLOG_IDENTIFIER"])
	}
	if got["PRIORITY"] != "4" {
		t.Fatalf("priority = %q", got["PRIORITY"])
	}
	if got["MESSAGE"] != "bridge start timeout" {
		t.Fatalf("message = %q", got["MESSAGE"])
	}
	if got["LINUXIO_SESSION_ID"] != "abc" {
		t.Fatalf("session field = %q", got["LINUXIO_SESSION_ID"])
	}
	if got["LINUXIO_PRIVILEGED"] != "true" {
		t.Fatalf("privileged field = %q", got["LINUXIO_PRIVILEGED"])
	}
}

func TestHandlerAddsSourceFields(t *testing.T) {
	sender := &mockSender{}
	handler, err := NewHandler(Options{
		Identifier: "linuxio-webserver",
		Level:      slog.LevelDebug,
		AddSource:  true,
		Sender:     sender,
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	logWithSource(slog.New(handler))
	got := fieldMap(sender.fields)
	if !strings.HasSuffix(got["CODE_FILE"], "handler_test.go") {
		t.Fatalf("CODE_FILE = %q", got["CODE_FILE"])
	}
	if !strings.Contains(got["CODE_FUNC"], "logWithSource") {
		t.Fatalf("CODE_FUNC = %q", got["CODE_FUNC"])
	}
	if got["CODE_LINE"] == "" {
		t.Fatal("CODE_LINE missing")
	}
}

func TestHandlerFlattensGroupsAndEncodesComplexValues(t *testing.T) {
	sender := &mockSender{}
	handler, err := NewHandler(Options{
		Identifier: "linuxio-webserver",
		Level:      slog.LevelDebug,
		Sender:     sender,
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	logger := slog.New(handler).WithGroup("request")
	logger.Info("request finished",
		slog.Group("network", slog.String("interface", "eth0")),
		slog.Any("payload", map[string]any{"status": "ok", "count": 2}),
	)

	got := fieldMap(sender.fields)
	if got["LINUXIO_REQUEST_NETWORK_INTERFACE"] != "eth0" {
		t.Fatalf("group field = %q", got["LINUXIO_REQUEST_NETWORK_INTERFACE"])
	}
	if got["LINUXIO_REQUEST_PAYLOAD"] != `{"count":2,"status":"ok"}` && got["LINUXIO_REQUEST_PAYLOAD"] != `{"status":"ok","count":2}` {
		t.Fatalf("payload field = %q", got["LINUXIO_REQUEST_PAYLOAD"])
	}
}

func TestHandlerAllowsStandardFieldPassthroughAndLastWriteWins(t *testing.T) {
	sender := &mockSender{}
	handler, err := NewHandler(Options{
		Identifier: "linuxio-auth",
		Level:      slog.LevelDebug,
		AddSource:  true,
		Sender:     sender,
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	record := slog.NewRecord(testTime, slog.LevelError, "bridge exec failed", 0)
	record.AddAttrs(
		slog.String("message_id", "linuxio.test"),
		slog.String("code_file", "/override/file.go"),
		slog.String("user", "first"),
		slog.String("user", "second"),
	)
	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	got := fieldMap(sender.fields)
	if got["MESSAGE_ID"] != "linuxio.test" {
		t.Fatalf("MESSAGE_ID = %q", got["MESSAGE_ID"])
	}
	if got["CODE_FILE"] != "/override/file.go" {
		t.Fatalf("CODE_FILE = %q", got["CODE_FILE"])
	}
	if got["LINUXIO_USER"] != "second" {
		t.Fatalf("LINUXIO_USER = %q", got["LINUXIO_USER"])
	}
}

var testTime = time.Unix(1_700_000_000, 0)

func logWithSource(logger *slog.Logger) {
	logger.Info("with source")
}

func fieldMap(fields []Field) map[string]string {
	out := make(map[string]string, len(fields))
	for _, field := range fields {
		out[field.Name] = field.Value
	}
	return out
}
