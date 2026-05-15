package docker

import (
	"context"
	"encoding/base64"

	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func (h dockerHandlers) handleGetIconURI(ctx context.Context, args []string, emit bridgeipc.Events) error {
	identifier, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	uri, err := GetIconURI(ctx, identifier)
	return bridgeipc.EmitResult(emit, map[string]string{"uri": uri}, err)
}

func (h dockerHandlers) handleGetIcon(ctx context.Context, args []string, emit bridgeipc.Events) error {
	identifier, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	data, err := GetIcon(ctx, identifier)
	if err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	return bridgeipc.EmitResult(emit, map[string]string{"data": encoded}, nil)
}

func (h dockerHandlers) handleGetIconInfo(ctx context.Context, args []string, emit bridgeipc.Events) error {
	identifier, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	return bridgeipc.EmitResult(emit, GetIconInfo(ctx, identifier), nil)
}

func (h dockerHandlers) handleClearIconCache(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if err := ClearIconCache(ctx); err != nil {
		return err
	}
	return bridgeipc.EmitResult(emit, map[string]string{"message": "Icon cache cleared successfully"}, nil)
}
