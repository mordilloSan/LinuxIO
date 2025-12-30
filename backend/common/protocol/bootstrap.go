// Package protocol defines the binary bootstrap protocol for LinuxIO auth/bridge communication.
// Keep in sync with packaging/linuxio_protocol.h
package protocol

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

	// Fixed header size: magic(4) + uid(4) + gid(4) + flags(1) + env(1) + log_fd(4) = 18
	ProtoHeaderSize = 18

	// Flags byte
	ProtoFlagVerbose    = 0x01
	ProtoFlagPrivileged = 0x02

	// Environment mode values
	ProtoEnvProduction  = 0
	ProtoEnvDevelopment = 1
)

// Bootstrap is the configuration passed from auth daemon to bridge via stdin.
// This replaces the previous JSON-based bootstrap.
type Bootstrap struct {
	UID           uint32
	GID           uint32
	Verbose       bool
	Privileged    bool
	EnvMode       uint8 // ProtoEnvProduction or ProtoEnvDevelopment
	LogFD         int32
	SessionID     string
	Username      string
	ServerBaseURL string
	ServerCert    string
}

// IsDevelopment returns true if running in development mode.
func (b *Bootstrap) IsDevelopment() bool {
	return b.EnvMode == ProtoEnvDevelopment
}

// ReadBootstrap reads a binary bootstrap from the given reader.
// Format: [magic:4][uid:4][gid:4][flags:1][env:1][log_fd:4][len:2][session_id]...
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
		EnvMode:    hdr[13],
		LogFD:      int32(binary.BigEndian.Uint32(hdr[14:18])),
	}

	// Read variable-length fields
	var err error
	if b.SessionID, err = readLenStr(r); err != nil {
		return nil, fmt.Errorf("read session_id: %w", err)
	}
	if b.Username, err = readLenStr(r); err != nil {
		return nil, fmt.Errorf("read username: %w", err)
	}
	if b.ServerBaseURL, err = readLenStr(r); err != nil {
		return nil, fmt.Errorf("read server_base_url: %w", err)
	}
	if b.ServerCert, err = readLenStr(r); err != nil {
		return nil, fmt.Errorf("read server_cert: %w", err)
	}

	return b, nil
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
