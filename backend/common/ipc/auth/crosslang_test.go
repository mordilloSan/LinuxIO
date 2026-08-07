// Cross-language tests: drive the REAL C parser/encoder (via the
// linuxio-auth-frametool test binary) with the REAL Go encoder/decoder in
// this package, so a wire-format drift between the two languages fails a
// test instead of only surfacing at runtime. Requires
// LINUXIO_AUTH_FRAMETOOL to point at a built frametool binary; run via
// 'make test-auth-protocol'.
package auth

import (
	"bytes"
	"encoding/binary"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

func frametoolPath(t *testing.T) string {
	t.Helper()
	path := os.Getenv("LINUXIO_AUTH_FRAMETOOL")
	if path == "" {
		t.Skip("LINUXIO_AUTH_FRAMETOOL not set; run via make test-auth-protocol")
	}
	return path
}

// runFrametool feeds stdin to the frametool binary and returns its stdout,
// combined stderr (for diagnostics only), and exit code.
func runFrametool(t *testing.T, path string, stdin []byte, args ...string) (stdout string, exitCode int) {
	t.Helper()
	cmd := exec.Command(path, args...)
	cmd.Stdin = bytes.NewReader(stdin)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("running frametool %v: %v (stderr: %s)", args, err, errBuf.String())
		}
	}
	if testing.Verbose() && errBuf.Len() > 0 {
		t.Logf("frametool stderr: %s", errBuf.String())
	}
	return outBuf.String(), code
}

// parsedFields splits frametool's "key=value" stdout lines into a map.
func parsedFields(t *testing.T, stdout string) map[string]string {
	t.Helper()
	fields := make(map[string]string)
	for line := range strings.SplitSeq(strings.TrimRight(stdout, "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			t.Fatalf("unparseable frametool line: %q", line)
		}
		fields[parts[0]] = parts[1]
	}
	return fields
}

func TestCrossLanguageRequestRoundTrip(t *testing.T) {
	path := frametoolPath(t)

	tests := []struct {
		name string
		req  AuthRequest
	}{
		{
			name: "typical ASCII",
			req: AuthRequest{
				Verbose:    true,
				User:       "miguel",
				Password:   "s3cr3t",
				SessionID:  "session-1",
				RemoteHost: "192.0.2.10",
			},
		},
		{
			name: "unicode username",
			req: AuthRequest{
				User:       "míguel",
				Password:   "hunter2",
				SessionID:  "session-2",
				RemoteHost: "2001:db8::1",
			},
		},
		{
			name: "max-length fields",
			req: AuthRequest{
				Verbose:    true,
				User:       strings.Repeat("u", MaxUsername-1),
				Password:   strings.Repeat("p", MaxPassword-1),
				SessionID:  strings.Repeat("s", MaxSessionID-1),
				RemoteHost: strings.Repeat("h", MaxRemoteHost-1),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteAuthRequest(&buf, &tt.req); err != nil {
				t.Fatalf("WriteAuthRequest: %v", err)
			}

			stdout, code := runFrametool(t, path, buf.Bytes(), "parse-request")
			if code != 0 {
				t.Fatalf("parse-request exited %d, stdout: %s", code, stdout)
			}

			wantVerbose := "0"
			if tt.req.Verbose {
				wantVerbose = "1"
			}
			want := map[string]string{
				"user":         tt.req.User,
				"password_len": strconv.Itoa(len(tt.req.Password)),
				"session_id":   tt.req.SessionID,
				"remote_host":  tt.req.RemoteHost,
				"verbose":      wantVerbose,
			}
			fields := parsedFields(t, stdout)
			for key, wantVal := range want {
				if fields[key] != wantVal {
					t.Errorf("%s = %q, want %q", key, fields[key], wantVal)
				}
			}
		})
	}
}

// rawRequestField appends a raw length-prefixed field, bypassing
// WriteAuthRequest's own validation so malformed frames can be constructed.
func rawRequestField(buf *bytes.Buffer, length uint16, data []byte) {
	var lenBuf [2]byte
	binary.BigEndian.PutUint16(lenBuf[:], length)
	buf.Write(lenBuf[:])
	buf.Write(data)
}

func rawRequestHeader(buf *bytes.Buffer, magic0, magic1, magic2, version, flags byte) {
	buf.Write([]byte{magic0, magic1, magic2, version, flags, 0, 0, 0})
}

