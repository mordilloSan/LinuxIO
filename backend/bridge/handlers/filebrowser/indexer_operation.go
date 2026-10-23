package filebrowser

import (
	"context"
	"errors"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/indexer"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

func runIndexerTask(ctx context.Context, task *bridgeipc.Task, req apischema.OptionalPathRequest) (indexer.IndexerResult, error) {
	path := "/"
	if req.Path != nil && *req.Path != "" {
		path = fsroot.CleanAbsPath(*req.Path)
	}
	return runIndexerOperation(ctx, task, path, false)
}

func runIndexerOperation(ctx context.Context, task *bridgeipc.Task, path string, attachOnly bool) (indexer.IndexerResult, error) {
	var result indexer.IndexerResult
	var hasResult bool
	var taskErr *bridgeipc.Error
	cb := indexer.IndexerCallbacks{
		OnProgress: func(p indexer.IndexerProgress) error {
			task.ReportProgress(p)
			return nil
		},
		OnResult: func(r indexer.IndexerResult) error {
			result = r
			hasResult = true
			return nil
		},
		OnError: func(msg string, code int) error {
			taskErr = bridgeipc.NewError(msg, code)
			return nil
		},
	}

	var err error
	if attachOnly {
		err = indexer.StreamIndexerAttach(ctx, path, cb)
	} else {
		err = indexer.StreamIndexer(ctx, path, cb)
		if err != nil && taskErr != nil && taskErr.Code == 409 {
			taskErr = nil
			err = indexer.StreamIndexerAttach(ctx, path, cb)
		}
	}
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, indexer.ErrAborted) {
			return indexer.IndexerResult{}, context.Canceled
		}
		if taskErr != nil {
			return indexer.IndexerResult{}, taskErr
		}
		return indexer.IndexerResult{}, err
	}

	if !hasResult {
		return indexer.IndexerResult{}, nil
	}
	return result, nil
}
