package virt

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	libvirt "github.com/digitalocean/go-libvirt"
	"libvirt.org/go/libvirtxml"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	ipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

const consoleSocketTimeout = 10 * time.Second

type consoleEndpoint struct {
	Network string
	Address string
}

func HandleConsoleSession(ctx context.Context, stream net.Conn, req apischema.NameRequest) error {
	if err := validateVMName(req.Name); err != nil {
		writeConsoleStreamError(stream, err)
		return err
	}
	endpoint, err := resolveConsoleEndpoint(ctx, req.Name)
	if err != nil {
		writeConsoleStreamError(stream, err)
		return err
	}
	dialer := net.Dialer{Timeout: 3 * time.Second}
	vnc, err := dialer.DialContext(ctx, endpoint.Network, endpoint.Address)
	if err != nil {
		err = fmt.Errorf("connect VNC console: %w", err)
		writeConsoleStreamError(stream, err)
		return err
	}
	defer vnc.Close()

	var wg sync.WaitGroup
	var closeOnce sync.Once
	closeRelays := func() {
		closeOnce.Do(func() {
			_ = vnc.Close()
			_ = stream.Close()
		})
	}
	wg.Go(func() {
		defer closeRelays()
		relayConnToStream(vnc, stream)
	})
	wg.Go(func() {
		defer closeRelays()
		relayStreamToConn(stream, vnc)
	})
	wg.Wait()
	return nil
}

func writeConsoleStreamError(stream net.Conn, err error) {
	if writeErr := ipc.WriteResultErrorAndClose(stream, 1, err.Error(), errorCode(err, 500)); writeErr != nil {
		slog.Debug("failed to write VNC stream error frame", "error", writeErr)
	}
}

