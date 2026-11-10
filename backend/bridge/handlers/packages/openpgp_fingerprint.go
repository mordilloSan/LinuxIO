package packages

import (
	"bytes"
	"crypto/sha1" // OpenPGP version 4 fingerprints are SHA-1 by definition.
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
)

const (
	pgpArmorBegin = "-----BEGIN PGP PUBLIC KEY BLOCK-----"
	pgpArmorEnd   = "-----END PGP PUBLIC KEY BLOCK-----"

	// Packet tags a public key file is made of (RFC 4880 section 11.1).
	pgpSignatureTag     = 2
	pgpPublicKeyTag     = 6
	pgpUserIDTag        = 13
	pgpPublicSubkeyTag  = 14
	pgpUserAttributeTag = 17
)

// openPGPPrimaryKeyFingerprints returns the fingerprint of every primary
// public key in an ASCII-armored OpenPGP key file, in order, as upper-case
// hex. A package manager trusts every key in such a file, so a caller that
// pins one fingerprint must also check that it is the only one returned.
//
// The file must be plain: one armored block holding only what a public key is
// made of. Whatever another parser might read differently is an error rather
// than something to skip, because the package manager reads the same bytes
// with its own parser. Only version 4 keys (RFC 4880) are understood.
func openPGPPrimaryKeyFingerprints(armored []byte) ([]string, error) {
	packets, err := dearmorPublicKey(armored)
	if err != nil {
		return nil, err
	}
	var fingerprints []string
	for len(packets) > 0 {
		tag, body, rest, err := nextPGPPacket(packets)
		if err != nil {
			return nil, err
		}
		packets = rest
		switch tag {
		case pgpPublicKeyTag:
			fingerprint, err := v4KeyFingerprint(body)
			if err != nil {
				return nil, err
			}
			fingerprints = append(fingerprints, fingerprint)
		case pgpSignatureTag, pgpUserIDTag, pgpPublicSubkeyTag, pgpUserAttributeTag:
			// The rest of a public key: none of these is a key to trust.
		default:
			// A secret key above all, which some parsers load as a key.
			return nil, fmt.Errorf("unexpected OpenPGP packet (tag %d) in a public key file", tag)
		}
	}
	if len(fingerprints) == 0 {
		return nil, errors.New("no public key in the OpenPGP data")
	}
	return fingerprints, nil
}

func v4KeyFingerprint(body []byte) (string, error) {
	if len(body) == 0 || body[0] != 4 {
		version := -1
		if len(body) > 0 {
			version = int(body[0])
		}
		return "", fmt.Errorf("unsupported OpenPGP key version %d", version)
	}
	hash := sha1.New()
	hash.Write([]byte{0x99, byte(len(body) >> 8), byte(len(body))})
	hash.Write(body)
	return strings.ToUpper(hex.EncodeToString(hash.Sum(nil))), nil
}

// dearmorPublicKey decodes a file that is exactly one armored public key
// block and returns its packets.
func dearmorPublicKey(data []byte) ([]byte, error) {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	body, err := armorBlockBody(lines)
	if err != nil {
		return nil, err
	}
	encoded, err := armorBlockData(body)
	if err != nil {
		return nil, err
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode OpenPGP public key block: %w", err)
	}
	return decoded, nil
}

func notBlank(line string) bool { return line != "" }

// armorBlockBody returns the lines between the begin and end lines of the
// one block the file holds, refusing anything outside it.
func armorBlockBody(lines []string) ([]string, error) {
	begin := slices.Index(lines, pgpArmorBegin)
	if begin < 0 {
		return nil, errors.New("no OpenPGP public key block found")
	}
	if slices.ContainsFunc(lines[:begin], notBlank) {
		return nil, errors.New("unexpected data before the OpenPGP public key block")
	}
	body := lines[begin+1:]
	end := slices.Index(body, pgpArmorEnd)
	if end < 0 {
		return nil, errors.New("unterminated OpenPGP public key block")
	}
	if slices.ContainsFunc(body[end+1:], notBlank) {
		return nil, errors.New("unexpected data after the OpenPGP public key block")
	}
	return body[:end], nil
}

