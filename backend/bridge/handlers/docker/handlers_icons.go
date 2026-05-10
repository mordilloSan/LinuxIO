package docker

import (
	"context"
	"encoding/base64"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/internal/rpc"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

func (h dockerHandlers) handleGetIconURI(ctx context.Context, args []string, emit ipc.Events) error {
	identifier, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	uri, err := GetIconURI(identifier)
	return rpc.EmitResult(emit, map[string]string{"uri": uri}, err)
}

func (h dockerHandlers) handleGetIcon(ctx context.Context, args []string, emit ipc.Events) error {
	identifier, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	data, err := GetIcon(identifier)
	if err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	return rpc.EmitResult(emit, map[string]string{"data": encoded}, nil)
}

func (h dockerHandlers) handleGetIconInfo(ctx context.Context, args []string, emit ipc.Events) error {
	identifier, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	return rpc.EmitResult(emit, GetIconInfo(identifier), nil)
}

func (h dockerHandlers) handleClearIconCache(ctx context.Context, args []string, emit ipc.Events) error {
	slog.Info("clear_icon_cache requested", "component", "docker")
	if err := ClearIconCache(); err != nil {
		return err
	}
	return rpc.EmitResult(emit, map[string]string{"message": "Icon cache cleared successfully"}, nil)
}
