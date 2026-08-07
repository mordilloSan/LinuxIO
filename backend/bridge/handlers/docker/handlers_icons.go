package docker

import (
	"context"
	"encoding/base64"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func (h dockerHandlers) handleGetIconURI(ctx context.Context, req apischema.IdentifierRequest) (apischema.DockerIconURIResponse, error) {
	uri, err := GetIconURI(ctx, req.Identifier)
	return apischema.DockerIconURIResponse{URI: uri}, err
}

func (h dockerHandlers) handleGetIcon(ctx context.Context, req apischema.IdentifierRequest) (apischema.DockerIconDataResponse, error) {
	data, err := GetIcon(ctx, req.Identifier)
	if err != nil {
		return apischema.DockerIconDataResponse{}, err
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	return apischema.DockerIconDataResponse{Data: encoded}, nil
}

func (h dockerHandlers) handleGetIconInfo(ctx context.Context, req apischema.IdentifierRequest) (apischema.DockerIconInfoResponse, error) {
	info := GetIconInfo(ctx, req.Identifier)
	return apischema.DockerIconInfoResponse{Type: string(info.Type), Identifier: info.Identifier, Cached: info.Cached}, nil
}

func (h dockerHandlers) handleClearIconCache(ctx context.Context, _ apischema.NoRequest) (apischema.MessageResponse, error) {
	if err := ClearIconCache(ctx); err != nil {
		return apischema.MessageResponse{}, err
	}
	return apischema.MessageResponse{Message: "Icon cache cleared successfully"}, nil
}
