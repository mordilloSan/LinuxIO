package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

const (
	linuxioTargetName              = "linuxio.target"
	linuxioWebserverServiceName    = "linuxio-webserver.service"
	linuxioAuthSocketName          = "linuxio-auth.socket"
	linuxioBridgeSocketUserService = "linuxio-bridge-socket-user.service"
)

func runSystemctl(ctx context.Context, action, target string) int {
	return runSystemctlTargets(ctx, action, []string{target}, target)
}

func runSystemctlTargets(parent context.Context, action string, targets []string, successLabel string) int {
	if len(targets) == 0 {
		fmt.Fprintf(os.Stderr, "No targets provided for systemctl %s\n", action)
		return 1
	}

	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	for _, target := range targets {
		var err error
		switch action {
		case "start":
			err = systemd.StartUnit(ctx, target)
		case "stop":
			err = systemd.StopUnit(ctx, target)
		case "restart":
			err = systemd.RestartUnit(ctx, target)
		case "enable":
			err = systemd.EnableUnit(ctx, target)
		case "disable":
			err = systemd.DisableUnit(ctx, target)
		default:
			fmt.Fprintf(os.Stderr, "Unknown action: %s\n", action)
			return 1
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to %s %s: %v\n", action, target, err)
			return 1
		}
	}

	if successLabel == "" {
		successLabel = strings.Join(targets, " ")
	}
	fmt.Printf("Successfully %s %s\n", pastTense(action), successLabel)
	return 0
}

func pastTense(action string) string {
	switch action {
	case "start":
		return "started"
	case "stop":
		return "stopped"
	case "restart":
		return "restarted"
	case "enable":
		return "enabled"
	case "disable":
		return "disabled"
	default:
		return action + "ed"
	}
}

func runRestart(ctx context.Context, args []string) int {
	targets, successLabel, err := restartTargets(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		fmt.Fprintln(os.Stderr, "Usage: linuxio restart [--full]")
		return 1
	}

	return runSystemctlTargets(ctx, "restart", targets, successLabel)
}

func restartTargets(args []string) ([]string, string, error) {
	if len(args) == 0 {
		return []string{
			linuxioBridgeSocketUserService,
			linuxioAuthSocketName,
			linuxioWebserverServiceName,
		}, "LinuxIO control plane", nil
	}

	if len(args) == 1 {
		switch args[0] {
		case "--full", "full":
			return []string{linuxioTargetName}, linuxioTargetName, nil
		}
	}

	return nil, "", fmt.Errorf("unknown restart option: %s", strings.Join(args, " "))
}
