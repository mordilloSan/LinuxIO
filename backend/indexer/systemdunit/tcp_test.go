package systemdunit

import (
	"strings"
	"testing"
)

func TestTCPListenerUnit(t *testing.T) {
	unit, err := TCPListenerUnit(" [::1]:8080 ")
	if err != nil {
		t.Fatalf("TCPListenerUnit: %v", err)
	}
	content := string(unit)
	if !strings.Contains(content, "ListenStream=[::1]:8080\n") ||
		!strings.Contains(content, "WantedBy=linuxio.target linuxio-webserver.socket\n") {
		t.Fatalf("unexpected unit:\n%s", content)
	}

	for _, addr := range []string{"8080", ":0", ":65536", ":http", "foo/bar:8080", ":8080\n[Service]"} {
		if _, err := TCPListenerUnit(addr); err == nil {
			t.Fatalf("TCPListenerUnit accepted %q", addr)
		}
	}
}
