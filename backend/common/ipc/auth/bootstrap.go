// Binary bootstrap protocol for LinuxIO auth/bridge communication.
// Keep in sync with packaging/linuxio_protocol.h
package auth

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
	ProtoVersion = 3

	// Fixed header size: magic(4) + uid(4) + gid(4) + flags(1) = 13
	ProtoHeaderSize = 13

	// Flags byte
	ProtoFlagVerbose    = 0x01
	ProtoFlagPrivileged = 0x02
	ProtoFlagReadyAck   = 0x04

	// Startup-status protocol bytes over the inherited status fd. Ready and
	// Error travel bridge -> auth; Go travels auth -> bridge. They are used
	// only when ProtoFlagReadyAck is set in the bootstrap.
	ProtoStartupReady = 0x02
	ProtoStartupError = 0x03
	ProtoStartupGo    = 0x04

	// The launcher truncates startup error messages to PROTO_MAX_ERROR-1.
	MaxStartupErrorLen = 255
)

// Bootstrap is the configuration passed from auth daemon to bridge via stdin.
// This replaces the previous JSON-based bootstrap.
type Bootstrap struct {
	UID        uint32
	GID        uint32
	Verbose    bool
	Privileged bool
	// ReadyAck means the launcher left the startup-status fd open across
	// exec and requires a ready/error byte before reporting login success.
	ReadyAck  bool
	SessionID string
	Username  string
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
		ReadyAck:   hdr[12]&ProtoFlagReadyAck != 0,
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
