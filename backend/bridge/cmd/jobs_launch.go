package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/jobslauncher"
	"github.com/mordilloSan/LinuxIO/backend/common/logging"
)

// runJobsLaunch serves the one connection systemd handed this instance on
// stdin (Accept=yes, StandardInput=socket). It exits 0 even when the launch
// was refused: the reply line carries the outcome, and a non-zero exit would
// mark the unit instance failed for a client error.
func runJobsLaunch(stdin *os.File, stderr io.Writer) int {
	conn, err := net.FileConn(stdin)
	if err != nil {
		fmt.Fprintf(stderr, "linuxio-bridge jobs launch: stdin is not a socket: %v\n", err)
		return 2
	}
	defer func() { _ = conn.Close() }()
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		fmt.Fprintln(stderr, "linuxio-bridge jobs launch: stdin is not a unix socket")
		return 2
	}
	if err := logging.Configure("linuxio-jobs-launcher", false); err != nil {
		fmt.Fprintf(stderr, "linuxio-bridge jobs launch: journald unavailable, logging to stderr: %v\n", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := jobslauncher.Serve(ctx, unixConn, jobslauncher.Deps{}); err != nil {
		slog.Warn("job host launch refused", "error", err)
	}
	return 0
}
