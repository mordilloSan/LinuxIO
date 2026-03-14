// Auth protocol types and helpers for LinuxIO auth/bridge communication.
// Keep in sync with packaging/linuxio_protocol.h
package ipc

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Binary bootstrap protocol constants
const (
	// Magic bytes "LIO" + version
	ProtoMagic0  = 'L'
	ProtoMagic1  = 'I'
	ProtoMagic2  = 'O'
	ProtoVersion = 1

	// Fixed header size: magic(4) + uid(4) + gid(4) + flags(1) = 13
	ProtoHeaderSize = 13

	// Flags byte
	ProtoFlagVerbose    = 0x01
	ProtoFlagPrivileged = 0x02
)

// Max lengths for fields (used for validation)
const (
	MaxUsername  = 256
	MaxPassword  = 8192
	MaxSessionID = 64
	MaxError     = 256
)

// Auth request/response protocol constants
const (
	AuthReqHeaderSize  = 8
	AuthRespHeaderSize = 8

	// Request flags
	ReqFlagVerbose = 0x01

	// Status values
	StatusOK    = 0
	StatusError = 1

	// Mode values
	ModeUnprivileged = 0
	ModePrivileged   = 1
)

// Bootstrap is the configuration passed from auth daemon to bridge via stdin.
type Bootstrap struct {
	UID        uint32
	GID        uint32
	Verbose    bool
	Privileged bool
	SessionID  string
	Username   string
}

// AuthRequest is the binary request sent to the auth daemon (Server -> Auth)
type AuthRequest struct {
	Verbose   bool
	User      string
	Password  string
	SessionID string
}

// AuthResponse is the binary response from the auth daemon (Auth -> Server)
type AuthResponse struct {
	Status uint8
	Mode   uint8
	Error  string
}

// ReadBootstrap reads a binary bootstrap from the given reader.
// Format: [magic:4][uid:4][gid:4][flags:1][len:2][session_id]...
func ReadBootstrap(r io.Reader) (*Bootstrap, error) {
	// Read fixed header
	var hdr [ProtoHeaderSize]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	// Validate magic
	if hdr[0] != ProtoMagic0 || hdr[1] != ProtoMagic1 || hdr[2] != ProtoMagic2 {
		return nil, errors.New("invalid bootstrap magic")
	}
	if hdr[3] != ProtoVersion {
		return nil, fmt.Errorf("unsupported bootstrap version: %d", hdr[3])
	}

	b := &Bootstrap{
		UID:        binary.BigEndian.Uint32(hdr[4:8]),
		GID:        binary.BigEndian.Uint32(hdr[8:12]),
		Verbose:    hdr[12]&ProtoFlagVerbose != 0,
		Privileged: hdr[12]&ProtoFlagPrivileged != 0,
	}

	// Read variable-length fields
	var err error
	if b.SessionID, err = readLenStr(r); err != nil {
		return nil, fmt.Errorf("read session_id: %w", err)
	}
	if b.Username, err = readLenStr(r); err != nil {
		return nil, fmt.Errorf("read username: %w", err)
	}

	return b, nil
}

// WriteAuthRequest writes a binary auth request to the writer.
func WriteAuthRequest(w io.Writer, req *AuthRequest) error {
	// Write header
	var header [AuthReqHeaderSize]byte
	header[0] = ProtoMagic0
	header[1] = ProtoMagic1
	header[2] = ProtoMagic2
	header[3] = ProtoVersion

	var flags uint8
	if req.Verbose {
		flags |= ReqFlagVerbose
	}
	header[4] = flags
	header[5] = 0 // reserved
	header[6] = 0 // reserved
	header[7] = 0 // reserved

	if _, err := w.Write(header[:]); err != nil {
		return fmt.Errorf("write header: %w", err)
	}

	// Write variable-length fields
	if err := writeLenStr(w, req.User); err != nil {
		return fmt.Errorf("write user: %w", err)
	}
	if err := writeLenStr(w, req.Password); err != nil {
		return fmt.Errorf("write password: %w", err)
	}
	if err := writeLenStr(w, req.SessionID); err != nil {
		return fmt.Errorf("write session_id: %w", err)
	}

	return nil
}

// ReadAuthResponse reads a binary auth response from the reader.
func ReadAuthResponse(r io.Reader) (*AuthResponse, error) {
	var header [AuthRespHeaderSize]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	// Validate magic
	if header[0] != ProtoMagic0 || header[1] != ProtoMagic1 ||
		header[2] != ProtoMagic2 || header[3] != ProtoVersion {
		return nil, errors.New("invalid response magic")
	}

	resp := &AuthResponse{
		Status: header[4],
		Mode:   header[5],
	}

	// Read error message if status is error
	if resp.Status == StatusError {
		errStr, err := readLenStr(r)
		if err != nil {
			return nil, fmt.Errorf("read error: %w", err)
		}
		resp.Error = errStr
	}

	return resp, nil
}

// IsPrivileged returns true if the mode indicates privileged access
func (r *AuthResponse) IsPrivileged() bool {
	return r.Mode == ModePrivileged
}

// IsOK returns true if the response status is OK
func (r *AuthResponse) IsOK() bool {
	return r.Status == StatusOK
}

// readLenStr reads a length-prefixed string (2-byte length + data).
func readLenStr(r io.Reader) (string, error) {
	var lenBuf [2]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return "", err
	}
	length := binary.BigEndian.Uint16(lenBuf[:])
	if length == 0 {
		return "", nil
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return "", err
	}
	return string(data), nil
}

// writeLenStr writes a length-prefixed string (2-byte length + data).
func writeLenStr(w io.Writer, s string) error {
	length := min(len(s), 0xFFFF)
	var lenBuf [2]byte
	binary.BigEndian.PutUint16(lenBuf[:], uint16(length))
	if _, err := w.Write(lenBuf[:]); err != nil {
		return err
	}
	if length > 0 {
		if _, err := w.Write([]byte(s[:length])); err != nil {
			return err
		}
	}
	return nil
}