func TestCrossLanguageRequestRejection(t *testing.T) {
	path := frametoolPath(t)

	validFields := func(buf *bytes.Buffer) {
		rawRequestField(buf, 6, []byte("miguel"))
		rawRequestField(buf, 6, []byte("secret"))
		rawRequestField(buf, 9, []byte("session-1"))
		rawRequestField(buf, 10, []byte("192.0.2.10"))
	}

	tests := []struct {
		name  string
		build func() []byte
	}{
		{
			name: "bad magic",
			build: func() []byte {
				var buf bytes.Buffer
				rawRequestHeader(&buf, 'X', 'I', 'O', ProtoVersion, 0)
				validFields(&buf)
				return buf.Bytes()
			},
		},
		{
			name: "wrong version",
			build: func() []byte {
				var buf bytes.Buffer
				rawRequestHeader(&buf, ProtoMagic0, ProtoMagic1, ProtoMagic2, ProtoVersion+1, 0)
				validFields(&buf)
				return buf.Bytes()
			},
		},
		{
			name: "field length prefix at max (session_id len 64)",
			build: func() []byte {
				var buf bytes.Buffer
				rawRequestHeader(&buf, ProtoMagic0, ProtoMagic1, ProtoMagic2, ProtoVersion, 0)
				rawRequestField(&buf, 6, []byte("miguel"))
				rawRequestField(&buf, 6, []byte("secret"))
				rawRequestField(&buf, MaxSessionID, bytes.Repeat([]byte("s"), MaxSessionID))
				rawRequestField(&buf, 10, []byte("192.0.2.10"))
				return buf.Bytes()
			},
		},
		{
			name: "embedded NUL inside a field",
			build: func() []byte {
				var buf bytes.Buffer
				rawRequestHeader(&buf, ProtoMagic0, ProtoMagic1, ProtoMagic2, ProtoVersion, 0)
				rawRequestField(&buf, 6, []byte("mi\x00uel"))
				rawRequestField(&buf, 6, []byte("secret"))
				rawRequestField(&buf, 9, []byte("session-1"))
				rawRequestField(&buf, 10, []byte("192.0.2.10"))
				return buf.Bytes()
			},
		},
		{
			name: "empty password",
			build: func() []byte {
				var buf bytes.Buffer
				rawRequestHeader(&buf, ProtoMagic0, ProtoMagic1, ProtoMagic2, ProtoVersion, 0)
				rawRequestField(&buf, 6, []byte("miguel"))
				rawRequestField(&buf, 0, nil)
				rawRequestField(&buf, 9, []byte("session-1"))
				rawRequestField(&buf, 10, []byte("192.0.2.10"))
				return buf.Bytes()
			},
		},
		{
			name: "username with a space",
			build: func() []byte {
				var buf bytes.Buffer
				rawRequestHeader(&buf, ProtoMagic0, ProtoMagic1, ProtoMagic2, ProtoVersion, 0)
				rawRequestField(&buf, 8, []byte("mi guel1"))
				rawRequestField(&buf, 6, []byte("secret"))
				rawRequestField(&buf, 9, []byte("session-1"))
				rawRequestField(&buf, 10, []byte("192.0.2.10"))
				return buf.Bytes()
			},
		},
		{
			name: "truncated stream",
			build: func() []byte {
				var buf bytes.Buffer
				rawRequestHeader(&buf, ProtoMagic0, ProtoMagic1, ProtoMagic2, ProtoVersion, 0)
				rawRequestField(&buf, 6, []byte("miguel"))
				buf.Write([]byte{0, 6, 's', 'e'}) // password length prefix, then cut short
				return buf.Bytes()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, code := runFrametool(t, path, tt.build(), "parse-request")
			if code != 1 {
				t.Fatalf("parse-request exited %d, want 1; stdout: %s", code, stdout)
			}
			if !strings.HasPrefix(stdout, "error=") {
				t.Fatalf("stdout = %q, want an error= line", stdout)
			}
		})
	}
}

func TestCrossLanguageOKResponse(t *testing.T) {
	path := frametoolPath(t)

	stdout, code := runFrametool(t, path, nil, "emit-ok-response", "user", "66051", "2696432848", "1")
	if code != 0 {
		t.Fatalf("emit-ok-response exited %d, stderr-in-stdout: %s", code, stdout)
	}

	resp, err := ReadAuthResponse(strings.NewReader(stdout))
	if err != nil {
		t.Fatalf("ReadAuthResponse: %v", err)
	}
	if !resp.IsOK() {
		t.Fatalf("status = %d, want OK", resp.Status)
	}
	if !resp.IsPrivileged() {
		t.Fatal("expected privileged mode")
	}
	if resp.User.UID != 66051 {
		t.Errorf("uid = %d, want %d", resp.User.UID, 66051)
	}
	if resp.User.GID != 2696432848 {
		t.Errorf("gid = %d, want %d", resp.User.GID, 2696432848)
	}
	if resp.User.Username != "user" {
		t.Errorf("username = %q, want %q", resp.User.Username, "user")
	}
}