func resolveConsoleEndpoint(ctx context.Context, name string) (consoleEndpoint, error) {
	deadline := time.Now().Add(consoleSocketTimeout)
	var lastErr error
	for {
		endpoint, err := lookupConsoleEndpoint(ctx, name)
		if err == nil {
			return endpoint, nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return consoleEndpoint{}, fmt.Errorf("wait for VNC endpoint: %w", lastErr)
		}
		select {
		case <-ctx.Done():
			return consoleEndpoint{}, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func lookupConsoleEndpoint(ctx context.Context, name string) (consoleEndpoint, error) {
	var endpoint consoleEndpoint
	connErr := withLibvirtConn(ctx, func(conn libvirtConn) error {
		resolved, resolveErr := consoleEndpointFromDomain(conn, name)
		if resolveErr != nil {
			return resolveErr
		}
		endpoint = resolved
		return nil
	})
	return endpoint, connErr
}

func consoleEndpointFromDomain(conn libvirtConn, name string) (consoleEndpoint, error) {
	domain, lookupErr := lookupDomain(conn, name)
	if lookupErr != nil {
		return consoleEndpoint{}, lookupErr
	}
	state, _, stateErr := conn.DomainGetState(domain, 0)
	if stateErr != nil {
		return consoleEndpoint{}, stateErr
	}
	if libvirt.DomainState(state) != libvirt.DomainRunning {
		return consoleEndpoint{}, fmt.Errorf("VM %q is not running", name)
	}
	xmlDoc, xmlErr := conn.DomainGetXMLDesc(domain, 0)
	if xmlErr != nil {
		return consoleEndpoint{}, xmlErr
	}
	return validateConsoleEndpoint(name, vncEndpointFromDomainXML(xmlDoc))
}

func validateConsoleEndpoint(name string, endpoint consoleEndpoint) (consoleEndpoint, error) {
	switch endpoint.Network {
	case "":
		return consoleEndpoint{}, fmt.Errorf("VM %q has no VNC endpoint", name)
	case "unix":
		return validateConsoleUnixEndpoint(endpoint)
	case "tcp":
		return validateConsoleTCPEndpoint(endpoint)
	default:
		return consoleEndpoint{}, fmt.Errorf("unsupported VNC endpoint network %q", endpoint.Network)
	}
}

func validateConsoleUnixEndpoint(endpoint consoleEndpoint) (consoleEndpoint, error) {
	if !isSafeVNCSocket(endpoint.Address) {
		return consoleEndpoint{}, fmt.Errorf("unsafe VNC socket path %q", endpoint.Address)
	}
	info, statErr := os.Stat(endpoint.Address)
	if statErr != nil {
		return consoleEndpoint{}, statErr
	}
	if info.Mode()&os.ModeSocket == 0 {
		return consoleEndpoint{}, fmt.Errorf("VNC path %q is not a unix socket", endpoint.Address)
	}
	return endpoint, nil
}

func validateConsoleTCPEndpoint(endpoint consoleEndpoint) (consoleEndpoint, error) {
	host, port, splitErr := net.SplitHostPort(endpoint.Address)
	if splitErr != nil {
		return consoleEndpoint{}, splitErr
	}
	portNumber, parseErr := strconv.Atoi(port)
	if parseErr != nil || !isVNCTCPPort(portNumber) {
		return consoleEndpoint{}, fmt.Errorf("unsafe VNC TCP port %q", port)
	}
	dialHost, ok := safeVNCTCPDialHost(host)
	if !ok {
		return consoleEndpoint{}, fmt.Errorf("unsafe VNC TCP listen address %q", host)
	}
	endpoint.Address = net.JoinHostPort(dialHost, port)
	return endpoint, nil
}

func vncEndpointFromDomainXML(xmlDoc string) consoleEndpoint {
	var domain libvirtxml.Domain
	if err := domain.Unmarshal(xmlDoc); err != nil || domain.Devices == nil {
		return consoleEndpoint{}
	}
	for _, graphic := range domain.Devices.Graphics {
		if graphic.VNC == nil {
			continue
		}
		if socket := vncGraphicSocket(graphic.VNC); socket != "" {
			return consoleEndpoint{Network: "unix", Address: socket}
		}
		if endpoint := vncGraphicTCPEndpoint(graphic.VNC); endpoint.Network != "" {
			return endpoint
		}
	}
	return consoleEndpoint{}
}

// vncGraphicSocket returns the unix socket path for a VNC graphics device.
// libvirt normalizes a `socket=` attribute into a `<listen type='socket'>`
// child element when the domain is defined and started, so the running XML
// usually carries the path on the listener rather than the attribute.
func vncGraphicSocket(vnc *libvirtxml.DomainGraphicVNC) string {
	if vnc.Socket != "" {
		return vnc.Socket
	}
	for _, listener := range vnc.Listeners {
		if listener.Socket != nil && listener.Socket.Socket != "" {
			return listener.Socket.Socket
		}
	}
	return ""
}

func vncGraphicTCPEndpoint(vnc *libvirtxml.DomainGraphicVNC) consoleEndpoint {
	if !isVNCTCPPort(vnc.Port) {
		return consoleEndpoint{}
	}
	host := vncGraphicTCPListen(vnc)
	if host == "" {
		host = "127.0.0.1"
	}
	return consoleEndpoint{
		Network: "tcp",
		Address: net.JoinHostPort(host, strconv.Itoa(vnc.Port)),
	}
}

func vncGraphicTCPListen(vnc *libvirtxml.DomainGraphicVNC) string {
	if vnc.Listen != "" {
		return vnc.Listen
	}
	for _, listener := range vnc.Listeners {
		if listener.Address != nil && listener.Address.Address != "" {
			return listener.Address.Address
		}
		if listener.Network != nil && listener.Network.Address != "" {
			return listener.Network.Address
		}
	}
	return ""
}

func isVNCTCPPort(port int) bool {
	return port >= 5900 && port <= 65535
}

func safeVNCTCPDialHost(host string) (string, bool) {
	normalized := strings.Trim(strings.ToLower(host), "[]")
	switch normalized {
	case "", "localhost", "127.0.0.1", "::1":
		if normalized == "" {
			return "127.0.0.1", true
		}
		return normalized, true
	case "0.0.0.0":
		return "127.0.0.1", true
	case "::":
		return "::1", true
	}
	ip := net.ParseIP(normalized)
	if ip == nil {
		return "", false
	}
	if ip.IsLoopback() {
		return normalized, true
	}
	if ip.IsUnspecified() {
		if ip.To4() != nil {
			return "127.0.0.1", true
		}
		return "::1", true
	}
	return "", false
}

func relayConnToStream(src net.Conn, dst net.Conn) {
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if writeErr := ipc.WriteRelayFrame(dst, &ipc.StreamFrame{
				Opcode:   ipc.OpStreamData,
				StreamID: 1,
				Payload:  buf[:n],
			}); writeErr != nil {
				return
			}
		}
		if err != nil {
			if closeErr := ipc.WriteStreamClose(dst, 1); closeErr != nil {
				slog.Debug("failed to write VNC stream close frame", "error", closeErr)
			}
			return
		}
	}
}

func relayStreamToConn(src net.Conn, dst net.Conn) {
	for {
		frame, err := ipc.ReadRelayFrame(src)
		if err != nil {
			return
		}
		switch frame.Opcode {
		case ipc.OpStreamData:
			if len(frame.Payload) > 0 {
				if _, err := dst.Write(frame.Payload); err != nil {
					return
				}
			}
		case ipc.OpStreamClose, ipc.OpStreamAbort:
			return
		}
	}
}
