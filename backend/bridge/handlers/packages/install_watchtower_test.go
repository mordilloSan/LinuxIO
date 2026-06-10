package packages

import "testing"

func TestWatchtowerAssetForArch(t *testing.T) {
	const testVersion = "9.9.9"

	tests := []struct {
		name      string
		goarch    string
		assetName string
	}{
		{
			name:      "amd64",
			goarch:    "amd64",
			assetName: "watchtower_linux_amd64_9.9.9.tar.gz",
		},
		{
			name:      "arm64",
			goarch:    "arm64",
			assetName: "watchtower_linux_arm64v8_9.9.9.tar.gz",
		},
		{
			name:      "arm",
			goarch:    "arm",
			assetName: "watchtower_linux_armhf_9.9.9.tar.gz",
		},
		{
			name:      "386",
			goarch:    "386",
			assetName: "watchtower_linux_i386_9.9.9.tar.gz",
		},
		{
			name:      "riscv64",
			goarch:    "riscv64",
			assetName: "watchtower_linux_riscv64_9.9.9.tar.gz",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			asset, err := watchtowerAssetForArch(tc.goarch, testVersion)
			if err != nil {
				t.Fatalf("watchtowerAssetForArch: %v", err)
			}
			if asset.name != tc.assetName {
				t.Fatalf("asset name = %q, want %q", asset.name, tc.assetName)
			}
			if asset.url != "https://github.com/nicholas-fedor/watchtower/releases/download/v9.9.9/"+tc.assetName {
				t.Fatalf("asset url = %q", asset.url)
			}
			if asset.checksumURL != "https://github.com/nicholas-fedor/watchtower/releases/download/v9.9.9/checksums.txt" {
				t.Fatalf("checksum url = %q", asset.checksumURL)
			}
		})
	}
}

func TestWatchtowerAssetForArchRejectsUnsupportedArchitecture(t *testing.T) {
	if _, err := watchtowerAssetForArch("ppc64le", "9.9.9"); err == nil {
		t.Fatal("expected unsupported architecture error")
	}
}

func TestParseWatchtowerChecksum(t *testing.T) {
	const expected = "26fe2b21853e52ac662954830223ab6a3cce4e29202299956fbde9e228aff0aa"
	data := []byte(`
d45f47a2c81b113fc42b0d11394a0c21a4fd4934bc6f4fe778e4085c12fc74c5  watchtower_linux_arm64v8_9.9.9.tar.gz
26fe2b21853e52ac662954830223ab6a3cce4e29202299956fbde9e228aff0aa  watchtower_linux_amd64_9.9.9.tar.gz
`)

	got, err := parseWatchtowerChecksum(data, "watchtower_linux_amd64_9.9.9.tar.gz")
	if err != nil {
		t.Fatalf("parseWatchtowerChecksum: %v", err)
	}
	if got != expected {
		t.Fatalf("checksum = %q, want %q", got, expected)
	}
}

func TestParseWatchtowerChecksumRejectsInvalidDigest(t *testing.T) {
	data := []byte("not-a-sha  watchtower_linux_amd64_9.9.9.tar.gz\n")
	if _, err := parseWatchtowerChecksum(data, "watchtower_linux_amd64_9.9.9.tar.gz"); err == nil {
		t.Fatal("expected invalid digest error")
	}
}
