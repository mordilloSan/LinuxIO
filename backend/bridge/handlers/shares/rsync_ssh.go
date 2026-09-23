package shares

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

func handleGetRsyncSSH(ctx context.Context, req apischema.RsyncSSHRequest) (apischema.RsyncSSHStatus, error) {
	if req.Port < 1 || req.Port > 65535 {
		return apischema.RsyncSSHStatus{}, errors.New("SSH port must be between 1 and 65535")
	}
	err := probeRsyncSSH(ctx, req.Port)
	if ctx.Err() != nil {
		return apischema.RsyncSSHStatus{}, ctx.Err()
	}
	status := apischema.RsyncSSHStatus{Available: err == nil, Port: req.Port}
	if err != nil {
		status.Error = utils.OptionalString(fmt.Sprintf("SSH did not respond on local port %d. Check the SSH service and its listening address in Services. This check does not test access from the NAS.", req.Port))
	}
	return status, nil
}

func probeRsyncSSH(ctx context.Context, port int) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort("localhost", strconv.Itoa(port)))
	if err != nil {
		return fmt.Errorf("connect to local SSH: %w", err)
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	if err = conn.SetReadDeadline(deadline); err != nil {
		return err
	}
	return readSSHIdentification(conn)
}

func readSSHIdentification(reader io.Reader) error {
	// SSH permits informational lines before its identification. Bound both
	// their count and length so an unrelated listener cannot exhaust memory.
	buffer := bufio.NewReaderSize(reader, 256)
	for range 50 {
		line, err := buffer.ReadSlice('\n')
		if err != nil {
			return fmt.Errorf("read SSH identification: %w", err)
		}
		if strings.HasPrefix(string(line), "SSH-2.0-") || strings.HasPrefix(string(line), "SSH-1.99-") {
			return nil
		}
		if strings.HasPrefix(string(line), "SSH-") || strings.HasPrefix(string(line), "@RSYNCD:") {
			return errors.New("listener does not support SSH 2")
		}
	}
	return errors.New("SSH identification was not received")
}
