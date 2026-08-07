package cmd

import (
	"bytes"
	"encoding/binary"
	"testing"

	authipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/auth"
)

// encodeLenStr appends a 2-byte big-endian length prefix followed by s.
func encodeLenStr(buf *bytes.Buffer, s string) {
	var lenBytes [2]byte
	binary.BigEndian.PutUint16(lenBytes[:], uint16(len(s)))
	buf.Write(lenBytes[:])
	buf.WriteString(s)
}

// buildBootstrapFrame assembles a valid bootstrap frame per the protocol:
// [magic 'L','I','O',version][uid:4 BE][gid:4 BE][flags:1][len:2]session_id[len:2]username.
func buildBootstrapFrame(version byte, uid, gid uint32, flags byte, sessionID, username string) []byte {
	var buf bytes.Buffer
	buf.WriteByte(authipc.ProtoMagic0)
	buf.WriteByte(authipc.ProtoMagic1)
	buf.WriteByte(authipc.ProtoMagic2)
	buf.WriteByte(version)

	var u32 [4]byte
	binary.BigEndian.PutUint32(u32[:], uid)
	buf.Write(u32[:])
	binary.BigEndian.PutUint32(u32[:], gid)
	buf.Write(u32[:])

	buf.WriteByte(flags)

	encodeLenStr(&buf, sessionID)
	encodeLenStr(&buf, username)

	return buf.Bytes()
}

func TestReadBootstrapValidFrameAcceptedWithFieldsAndFlagsPropagated(t *testing.T) {
	flags := byte(authipc.ProtoFlagVerbose | authipc.ProtoFlagPrivileged | authipc.ProtoFlagReadyAck)
	frame := buildBootstrapFrame(authipc.ProtoVersion, 1001, 1002, flags, "sess-abc123", "alice")

	b, err := readBootstrap(bytes.NewReader(frame))
	if err != nil {
		t.Fatalf("readBootstrap: unexpected error: %v", err)
	}
	if b.UID != 1001 {
		t.Errorf("UID = %d, want 1001", b.UID)
	}
	if b.GID != 1002 {
		t.Errorf("GID = %d, want 1002", b.GID)
	}
	if b.SessionID != "sess-abc123" {
		t.Errorf("SessionID = %q, want %q", b.SessionID, "sess-abc123")
	}
	if b.Username != "alice" {
		t.Errorf("Username = %q, want %q", b.Username, "alice")
	}
	if !b.Verbose {
		t.Error("Verbose = false, want true")
	}
	if !b.Privileged {
		t.Error("Privileged = false, want true")
	}
	if !b.ReadyAck {
		t.Error("ReadyAck = false, want true")
	}
}

func TestReadBootstrapEmptySessionIDRejected(t *testing.T) {
	frame := buildBootstrapFrame(authipc.ProtoVersion, 1, 1, 0, "", "alice")

	b, err := readBootstrap(bytes.NewReader(frame))
	if err == nil {
		t.Fatal("readBootstrap: expected error for empty session_id, got nil")
	}
	if b != nil {
		t.Errorf("readBootstrap: expected nil bootstrap on error, got %+v", b)
	}
}

func TestReadBootstrapEmptyUsernameRejected(t *testing.T) {
	frame := buildBootstrapFrame(authipc.ProtoVersion, 1, 1, 0, "sess-abc123", "")

	b, err := readBootstrap(bytes.NewReader(frame))
	if err == nil {
		t.Fatal("readBootstrap: expected error for empty username, got nil")
	}
	if b != nil {
		t.Errorf("readBootstrap: expected nil bootstrap on error, got %+v", b)
	}
}

func TestReadBootstrapWrongVersionRejected(t *testing.T) {
	frame := buildBootstrapFrame(authipc.ProtoVersion+1, 1, 1, 0, "sess-abc123", "alice")

	b, err := readBootstrap(bytes.NewReader(frame))
	if err == nil {
		t.Fatal("readBootstrap: expected error for wrong version, got nil")
	}
	if b != nil {
		t.Errorf("readBootstrap: expected nil bootstrap on error, got %+v", b)
	}
}

func TestReadBootstrapTruncatedFrameRejected(t *testing.T) {
	frame := buildBootstrapFrame(authipc.ProtoVersion, 1, 1, 0, "sess-abc123", "alice")
	truncated := frame[:len(frame)-3]

	b, err := readBootstrap(bytes.NewReader(truncated))
	if err == nil {
		t.Fatal("readBootstrap: expected error for truncated frame, got nil")
	}
	if b != nil {
		t.Errorf("readBootstrap: expected nil bootstrap on error, got %+v", b)
	}
}