func TestCrossLanguageErrorResponse(t *testing.T) {
	path := frametoolPath(t)

	t.Run("with message", func(t *testing.T) {
		stdout, code := runFrametool(t, path, nil, "emit-error-response",
			strconv.Itoa(int(ResultAuthFailed)), "authentication failed")
		if code != 0 {
			t.Fatalf("emit-error-response exited %d, output: %s", code, stdout)
		}

		resp, err := ReadAuthResponse(strings.NewReader(stdout))
		if err != nil {
			t.Fatalf("ReadAuthResponse: %v", err)
		}
		if resp.Status != StatusError {
			t.Fatalf("status = %d, want error", resp.Status)
		}
		if resp.ResultCode != ResultAuthFailed {
			t.Errorf("result code = %d, want %d", resp.ResultCode, ResultAuthFailed)
		}
		if resp.Error != "authentication failed" {
			t.Errorf("error = %q, want %q", resp.Error, "authentication failed")
		}
	})

	// Regression test: a NULL error message must still emit the zero-length
	// lenstr so the peer can consume a complete frame instead of stalling.
	t.Run("null message", func(t *testing.T) {
		stdout, code := runFrametool(t, path, nil, "emit-error-response-null",
			strconv.Itoa(int(ResultInternalError)))
		if code != 0 {
			t.Fatalf("emit-error-response-null exited %d, output: %s", code, stdout)
		}

		resp, err := ReadAuthResponse(strings.NewReader(stdout))
		if err != nil {
			t.Fatalf("ReadAuthResponse: %v", err)
		}
		if resp.Status != StatusError {
			t.Fatalf("status = %d, want error", resp.Status)
		}
		if resp.ResultCode != ResultInternalError {
			t.Errorf("result code = %d, want %d", resp.ResultCode, ResultInternalError)
		}
		if resp.Error != "" {
			t.Errorf("error = %q, want empty string", resp.Error)
		}
	})
}

// decodeBootstrapViaFrametool runs an emit-bootstrap invocation and decodes
// its output with the real Go decoder, failing the test on any error.
func decodeBootstrapViaFrametool(t *testing.T, path string, args ...string) *Bootstrap {
	t.Helper()
	stdout, code := runFrametool(t, path, nil, args...)
	if code != 0 {
		t.Fatalf("emit-bootstrap exited %d, output: %s", code, stdout)
	}
	bootstrap, err := ReadBootstrap(strings.NewReader(stdout))
	if err != nil {
		t.Fatalf("ReadBootstrap: %v", err)
	}
	return bootstrap
}

func TestCrossLanguageBootstrap(t *testing.T) {
	path := frametoolPath(t)

	t.Run("verbose and privileged", func(t *testing.T) {
		bootstrap := decodeBootstrapViaFrametool(t, path, "emit-bootstrap",
			"sid", "user", "1000", "1000", "1", "1")
		if bootstrap.SessionID != "sid" || bootstrap.Username != "user" {
			t.Errorf("session/user = %q/%q, want %q/%q", bootstrap.SessionID, bootstrap.Username, "sid", "user")
		}
		if bootstrap.UID != 1000 || bootstrap.GID != 1000 {
			t.Errorf("ids = %d/%d, want 1000/1000", bootstrap.UID, bootstrap.GID)
		}
		if !bootstrap.Verbose || !bootstrap.Privileged || !bootstrap.ReadyAck {
			t.Errorf("flags = verbose:%v privileged:%v readyAck:%v, want all true",
				bootstrap.Verbose, bootstrap.Privileged, bootstrap.ReadyAck)
		}
	})

	t.Run("zero flags still sets ReadyAck", func(t *testing.T) {
		bootstrap := decodeBootstrapViaFrametool(t, path, "emit-bootstrap",
			"sid", "user", "0", "0", "0", "0")
		if bootstrap.Verbose || bootstrap.Privileged {
			t.Errorf("flags = verbose:%v privileged:%v, want both false", bootstrap.Verbose, bootstrap.Privileged)
		}
		if !bootstrap.ReadyAck {
			t.Error("ReadyAck = false, want true (the C launcher always sets it)")
		}
	})
}
