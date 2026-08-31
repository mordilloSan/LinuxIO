package indexer

import (
	"context"
	"errors"
	"fmt"
	"os"

	systemdapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/systemd"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
	"github.com/mordilloSan/LinuxIO/backend/indexer/systemdunit"
)

var (
	tcpSocketUnitPath    = systemdunit.TCPSocketUnitPath
	writeTCPSocketUnit   = utils.WriteFileAtomic
	enableTCPSocketUnit  = systemdapi.EnableUnit
	disableTCPSocketUnit = systemdapi.DisableUnit
	stopTCPSocketUnit    = systemdapi.StopUnit
	restartTCPSocketUnit = systemdapi.RestartUnit
	reloadTCPSystemd     = systemdapi.DaemonReload
	configureTCPListener = ConfigureTCPListener
)

func ConfigureTCPListener(ctx context.Context, listenAddr string) error {
	if listenAddr == "" {
		if _, err := os.Stat(tcpSocketUnitPath); errors.Is(err, os.ErrNotExist) {
			return nil
		} else if err != nil {
			return fmt.Errorf("stat indexer TCP socket unit: %w", err)
		}
		if err := disableTCPSocketUnit(ctx, systemdunit.TCPSocketUnitName); err != nil {
			return err
		}
		if err := stopTCPSocketUnit(ctx, systemdunit.TCPSocketUnitName); err != nil {
			return err
		}
		if err := os.Remove(tcpSocketUnitPath); err != nil {
			return fmt.Errorf("remove indexer TCP socket unit: %w", err)
		}
		return reloadTCPSystemd(ctx)
	}

	unit, err := systemdunit.TCPListenerUnit(listenAddr)
	if err != nil {
		return err
	}
	if err := writeTCPSocketUnit(tcpSocketUnitPath, unit, 0o644); err != nil {
		return fmt.Errorf("write indexer TCP socket unit: %w", err)
	}
	if err := enableTCPSocketUnit(ctx, systemdunit.TCPSocketUnitName); err != nil {
		return err
	}
	return restartTCPSocketUnit(ctx, systemdunit.TCPSocketUnitName)
}
