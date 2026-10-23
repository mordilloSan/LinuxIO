package wireguard

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"syscall"

	"golang.zx2c4.com/wireguard/wgctrl"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/hostcmd"
)

var (
	runWGQuickCommand = runWGQuick
	isInterfaceUpFunc = isInterfaceUp
)

func UpInterface(ctx context.Context, req apischema.NameRequest) (any, error) {
	if req.Name == "" {
		slog.Error("invalid up interface request")
		return nil, fmt.Errorf("usage: up_interface <name>")
	}
	name := req.Name
	if err := validateInterfaceName(name); err != nil {
		slog.Error("invalid WireGuard interface name", "interface", name, "error", err)
		return nil, fmt.Errorf("invalid interface name: %w", err)
	}
	out, err := runWGQuickCommand(ctx, "up", name)
	if err != nil {
		slog.Error("failed to bring up WireGuard interface", "interface", name, "error", err, "output", out)
		return nil, fmt.Errorf("bring up interface: %w", err)
	}

	slog.Info("WireGuard interface brought up", "interface", name)
	return map[string]any{
		"status": "on",
		"output": out,
	}, nil
}

func DownInterface(ctx context.Context, req apischema.NameRequest) (any, error) {
	if req.Name == "" {
		slog.Error("invalid down interface request")
		return nil, fmt.Errorf("usage: down_interface <name>")
	}

	name := req.Name
	if err := validateInterfaceName(name); err != nil {
		slog.Error("invalid WireGuard interface name", "interface", name, "error", err)
		return nil, fmt.Errorf("invalid interface name: %w", err)
	}
	out, err := runWGQuickCommand(ctx, "down", name)
	if err != nil {
		slog.Error("failed to bring down WireGuard interface", "interface", name, "error", err, "output", out)
		return nil, fmt.Errorf("bring down interface: %w", err)
	}
	slog.Info("WireGuard interface brought down", "interface", name)
	return map[string]any{
		"status": "off",
		"output": out,
	}, nil
}

func bringUpInterface(ctx context.Context, name string) error {
	out, err := runWGQuickCommand(ctx, "up", name)
	if err != nil {
		slog.Error("failed to bring up WireGuard interface", "interface", name, "error", err, "output", out)
		return fmt.Errorf("bring up interface: %w", err)
	}
	return nil
}

func runWGQuick(ctx context.Context, action, name string) (string, error) {
	wgQuickPath, err := exec.LookPath("wg-quick")
	if err != nil {
		return "", fmt.Errorf("wg-quick not found: %w", err)
	}

	cmd := rootCommand(ctx, wgQuickPath, action, name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), hostcmd.OutputError("wg-quick", []string{action, name}, out, err)
	}
	return string(out), nil
}

func rootCommand(ctx context.Context, path string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: 0, Gid: 0},
	}
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin"}
	cmd.Dir = "/"
	return cmd
}

func isInterfaceUp(name string) bool {
	client, err := wgctrl.New()
	if err != nil {
		slog.Debug("failed to create wgctrl client", "component", "wireguard", "interface", name, "error", err)
		return false
	}
	defer client.Close()

	_, err = client.Device(name)
	return err == nil
}
