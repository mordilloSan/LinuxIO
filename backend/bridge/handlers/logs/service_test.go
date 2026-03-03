package logs

import (
	"encoding/json"
	"net"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

func TestHandleServiceLogsStreamRejectsTemplateUnits(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()

	errCh := make(chan error, 1)
	go func() {
		defer server.Close()
		errCh <- HandleServiceLogsStream(nil, server, []string{"apport-coredump-hook@.service"})
	}()

	resultFrame, err := ipc.ReadRelayFrame(client)
	if err != nil {
		t.Fatalf("ReadRelayFrame(result): %v", err)
	}
	if resultFrame.Opcode != ipc.OpStreamResult {
		t.Fatalf("opcode = 0x%02x, want OpStreamResult", resultFrame.Opcode)
	}

	var result ipc.ResultFrame
	if unmarshalErr := json.Unmarshal(resultFrame.Payload, &result); unmarshalErr != nil {
		t.Fatalf("json.Unmarshal: %v", unmarshalErr)
	}
	if result.Status != "error" {
		t.Fatalf("status = %q, want error", result.Status)
	}
	if !strings.Contains(result.Error, "template unit") {
		t.Fatalf("error = %q, want template-unit message", result.Error)
	}

	closeFrame, err := ipc.ReadRelayFrame(client)
	if err != nil {
		t.Fatalf("ReadRelayFrame(close): %v", err)
	}
	if closeFrame.Opcode != ipc.OpStreamClose {
		t.Fatalf("opcode = 0x%02x, want OpStreamClose", closeFrame.Opcode)
	}

	if err := <-errCh; err == nil || !strings.Contains(err.Error(), "does not have logs") {
		t.Fatalf("handler err = %v, want template-unit error", err)
	}
}
