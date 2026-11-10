package packages

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

// fakePublicKeyBody is a version 4 RSA public-key packet body; seed varies the
// key material so two bodies have different fingerprints.
func fakePublicKeyBody(seed byte) []byte {
	return []byte{
		4,                      // version
		0x65, 0x00, 0x00, seed, // creation time
		1,                // RSA
		0x00, 0x08, seed, // modulus MPI
		0x00, 0x02, 0x03, // exponent MPI
	}
}

func oldFormatPacket(tag byte, body []byte) []byte {
	packet := []byte{0x80 | tag<<2 | 1, 0, 0}
	binary.BigEndian.PutUint16(packet[1:], uint16(len(body)))
	return append(packet, body...)
}

func newFormatPacket(tag byte, body []byte) []byte {
	return append([]byte{0xC0 | tag, byte(len(body))}, body...)
}

func armor(packets ...[]byte) []byte {
	var raw []byte
	for _, packet := range packets {
		raw = append(raw, packet...)
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	var lines []string
	for len(encoded) > 64 {
		lines = append(lines, encoded[:64])
		encoded = encoded[64:]
	}
	lines = append(lines, encoded)
	return []byte("-----BEGIN PGP PUBLIC KEY BLOCK-----\nVersion: test\n\n" +
		strings.Join(lines, "\n") + "\n=AAAA\n-----END PGP PUBLIC KEY BLOCK-----\n")
}

func encode(packet []byte) string {
	return base64.StdEncoding.EncodeToString(packet)
}

// rawBlock wraps body, verbatim, in the armor's begin and end lines.
func rawBlock(body string) []byte {
	return []byte("-----BEGIN PGP PUBLIC KEY BLOCK-----\n" + body + "-----END PGP PUBLIC KEY BLOCK-----\n")
}

func v4Fingerprint(body []byte) string {
	hash := sha1.New()
	hash.Write([]byte{0x99, byte(len(body) >> 8), byte(len(body))})
	hash.Write(body)
	return strings.ToUpper(hex.EncodeToString(hash.Sum(nil)))
}

func TestOpenPGPPrimaryKeyFingerprintsReadsAnthropicsPublishedKey(t *testing.T) {
	key, err := os.ReadFile("testdata/claude-code.asc")
	if err != nil {
		t.Fatal(err)
	}
	fingerprints, err := openPGPPrimaryKeyFingerprints(key)
	if err != nil {
		t.Fatalf("openPGPPrimaryKeyFingerprints: %v", err)
	}
	if len(fingerprints) != 1 || fingerprints[0] != claudeCodeKeyFingerprint {
		t.Fatalf("fingerprints = %v, want only %s", fingerprints, claudeCodeKeyFingerprint)
	}
}

func TestOpenPGPPrimaryKeyFingerprints(t *testing.T) {
	first, second := fakePublicKeyBody(1), fakePublicKeyBody(2)
	userID := newFormatPacket(13, []byte("someone"))
	subkey := oldFormatPacket(14, fakePublicKeyBody(9))

	tests := []struct {
		name    string
		key     []byte
		want    []string
		wantErr string
	}{
		{
			name: "old-format packet with user id and subkey",
			key:  armor(oldFormatPacket(6, first), userID, subkey),
			want: []string{v4Fingerprint(first)},
		},
		{
			name: "new-format packet",
			key:  armor(newFormatPacket(6, first)),
			want: []string{v4Fingerprint(first)},
		},
		{
			name: "a second key in the same block is reported",
			key:  armor(oldFormatPacket(6, first), userID, oldFormatPacket(6, second)),
			want: []string{v4Fingerprint(first), v4Fingerprint(second)},
		},
		{
			name:    "a second armored block",
			key:     append(armor(oldFormatPacket(6, first)), armor(oldFormatPacket(6, second))...),
			wantErr: "unexpected data after the OpenPGP public key block",
		},
		{
			name:    "text before the block",
			key:     append([]byte("Comment: hello\n"), armor(oldFormatPacket(6, first))...),
			wantErr: "unexpected data before the OpenPGP public key block",
		},
		{
			// Another parser could read these lines as key data.
			name:    "key data where the armor headers belong",
			key:     rawBlock(encode(oldFormatPacket(6, second)) + "\n\n" + encode(oldFormatPacket(6, first)) + "\n"),
			wantErr: "malformed armor header",
		},
		{
			// Other parsers stop at the checksum; this one must not read past it.
			name:    "key data after the armor checksum",
			key:     rawBlock("\n" + encode(oldFormatPacket(6, first)) + "\n=AAAA\n" + encode(oldFormatPacket(6, second)) + "\n"),
			wantErr: "unexpected data after the armor checksum",
		},
		{
			name:    "a secret key beside the public key",
			key:     armor(oldFormatPacket(6, first), userID, oldFormatPacket(5, second)),
			wantErr: "unexpected OpenPGP packet (tag 5)",
		},
		{
			name:    "an unknown packet beside the public key",
			key:     armor(oldFormatPacket(6, first), newFormatPacket(60, []byte("x"))),
			wantErr: "unexpected OpenPGP packet (tag 60)",
		},
		{name: "not armored", key: []byte("<html>not found</html>"), wantErr: "no OpenPGP public key block"},
		{name: "empty", key: nil, wantErr: "no OpenPGP public key block"},
		{
			name:    "unterminated block",
			key:     []byte("-----BEGIN PGP PUBLIC KEY BLOCK-----\n\nAAAA\n"),
			wantErr: "unterminated",
		},
		{
			name:    "bad base64",
			key:     []byte("-----BEGIN PGP PUBLIC KEY BLOCK-----\n\n!!!!\n-----END PGP PUBLIC KEY BLOCK-----\n"),
			wantErr: "decode",
		},
		{
			name:    "packet longer than the data",
			key:     armor(oldFormatPacket(6, first)[:8]),
			wantErr: "truncated",
		},
		{
			name:    "no key packet",
			key:     armor(userID),
			wantErr: "no public key",
		},
		{
			name:    "unsupported key version",
			key:     armor(oldFormatPacket(6, append([]byte{5}, first[1:]...))),
			wantErr: "version 5",
		},
		{
			name:    "not an OpenPGP packet",
			key:     armor([]byte{0x00, 0x01, 0x02}),
			wantErr: "not an OpenPGP packet",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := openPGPPrimaryKeyFingerprints(tt.key)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("fingerprints = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSinglePinnedFingerprint(t *testing.T) {
	first, second := fakePublicKeyBody(1), fakePublicKeyBody(2)
	tests := []struct {
		name    string
		key     []byte
		wantErr string
	}{
		{name: "the pinned key alone", key: armor(oldFormatPacket(6, first))},
		{name: "another key", key: armor(oldFormatPacket(6, second)), wantErr: "signing key fingerprint is " + v4Fingerprint(second)},
		{
			name:    "the pinned key and another in one block",
			key:     armor(oldFormatPacket(6, first), oldFormatPacket(6, second)),
			wantErr: "expected one signing key, found 2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := singlePinnedFingerprint(tt.key, v4Fingerprint(first))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}
