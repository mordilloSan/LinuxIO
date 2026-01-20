// Package protocol defines shared types and constants for LinuxIO auth communication.
// Keep in sync with packaging/linuxio_protocol.h
package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
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

// writeLenStr writes a length-prefixed string (2-byte length + data).
func writeLenStr(w io.Writer, s string) error {
	length := len(s)
	if length > 0xFFFF {
		length = 0xFFFF
	}
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

// IsPrivileged returns true if the mode indicates privileged access
func (r *AuthResponse) IsPrivileged() bool {
	return r.Mode == ModePrivileged
}

// IsOK returns true if the response status is OK
func (r *AuthResponse) IsOK() bool {
	return r.Status == StatusOK
}
