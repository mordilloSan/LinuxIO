package ipc

import (
	"bytes"
	"testing"
)

func TestReadAuthResponse_DecodesSuccessUser(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte{
		ProtoMagic0,
		ProtoMagic1,
		ProtoMagic2,
		ProtoVersion,
		StatusOK,
		ModePrivileged,
		byte(ResultOK),
		0,
	})
	buf.Write([]byte{0, 0, 3, 232}) // uid 1000
	buf.Write([]byte{0, 0, 3, 233}) // gid 1001
	if err := writeLenStr(&buf, "miguel"); err != nil {
		t.Fatalf("writeLenStr: %v", err)
	}

	resp, err := ReadAuthResponse(&buf)
	if err != nil {
		t.Fatalf("ReadAuthResponse: %v", err)
	}

	if !resp.IsOK() {
		t.Fatalf("status = %d, want %d", resp.Status, StatusOK)
	}
	if !resp.IsPrivileged() {
		t.Fatal("expected privileged mode")
	}
	if resp.User.Username != "miguel" {
		t.Fatalf("username = %q, want %q", resp.User.Username, "miguel")
	}
	if resp.User.UID != 1000 {
		t.Fatalf("uid = %d, want %d", resp.User.UID, 1000)
	}
	if resp.User.GID != 1001 {
		t.Fatalf("gid = %d, want %d", resp.User.GID, 1001)
	}
}

func TestReadAuthResponse_DecodesStructuredResultCode(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte{
		ProtoMagic0,
		ProtoMagic1,
		ProtoMagic2,
		ProtoVersion,
		StatusError,
		ModeUnprivileged,
		byte(ResultPasswordExpired),
		0,
	})
	if err := writeLenStr(&buf, "password expired"); err != nil {
		t.Fatalf("writeLenStr: %v", err)
	}

	resp, err := ReadAuthResponse(&buf)
	if err != nil {
		t.Fatalf("ReadAuthResponse: %v", err)
	}

	if resp.Status != StatusError {
		t.Fatalf("status = %d, want %d", resp.Status, StatusError)
	}
	if resp.ResultCode != ResultPasswordExpired {
		t.Fatalf("result code = %d, want %d", resp.ResultCode, ResultPasswordExpired)
	}
	if resp.Error != "password expired" {
		t.Fatalf("error = %q, want %q", resp.Error, "password expired")
	}
	if resp.User.Username != "" {
		t.Fatalf("username = %q, want empty", resp.User.Username)
	}
}

func TestAuthResultCodeHelpers(t *testing.T) {
	if !ResultAuthFailed.IsUnauthorized() {
		t.Fatal("ResultAuthFailed should be unauthorized")
	}
	if !ResultPasswordExpired.IsUnauthorized() {
		t.Fatal("ResultPasswordExpired should be unauthorized")
	}
	if ResultBridgeError.IsUnauthorized() {
		t.Fatal("ResultBridgeError should not be unauthorized")
	}
	if got := ResultBridgeError.DefaultMessage(); got != "failed to start bridge" {
		t.Fatalf("default message = %q, want %q", got, "failed to start bridge")
	}
	if got := ResultPasswordExpired.APIName(); got != "password_expired" {
		t.Fatalf("api name = %q, want %q", got, "password_expired")
	}
}
