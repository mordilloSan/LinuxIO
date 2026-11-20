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
	linuxioWebserverSocketName     = "linuxio-webserver.socket"
	linuxioAuthSocketName          = "linuxio-auth.socket"
	linuxioBridgeSocketUserService = "linuxio-bridge-socket-user.service"
)

// A full restart stops linuxio-webserver.socket and binds its port again. A
// helper another unit forks in that window, such as a user's job host socket
// at a login, can still hold the old listener for a moment, and the bind fails
// with "Address already in use". systemd ends such a start with the socket's
// Result "resources" (the errno reaches only the journal), so the socket is
// started again on that result, webSocketRetries times, webSocketRetryDelay
// apart. The restart's start and three more stay under the default start
// limit of five starts in 10 s.
const (
	webSocketRetries    = 3
	webSocketRetryDelay = 500 * time.Millisecond
	unitJobPollInterval = 100 * time.Millisecond
)

// The manager calls and waits of a restart, swapped by tests.
var (
	restartUnit     = systemd.RestartUnit
	startUnit       = systemd.StartUnit
	readSocketState = systemd.GetSocketState
	pause           = sleepContext
)

func runSystemctl(parent context.Context, action string, targets []string, successLabel string) int {
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
			err = startUnit(ctx, target)
		case "stop":
			err = systemd.StopUnit(ctx, target)
		case "restart":
			err = restartUnit(ctx, target)
			if err == nil && target == linuxioTargetName {
				err = awaitWebSocket(ctx)
			}
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

// awaitWebSocket waits for the web socket's start in a full restart to finish
// and starts the socket again while it could not listen.
func awaitWebSocket(ctx context.Context) error {
	for retry := 1; ; retry++ {
		state, err := settledSocketState(ctx, linuxioWebserverSocketName)
		if err != nil {
			return fmt.Errorf("wait for %s: %w", linuxioWebserverSocketName, err)
		}
		switch {
		case state.ActiveState == "active":
			return nil
		case state.ActiveState != "failed" || state.Result != "resources":
			return fmt.Errorf("%s is %s (result %q)", linuxioWebserverSocketName, state.ActiveState, state.Result)
		case retry > webSocketRetries:
			return fmt.Errorf("%s could not listen on its address in %d starts (result %q); see journalctl -u %s",
				linuxioWebserverSocketName, retry, state.Result, linuxioWebserverSocketName)
		}
		fmt.Printf("Starting %s again: it could not listen on its address (retry %d of %d)\n",
			linuxioWebserverSocketName, retry, webSocketRetries)
		if err := pause(ctx, webSocketRetryDelay); err != nil {
			return fmt.Errorf("wait to start %s again: %w", linuxioWebserverSocketName, err)
		}
		if err := startUnit(ctx, linuxioWebserverSocketName); err != nil {
			return err
		}
	}
}

// settledSocketState reads the socket once no job is pending for it.
func settledSocketState(ctx context.Context, name string) (systemd.SocketState, error) {
	for {
		state, err := readSocketState(ctx, name)
		if err != nil || !state.JobPending {
			return state, err
		}
		if err := pause(ctx, unitJobPollInterval); err != nil {
			return state, err
		}
	}
}

// sleepContext waits d, or until ctx is done.
func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
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

	return runSystemctl(ctx, "restart", targets, successLabel)
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
