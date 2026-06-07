package docker

import (
	"context"
	"encoding/base64"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func (h dockerHandlers) handleGetIconURI(ctx context.Context, req apischema.IdentifierRequest, emit bridgeipc.Events) error {
	uri, err := GetIconURI(ctx, req.Identifier)
	return bridgeipc.EmitResult(emit, map[string]string{"uri": uri}, err)
}

func (h dockerHandlers) handleGetIcon(ctx context.Context, req apischema.IdentifierRequest, emit bridgeipc.Events) error {
	data, err := GetIcon(ctx, req.Identifier)
	if err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	return bridgeipc.EmitResult(emit, map[string]string{"data": encoded}, nil)
}

func (h dockerHandlers) handleGetIconInfo(ctx context.Context, req apischema.IdentifierRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, GetIconInfo(ctx, req.Identifier), nil)
}

func (h dockerHandlers) handleClearIconCache(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	if err := ClearIconCache(ctx); err != nil {
		return err
	}
	return bridgeipc.EmitResult(emit, map[string]string{"message": "Icon cache cleared successfully"}, nil)
}
