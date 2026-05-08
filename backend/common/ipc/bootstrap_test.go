package ipc

import (
	"bytes"
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
}