// armorBlockData returns the base64 key data of a block body: what follows
// the armor headers and their blank line, up to the optional checksum line.
func armorBlockData(body []string) (string, error) {
	blank := slices.Index(body, "")
	if blank < 0 {
		return "", errors.New("malformed armor header: no blank line before the key data")
	}
	for _, header := range body[:blank] {
		key, _, ok := strings.Cut(header, ": ")
		if !ok || key == "" || strings.ContainsAny(key, " \t") {
			return "", errors.New("malformed armor header")
		}
	}
	var encoded strings.Builder
	data := body[blank+1:]
	for i, line := range data {
		// A line starting with "=" is the armor checksum and ends the data.
		if strings.HasPrefix(line, "=") {
			if slices.ContainsFunc(data[i+1:], notBlank) {
				return "", errors.New("unexpected data after the armor checksum")
			}
			break
		}
		encoded.WriteString(line)
	}
	return encoded.String(), nil
}

// nextPGPPacket splits the first packet off data and returns its tag, its
// body and what follows it.
func nextPGPPacket(data []byte) (tag byte, body, rest []byte, err error) {
	if data[0]&0x80 == 0 {
		return 0, nil, nil, errors.New("not an OpenPGP packet")
	}
	var length, header int
	if data[0]&0x40 != 0 {
		tag = data[0] & 0x3f
		length, header, err = newFormatPGPLength(data[1:])
	} else {
		tag = data[0] >> 2 & 0x0f
		length, header, err = oldFormatPGPLength(data[0]&0x03, data[1:])
	}
	if err != nil {
		return 0, nil, nil, err
	}
	start := 1 + header
	if length > len(data)-start {
		return 0, nil, nil, errors.New("truncated OpenPGP packet")
	}
	return tag, data[start : start+length], data[start+length:], nil
}

// newFormatPGPLength decodes a new-format body length and reports how many
// octets it occupied.
func newFormatPGPLength(data []byte) (length, octets int, err error) {
	if len(data) == 0 {
		return 0, 0, errors.New("truncated OpenPGP packet")
	}
	first := int(data[0])
	switch {
	case first < 192:
		return first, 1, nil
	case first < 224:
		if len(data) < 2 {
			return 0, 0, errors.New("truncated OpenPGP packet")
		}
		return (first-192)<<8 + int(data[1]) + 192, 2, nil
	case first == 255:
		if len(data) < 5 {
			return 0, 0, errors.New("truncated OpenPGP packet")
		}
		length, err = pgpLength32(data[1:5])
		return length, 5, err
	default:
		return 0, 0, errors.New("OpenPGP partial body lengths are not supported")
	}
}

// oldFormatPGPLength decodes an old-format body length of the given type and
// reports how many octets it occupied.
func oldFormatPGPLength(lengthType byte, data []byte) (length, octets int, err error) {
	if lengthType == 3 {
		return 0, 0, errors.New("OpenPGP indeterminate lengths are not supported")
	}
	octets = 1 << lengthType
	if len(data) < octets {
		return 0, 0, errors.New("truncated OpenPGP packet")
	}
	switch octets {
	case 1:
		return int(data[0]), octets, nil
	case 2:
		return int(binary.BigEndian.Uint16(data)), octets, nil
	default:
		length, err = pgpLength32(data[:4])
		return length, octets, err
	}
}

// pgpLength32 reads a four-octet body length. Key packets are a few
// kilobytes, so a huge value means the data is not a key.
func pgpLength32(data []byte) (int, error) {
	value := binary.BigEndian.Uint32(data)
	if value > 1<<24 {
		return 0, errors.New("OpenPGP packet is implausibly large")
	}
	return int(value), nil
}

// singlePinnedFingerprint checks that armored holds exactly one primary key
// and that it is the pinned one.
func singlePinnedFingerprint(armored []byte, pinned string) error {
	fingerprints, err := openPGPPrimaryKeyFingerprints(bytes.TrimSpace(armored))
	if err != nil {
		return err
	}
	if len(fingerprints) != 1 {
		return fmt.Errorf("expected one signing key, found %d", len(fingerprints))
	}
	if fingerprints[0] != pinned {
		return fmt.Errorf("signing key fingerprint is %s, expected %s", fingerprints[0], pinned)
	}
	return nil
}
