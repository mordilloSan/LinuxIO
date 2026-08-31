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
		Identifier:     "linuxio-bridge",
		Level:          slog.LevelDebug,
		FieldPrefix:    "LINUXIO",
		SuppressFields: []string{"SESSION_ID"},
		Sender:         sender,
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
	if _, ok := got["LINUXIO_SESSION_ID"]; ok {
		t.Fatalf("session field unexpectedly present: %q", got["LINUXIO_SESSION_ID"])
	}
	if got["LINUXIO_PRIVILEGED"] != "true" {
		t.Fatalf("privileged field = %q", got["LINUXIO_PRIVILEGED"])
	}
}

func TestHandlerDefaultsToUnprefixedAppFields(t *testing.T) {
	sender := &mockSender{}
	handler, err := NewHandler(Options{
		Identifier: "generic-service",
		Level:      slog.LevelDebug,
		Sender:     sender,
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	slog.New(handler).Info("request complete", "user", "miguelmariz")

	got := fieldMap(sender.fields)
	if got["USER"] != "miguelmariz" {
		t.Fatalf("USER = %q", got["USER"])
	}
	if _, ok := got["LINUXIO_USER"]; ok {
		t.Fatalf("LINUXIO_USER unexpectedly present: %q", got["LINUXIO_USER"])
	}
}

func TestHandlerSuppressesFieldsIndependentOfPrefix(t *testing.T) {
	tests := []struct {
		name          string
		fieldPrefix   string
		suppressedKey string
		visibleKey    string
	}{
		{
			name:          "unprefixed",
			suppressedKey: "SESSION_ID",
			visibleKey:    "USER",
		},
		{
			name:          "prefixed",
			fieldPrefix:   "LINUXIO",
			suppressedKey: "LINUXIO_SESSION_ID",
			visibleKey:    "LINUXIO_USER",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sender := &mockSender{}
			handler, err := NewHandler(Options{
				Identifier:     "linuxio-bridge",
				Level:          slog.LevelDebug,
				FieldPrefix:    tt.fieldPrefix,
				SuppressFields: []string{"SESSION_ID"},
				Sender:         sender,
			})
			if err != nil {
				t.Fatalf("NewHandler: %v", err)
			}

			slog.New(handler).Info("bridge start timeout", "session_id", "abc", "user", "miguelmariz")

			got := fieldMap(sender.fields)
			if _, ok := got[tt.suppressedKey]; ok {
				t.Fatalf("%s unexpectedly present: %q", tt.suppressedKey, got[tt.suppressedKey])
			}
			if got[tt.visibleKey] != "miguelmariz" {
				t.Fatalf("%s = %q", tt.visibleKey, got[tt.visibleKey])
			}
		})
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
		Identifier:  "linuxio-webserver",
		Level:       slog.LevelDebug,
		FieldPrefix: "LINUXIO",
		Sender:      sender,
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
		Identifier:  "linuxio-auth",
		Level:       slog.LevelDebug,
		AddSource:   true,
		FieldPrefix: "LINUXIO",
		Sender:      sender,
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

func TestHandlerBridgeBootFieldNamesAreUnprefixedAtCallSite(t *testing.T) {
	sender := &mockSender{}
	handler, err := NewHandler(Options{
		Identifier:  "linuxio-bridge",
		Level:       slog.LevelDebug,
		FieldPrefix: "LINUXIO",
		Sender:      sender,
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	slog.New(handler).Info("bridge boot",
		"effective_uid", 0,
		"uid", 1000,
		"gid", 1000,
	)

	got := fieldMap(sender.fields)
	if got["LINUXIO_EFFECTIVE_UID"] != "0" {
		t.Fatalf("LINUXIO_EFFECTIVE_UID = %q", got["LINUXIO_EFFECTIVE_UID"])
	}
	if got["LINUXIO_UID"] != "1000" {
		t.Fatalf("LINUXIO_UID = %q", got["LINUXIO_UID"])
	}
	if got["LINUXIO_GID"] != "1000" {
		t.Fatalf("LINUXIO_GID = %q", got["LINUXIO_GID"])
	}
	if _, ok := got["LINUXIO_LINUXIO_UID"]; ok {
		t.Fatalf("LINUXIO_LINUXIO_UID unexpectedly present: %q", got["LINUXIO_LINUXIO_UID"])
	}
	if _, ok := got["LINUXIO_LINUXIO_GID"]; ok {
		t.Fatalf("LINUXIO_LINUXIO_GID unexpectedly present: %q", got["LINUXIO_LINUXIO_GID"])
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
