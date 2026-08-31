package docker

import (
	"context"
	"log/slog"
	"os"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/indexer"
)

func notifyIndexerForComposeFile(ctx context.Context, path string) {
	info, err := os.Stat(path)
	if err != nil {
		slog.Debug("failed to stat compose file for indexer update", "path", path, "error", err)
		return
	}
	if err := indexer.Add(ctx, indexer.EntryFromFileInfo(path, info, -1)); err != nil {
		slog.Debug("failed to update compose file in indexer", "path", path, "error", err)
	}
}

func notifyIndexerForDeletedComposePath(ctx context.Context, path string) {
	if err := indexer.Delete(ctx, path); err != nil {
		slog.Debug("failed to delete compose path from indexer", "path", path, "error", err)
	}
}
