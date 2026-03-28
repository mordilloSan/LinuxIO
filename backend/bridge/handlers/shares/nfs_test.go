package shares

import "testing"

func TestParseKernelNFSClientInfo(t *testing.T) {
	raw := `clientid: 0x3a86ed2469c2322e
address: "192.168.1.249:773"
status: confirmed
seconds from last renew: 23
name: "Linux NFSv4.2 Tower"
minor version: 2
callback state: UP
callback address: 192.168.1.249:0
`

	client, err := parseKernelNFSClientInfo(raw)
	if err != nil {
		t.Fatalf("parseKernelNFSClientInfo returned error: %v", err)
	}

	if client.IP != "192.168.1.249" {
		t.Fatalf("unexpected IP: %q", client.IP)
	}
	if client.Name != "Linux NFSv4.2 Tower" {
		t.Fatalf("unexpected name: %q", client.Name)
	}
	if client.Status != "confirmed" {
		t.Fatalf("unexpected status: %q", client.Status)
	}
	if client.SecondsFromLastRenew != 23 {
		t.Fatalf("unexpected secondsFromLastRenew: %d", client.SecondsFromLastRenew)
	}
	if client.MinorVersion != 2 {
		t.Fatalf("unexpected minorVersion: %d", client.MinorVersion)
	}
}

func TestNormalizeNFSClientAddress(t *testing.T) {
	testCases := []struct {
		input string
		want  string
	}{
		{input: `"192.168.1.249:773"`, want: "192.168.1.249"},
		{input: `"192.168.1.249"`, want: "192.168.1.249"},
		{input: `"[2001:db8::10]:2049"`, want: "2001:db8::10"},
	}

	for _, tc := range testCases {
		if got := normalizeNFSClientAddress(tc.input); got != tc.want {
			t.Fatalf("normalizeNFSClientAddress(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
