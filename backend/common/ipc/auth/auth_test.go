package auth

import (
	"bytes"
	"strings"
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

func TestWriteAuthRequest_EncodesRemoteHost(t *testing.T) {
	var buf bytes.Buffer
	req := &AuthRequest{
		Verbose:    true,
		User:       "miguel",
		Password:   "pw",
		SessionID:  "session-1",
		RemoteHost: "192.168.1.239",
	}

	if err := WriteAuthRequest(&buf, req); err != nil {
		t.Fatalf("WriteAuthRequest: %v", err)
	}

	header := buf.Next(AuthReqHeaderSize)
	if len(header) != AuthReqHeaderSize {
		t.Fatalf("header len = %d, want %d", len(header), AuthReqHeaderSize)
	}
	if header[0] != ProtoMagic0 || header[1] != ProtoMagic1 || header[2] != ProtoMagic2 || header[3] != ProtoVersion {
		t.Fatalf("bad header: %v", header)
	}
	if header[4]&ReqFlagVerbose == 0 {
		t.Fatalf("verbose flag not set: %v", header)
	}

	fields := []string{"miguel", "pw", "session-1", "192.168.1.239"}
	for _, want := range fields {
		got, err := readLenStr(&buf)
		if err != nil {
			t.Fatalf("readLenStr: %v", err)
		}
		if got != want {
			t.Fatalf("field = %q, want %q", got, want)
		}
	}
}

func TestWriteAuthRequest_RejectsFieldsTheCReceiverCannotRepresent(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
		value     string
		apply     func(*AuthRequest, string)
	}{
		{
			name:      "username length",
			fieldName: "user",
			value:     strings.Repeat("u", MaxUsername),
			apply:     func(req *AuthRequest, value string) { req.User = value },
		},
		{
			name:      "password length",
			fieldName: "password",
			value:     strings.Repeat("p", MaxPassword),
			apply:     func(req *AuthRequest, value string) { req.Password = value },
		},
		{
			name:      "session ID length",
			fieldName: "session_id",
			value:     strings.Repeat("s", MaxSessionID),
			apply:     func(req *AuthRequest, value string) { req.SessionID = value },
		},
		{
			name:      "remote host length",
			fieldName: "remote_host",
			value:     strings.Repeat("h", MaxRemoteHost),
			apply:     func(req *AuthRequest, value string) { req.RemoteHost = value },
		},
		{
			name:      "username NUL",
			fieldName: "user",
			value:     "mi\x00guel",
			apply:     func(req *AuthRequest, value string) { req.User = value },
		},
		{
			name:      "password NUL",
			fieldName: "password",
			value:     "sec\x00ret",
			apply:     func(req *AuthRequest, value string) { req.Password = value },
		},
		{
			name:      "session ID NUL",
			fieldName: "session_id",
			value:     "session\x001",
			apply:     func(req *AuthRequest, value string) { req.SessionID = value },
		},
		{
			name:      "remote host NUL",
			fieldName: "remote_host",
			value:     "192.0.2.\x001",
			apply:     func(req *AuthRequest, value string) { req.RemoteHost = value },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &AuthRequest{
				User:       "miguel",
				Password:   "secret",
				SessionID:  "session-1",
				RemoteHost: "192.0.2.1",
			}
			tt.apply(req, tt.value)

			err := WriteAuthRequest(&bytes.Buffer{}, req)
			if err == nil {
				t.Fatal("WriteAuthRequest succeeded, want rejection")
			}
			if !strings.Contains(err.Error(), tt.fieldName) {
				t.Fatalf("error = %q, want field %q", err, tt.fieldName)
			}
		})
	}
}

func TestWriteAuthRequest_AcceptsMaximumRepresentableFieldLengths(t *testing.T) {
	req := &AuthRequest{
		User:       strings.Repeat("u", MaxUsername-1),
		Password:   strings.Repeat("p", MaxPassword-1),
		SessionID:  strings.Repeat("s", MaxSessionID-1),
		RemoteHost: strings.Repeat("h", MaxRemoteHost-1),
	}

	if err := WriteAuthRequest(&bytes.Buffer{}, req); err != nil {
		t.Fatalf("WriteAuthRequest: %v", err)
	}
}

func TestReadAuthResponse_RejectsMalformedFrames(t *testing.T) {
	validHeader := []byte{
		ProtoMagic0,
		ProtoMagic1,
		ProtoMagic2,
		ProtoVersion,
		StatusOK,
		ModeUnprivileged,
		byte(ResultOK),
		0,
	}
	tests := []struct {
		name    string
		frame   []byte
		wantErr string
	}{
		{name: "short header", frame: validHeader[:7], wantErr: "read header"},
		{name: "bad magic", frame: append([]byte{'X'}, validHeader[1:]...), wantErr: "invalid response magic"},
		{name: "bad version", frame: append(append([]byte{}, validHeader[:3]...), append([]byte{ProtoVersion + 1}, validHeader[4:]...)...), wantErr: "unsupported auth protocol version"},
		{name: "missing uid", frame: validHeader, wantErr: "read uid"},
		{name: "truncated uid", frame: append(append([]byte{}, validHeader...), 0, 0), wantErr: "read uid"},
		{name: "missing gid", frame: append(append([]byte{}, validHeader...), 0, 0, 3, 232), wantErr: "read gid"},
		{name: "missing username", frame: append(append([]byte{}, validHeader...), 0, 0, 3, 232, 0, 0, 3, 233), wantErr: "read username"},
		{
			name: "missing error",
			frame: []byte{
				ProtoMagic0, ProtoMagic1, ProtoMagic2, ProtoVersion,
				StatusError, ModeUnprivileged, byte(ResultAuthFailed), 0,
			},
			wantErr: "read error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ReadAuthResponse(bytes.NewReader(tt.frame))
			if err == nil {
				t.Fatal("ReadAuthResponse succeeded, want error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want substring %q", err, tt.wantErr)
			}
		})
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
