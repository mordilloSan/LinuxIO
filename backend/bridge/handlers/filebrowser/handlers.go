package filebrowser

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/mordilloSan/go_logger/v2/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

// RegisterHandlers registers all filebrowser handlers with the global registry
func RegisterHandlers() {
	// Simple JSON handlers
	ipc.RegisterFunc("filebrowser", "resource_get", func(ctx context.Context, args []string, emit ipc.Events) error {
		result, err := resourceGet(args)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("filebrowser", "resource_stat", func(ctx context.Context, args []string, emit ipc.Events) error {
		result, err := resourceStat(args)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("filebrowser", "resource_delete", func(ctx context.Context, args []string, emit ipc.Events) error {
		result, err := resourceDelete(args)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("filebrowser", "resource_post", func(ctx context.Context, args []string, emit ipc.Events) error {
		result, err := resourcePost(args)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("filebrowser", "resource_patch", func(ctx context.Context, args []string, emit ipc.Events) error {
		result, err := resourcePatch(args)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("filebrowser", "dir_size", func(ctx context.Context, args []string, emit ipc.Events) error {
		result, err := dirSize(args)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("filebrowser", "subfolders", func(ctx context.Context, args []string, emit ipc.Events) error {
		result, err := subfolders(args)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("filebrowser", "search", func(ctx context.Context, args []string, emit ipc.Events) error {
		result, err := searchFiles(args)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("filebrowser", "indexer_status", func(ctx context.Context, args []string, emit ipc.Events) error {
		result, err := indexerStatus(args)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("filebrowser", "chmod", func(ctx context.Context, args []string, emit ipc.Events) error {
		result, err := resourceChmod(args)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("filebrowser", "users_groups", func(ctx context.Context, args []string, emit ipc.Events) error {
		result, err := usersGroups()
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("filebrowser", "file_update_from_temp", func(ctx context.Context, args []string, emit ipc.Events) error {
		result, err := fileUpdateFromTemp(args)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	// Streaming handlers with progress

	// Download - streams file data with progress updates
	ipc.RegisterFunc("filebrowser", "download", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}

		path := args[0]
		realPath := filepath.Clean(path)

		// Stat the file
		stat, err := os.Stat(realPath)
		if err != nil {
			return fmt.Errorf("file not found: %w", err)
		}

		if stat.IsDir() {
			return fmt.Errorf("path is a directory, use archive instead")
		}

		totalSize := stat.Size()

		// Send initial progress
		_ = emit.Progress(FileProgress{
			Total: totalSize,
			Phase: "starting",
		})

		// Open file
		file, err := os.Open(realPath)
		if err != nil {
			return fmt.Errorf("cannot open file: %w", err)
		}
		defer file.Close()

		// Stream chunks
		buf := make([]byte, chunkSize)
		var bytesRead int64
		var lastProgress int64

		for {
			n, readErr := file.Read(buf)
			if n > 0 {
				// Send data chunk
				if err := emit.Data(buf[:n]); err != nil {
					return fmt.Errorf("write data chunk: %w", err)
				}

				bytesRead += int64(n)

				// Send progress periodically
				if bytesRead-lastProgress >= progressIntervalDownload || bytesRead == totalSize {
					pct := 0
					if totalSize > 0 {
						pct = int(bytesRead * 100 / totalSize)
					}
					_ = emit.Progress(FileProgress{
						Bytes: bytesRead,
						Total: totalSize,
						Pct:   pct,
					})
					lastProgress = bytesRead
				}
			}

			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				return fmt.Errorf("read file: %w", readErr)
			}

			// Check context cancellation
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}

		// Send result
		logger.Infof("[FBHandler] Download complete: path=%s size=%d", path, totalSize)
		return emit.Result(map[string]any{
			"path":     path,
			"size":     totalSize,
			"fileName": filepath.Base(realPath),
		})
	})

	// Upload - bidirectional handler (receives data from client)
	ipc.Register("filebrowser", "upload", &uploadHandler{})

	// Archive download - creates and streams a tar.gz
	ipc.RegisterFunc("filebrowser", "archive", func(ctx context.Context, args []string, emit ipc.Events) error {
		// Implementation will call existing handleArchiveDownload logic
		return fmt.Errorf("archive download not yet migrated")
	})

	// Compress - creates archive from multiple paths
	ipc.RegisterFunc("filebrowser", "compress", func(ctx context.Context, args []string, emit ipc.Events) error {
		// Implementation will call existing handleCompress logic
		return fmt.Errorf("compress not yet migrated")
	})

	// Extract - extracts archive to destination
	ipc.RegisterFunc("filebrowser", "extract", func(ctx context.Context, args []string, emit ipc.Events) error {
		// Implementation will call existing handleExtract logic
		return fmt.Errorf("extract not yet migrated")
	})
}

// uploadHandler implements BidirectionalHandler for file uploads
type uploadHandler struct{}

func (h *uploadHandler) Execute(ctx context.Context, args []string, emit ipc.Events) error {
	return fmt.Errorf("upload requires bidirectional stream")
}

func (h *uploadHandler) ExecuteWithInput(ctx context.Context, args []string, emit ipc.Events, input <-chan []byte) error {
	if len(args) < 2 {
		return fmt.Errorf("missing path or size")
	}

	path := args[0]
	sizeStr := args[1]
	expectedSize, err := strconv.ParseInt(sizeStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid size: %w", err)
	}

	realPath := filepath.Clean(path)

	// Check if file exists to preserve permissions
	existingStat, existsErr := os.Stat(realPath)
	var preserveMode os.FileMode
	var preserveUID, preserveGID int
	if existsErr == nil {
		preserveMode = existingStat.Mode()
		if sysStat, ok := existingStat.Sys().(*syscall.Stat_t); ok {
			preserveUID = int(sysStat.Uid)
			preserveGID = int(sysStat.Gid)
		}
	}

	// Create temp file
	tempPath := realPath + ".upload.tmp"
	file, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("cannot create temp file: %w", err)
	}
	defer func() {
		file.Close()
		os.Remove(tempPath)
	}()

	// Send initial progress
	_ = emit.Progress(FileProgress{
		Total: expectedSize,
		Phase: "receiving",
	})

	// Receive data chunks
	var bytesWritten int64
	var lastProgress int64

	for chunk := range input {
		n, err := file.Write(chunk)
		if err != nil {
			return fmt.Errorf("write error: %w", err)
		}

		bytesWritten += int64(n)

		// Send progress periodically
		if bytesWritten-lastProgress >= progressIntervalUpload || bytesWritten == expectedSize {
			pct := 0
			if expectedSize > 0 {
				pct = int(bytesWritten * 100 / expectedSize)
			}
			_ = emit.Progress(FileProgress{
				Bytes: bytesWritten,
				Total: expectedSize,
				Pct:   pct,
			})
			lastProgress = bytesWritten
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}

	// Close temp file before rename
	file.Close()

	// Verify size
	if bytesWritten != expectedSize {
		return fmt.Errorf("size mismatch: expected %d, got %d", expectedSize, bytesWritten)
	}

	// Rename temp to final
	if err := os.Rename(tempPath, realPath); err != nil {
		return fmt.Errorf("rename failed: %w", err)
	}

	// Restore permissions if file existed
	if existsErr == nil {
		_ = os.Chmod(realPath, preserveMode)
		_ = os.Chown(realPath, preserveUID, preserveGID)
	}

	logger.Infof("[FBHandler] Upload complete: path=%s size=%d", path, bytesWritten)
	return emit.Result(map[string]any{
		"path": path,
		"size": bytesWritten,
	})
}
