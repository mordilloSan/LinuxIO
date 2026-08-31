package systemdunit

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"unicode"
)

const (
	TCPSocketUnitName = "linuxio-indexer-tcp.socket"
	TCPSocketUnitPath = "/etc/systemd/system/" + TCPSocketUnitName
)

func NormalizeTCPListenAddress(raw string) (string, error) {
	addr := strings.TrimSpace(raw)
	if addr == "" {
		return "", nil
	}
	if strings.IndexFunc(addr, unicode.IsSpace) >= 0 {
		return "", fmt.Errorf("invalid listen_addr %q: whitespace is not allowed", raw)
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("invalid listen_addr %q: use host:port or :port: %w", raw, err)
	}
	for _, char := range host {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || strings.ContainsRune(".-_:%", char)) {
			return "", fmt.Errorf("invalid listen_addr %q: host contains unsupported characters", raw)
		}
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return "", fmt.Errorf("invalid listen_addr %q: port must be between 1 and 65535", raw)
	}
	return addr, nil
}

func TCPListenerUnit(raw string) ([]byte, error) {
	addr, err := NormalizeTCPListenAddress(raw)
	if err != nil {
		return nil, err
	}
	if addr == "" {
		return nil, fmt.Errorf("listen_addr cannot be empty")
	}
	return fmt.Appendf(nil, `[Unit]
Description=LinuxIO Indexer Read-Only TCP API Socket
Documentation=https://github.com/mordilloSan/LinuxIO
PartOf=linuxio.target
Requires=linuxio-webserver.socket
After=linuxio-webserver.socket
BindsTo=linuxio-webserver.socket

[Socket]
ListenStream=%s
Accept=no
Service=linuxio-indexer.service
FileDescriptorName=indexer-tcp
FlushPending=true

[Install]
WantedBy=linuxio.target linuxio-webserver.socket
`, addr), nil
}
