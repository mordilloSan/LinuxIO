package auth

import (
	"bytes"
	"strings"
	"testing"
)

func TestReadBootstrap_DecodesHeaderAndStrings(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte{
		ProtoMagic0,
		ProtoMagic1,
		ProtoMagic2,
		ProtoVersion,
		0, 0, 3, 232, // uid 1000
		0, 0, 3, 233, // gid 1001
		ProtoFlagVerbose | ProtoFlagPrivileged,
	})
	if err := writeLenStr(&buf, "session-1"); err != nil {
		t.Fatalf("writeLenStr session: %v", err)
	}
	if err := writeLenStr(&buf, "miguel"); err != nil {
		t.Fatalf("writeLenStr username: %v", err)
	}

	bootstrap, err := ReadBootstrap(&buf)
	if err != nil {
		t.Fatalf("ReadBootstrap: %v", err)
	}
	if bootstrap.UID != 1000 || bootstrap.GID != 1001 {
		t.Fatalf("ids = %d/%d, want 1000/1001", bootstrap.UID, bootstrap.GID)
	}
	if !bootstrap.Verbose || !bootstrap.Privileged {
		t.Fatalf("flags = verbose:%v privileged:%v, want both true", bootstrap.Verbose, bootstrap.Privileged)
	}
	if bootstrap.SessionID != "session-1" || bootstrap.Username != "miguel" {
		t.Fatalf("session/user = %q/%q", bootstrap.SessionID, bootstrap.Username)
	}
	if bootstrap.ReadyAck {
		t.Fatal("ReadyAck = true without ProtoFlagReadyAck")
	}
}

func TestReadBootstrap_ReadyAckFlag(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte{
		ProtoMagic0,
		ProtoMagic1,
		ProtoMagic2,
		ProtoVersion,
		0, 0, 3, 232,
		0, 0, 3, 233,
		ProtoFlagReadyAck,
	})
	if err := writeLenStr(&buf, "session-1"); err != nil {
		t.Fatalf("writeLenStr session: %v", err)
	}
	if err := writeLenStr(&buf, "miguel"); err != nil {
		t.Fatalf("writeLenStr username: %v", err)
	}

	bootstrap, err := ReadBootstrap(&buf)
	if err != nil {
		t.Fatalf("ReadBootstrap: %v", err)
	}
	if !bootstrap.ReadyAck {
		t.Fatal("ReadyAck = false, want true")
	}
	if bootstrap.Verbose || bootstrap.Privileged {
		t.Fatalf("flags = verbose:%v privileged:%v, want both false", bootstrap.Verbose, bootstrap.Privileged)
	}
}

func TestReadBootstrap_RejectsMalformedFrames(t *testing.T) {
	validHeader := []byte{
		ProtoMagic0,
		ProtoMagic1,
		ProtoMagic2,
		ProtoVersion,
		0, 0, 3, 232,
		0, 0, 3, 233,
		0,
	}
	tests := []struct {
		name    string
		frame   []byte
		wantErr string
	}{
		{name: "short header", frame: validHeader[:12], wantErr: "read header"},
		{name: "bad magic", frame: append([]byte{'X'}, validHeader[1:]...), wantErr: "invalid bootstrap magic"},
		{name: "bad version", frame: append(append([]byte{}, validHeader[:3]...), append([]byte{ProtoVersion + 1}, validHeader[4:]...)...), wantErr: "unsupported bootstrap version"},
		{name: "missing session ID", frame: validHeader, wantErr: "read session_id"},
		{name: "truncated session ID", frame: append(append([]byte{}, validHeader...), 0, 2, 's'), wantErr: "read session_id"},
		{name: "missing username", frame: append(append([]byte{}, validHeader...), 0, 1, 's'), wantErr: "read username"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ReadBootstrap(bytes.NewReader(tt.frame))
			if err == nil {
				t.Fatal("ReadBootstrap succeeded, want error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want substring %q", err, tt.wantErr)
			}
		})
	}
}
