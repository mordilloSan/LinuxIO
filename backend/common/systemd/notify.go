package systemd

import (
	"fmt"
	"net"
	"os"
)

// Notify sends an sd_notify state string to NOTIFY_SOCKET. It does nothing
// when the variable is unset, so it is safe outside systemd.
func Notify(state string) error {
	name := os.Getenv("NOTIFY_SOCKET")
	if name == "" {
		return nil
	}
	if name[0] == '@' { // abstract socket
		name = "\x00" + name[1:]
	}
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: name, Net: "unixgram"})
	if err != nil {
		return fmt.Errorf("dial notify socket: %w", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(state)); err != nil {
		return fmt.Errorf("write notify socket: %w", err)
	}
	return nil
}
