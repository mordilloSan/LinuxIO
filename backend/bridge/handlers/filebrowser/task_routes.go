package filebrowser

import (
	"context"
	"fmt"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/indexer"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
)

const (
	routeArchive     = "filebrowser.archive"
	routeUpload      = "filebrowser.upload"
	routeUploadBatch = "filebrowser.upload_batch"
)

var fileTaskRoutes = fileTaskBindings(nil, uploadOwner{}).Routes()

func transferResult[T any](value any, err error) (T, error) {
	if err != nil {
		var zero T
		return zero, err
	}
	result, ok := value.(T)
	if !ok {
		var zero T
		return zero, fmt.Errorf("unexpected transfer result %T", value)
	}
	return result, nil
}

func fileTaskBindings(store *userconfig.UserStore, owner uploadOwner) apischema.BindingSet {
	return apischema.Bindings(
		apischema.TaskRunner[apischema.FileCompressRequest, FileCompressResult]("filebrowser.compress", apischema.SessionTask(), apischema.WithTaskProgress[FileProgress](), apischema.WithTaskMetadata(func(req apischema.FileCompressRequest) bridgeipc.TaskMetadata {
			return bridgeipc.TaskMetadata{Identity: []string{req.TargetPath}, Label: req.TargetPath, Path: req.TargetPath}
		})).Run(
			func(ctx context.Context, task *bridgeipc.Task, req apischema.FileCompressRequest) (FileCompressResult, error) {
				return runCompressTask(ctx, task, store, req)
			},
			bridgeipc.TaskDefault,
		),
		apischema.TaskRunner[apischema.FileExtractRequest, FileExtractResult]("filebrowser.extract", apischema.SessionTask(), apischema.WithTaskProgress[FileProgress](), apischema.WithTaskMetadata(func(req apischema.FileExtractRequest) bridgeipc.TaskMetadata {
			identity := []string{req.ArchivePath}
			if req.Destination != nil {
				identity = append(identity, *req.Destination)
			}
			return bridgeipc.TaskMetadata{Identity: identity, Label: req.ArchivePath, Path: req.ArchivePath}
		})).Run(
			func(ctx context.Context, task *bridgeipc.Task, req apischema.FileExtractRequest) (FileExtractResult, error) {
				return runExtractTask(ctx, task, store, req)
			},
			bridgeipc.TaskDefault,
		),
		apischema.TaskRunner[apischema.BatchTransferRequest, FileBatchResult]("filebrowser.copy_batch", apischema.SessionTask(), apischema.WithTaskProgress[FileProgress](), apischema.WithTaskMetadata(func(req apischema.BatchTransferRequest) bridgeipc.TaskMetadata {
			return bridgeipc.TaskMetadata{Identity: append(append([]string{}, req.Sources...), req.Destination), Label: batchTaskLabel(req.Sources), Path: req.Destination}
		})).Run(
			func(ctx context.Context, task *bridgeipc.Task, req apischema.BatchTransferRequest) (FileBatchResult, error) {
				return runCopyBatchTask(ctx, task, store, req)
			},
			bridgeipc.TaskDefault,
		),
		apischema.TaskRunner[apischema.BatchTransferRequest, FileBatchResult]("filebrowser.move_batch", apischema.SessionTask(), apischema.WithTaskProgress[FileProgress](), apischema.WithTaskMetadata(func(req apischema.BatchTransferRequest) bridgeipc.TaskMetadata {
			return bridgeipc.TaskMetadata{Identity: append(append([]string{}, req.Sources...), req.Destination), Label: batchTaskLabel(req.Sources), Path: req.Destination}
		})).Run(
			func(ctx context.Context, task *bridgeipc.Task, req apischema.BatchTransferRequest) (FileBatchResult, error) {
				return runMoveBatchTask(ctx, task, store, req)
			},
			bridgeipc.TaskDefault,
		),
		apischema.TaskRunner[apischema.BatchPathRequest, FileBatchResult]("filebrowser.delete_batch", apischema.SessionTask(), apischema.WithTaskProgress[CountProgress](), apischema.WithTaskMetadata(func(req apischema.BatchPathRequest) bridgeipc.TaskMetadata {
			return bridgeipc.TaskMetadata{Identity: append([]string{}, req.Paths...), Label: batchTaskLabel(req.Paths)}
		})).Run(
			func(ctx context.Context, task *bridgeipc.Task, req apischema.BatchPathRequest) (FileBatchResult, error) {
				return runDeleteBatchTask(ctx, task, store, req)
			},
			bridgeipc.TaskDefault,
		),
		apischema.TaskRunner[apischema.BatchPathRequest, FileBatchResult]("filebrowser.trash_batch", apischema.SessionTask(), apischema.WithTaskProgress[CountProgress](), apischema.WithTaskMetadata(pathBatchMetadata)).Run(
			func(ctx context.Context, task *bridgeipc.Task, req apischema.BatchPathRequest) (FileBatchResult, error) {
				return runTrashBatchTask(ctx, task, store, req, owner)
			},
			bridgeipc.TaskDefault,
		),
		apischema.TaskRunner[apischema.BatchPathRequest, FileBatchResult]("filebrowser.trash_restore", apischema.SessionTask(), apischema.WithTaskProgress[CountProgress](), apischema.WithTaskMetadata(pathBatchMetadata)).Run(
			func(ctx context.Context, task *bridgeipc.Task, req apischema.BatchPathRequest) (FileBatchResult, error) {
				return runTrashRestoreTask(ctx, task, store, req, owner)
			},
			bridgeipc.TaskDefault,
		),
		apischema.TaskRunner[apischema.BatchPathRequest, FileBatchResult]("filebrowser.trash_purge", apischema.SessionTask(), apischema.WithTaskProgress[CountProgress](), apischema.WithTaskMetadata(pathBatchMetadata)).Run(
			func(ctx context.Context, task *bridgeipc.Task, req apischema.BatchPathRequest) (FileBatchResult, error) {
				return runTrashPurgeTask(ctx, task, store, req, owner)
			},
			bridgeipc.TaskDefault,
		),
		// The trash belongs to the session user, which only these bindings know.
		apischema.Call[apischema.NoRequest, apischema.TrashListResponse]("filebrowser.trash_list", apischema.RetrySafe()).Handle(
			func(ctx context.Context, _ apischema.NoRequest) (apischema.TrashListResponse, error) {
				return listTrash(ctx, owner)
			},
		),
		apischema.TaskRunner[apischema.OptionalPathRequest, indexer.IndexerResult]("filebrowser.index", apischema.Privileged(), apischema.SessionTask(), apischema.WithTaskProgress[indexer.IndexerProgress](), apischema.WithTaskMetadata(func(req apischema.OptionalPathRequest) bridgeipc.TaskMetadata {
			path := ""
			if req.Path != nil {
				path = *req.Path
			}
			return bridgeipc.TaskMetadata{Identity: []string{path}, Path: path, Label: path}
		})).Run(runIndexerTask, bridgeipc.TaskExclusive),
		apischema.TaskRunner[apischema.FileUploadRequest, FileUploadResult](routeUpload, apischema.SessionTask(), apischema.WithTaskProgress[FileProgress](), apischema.WithTaskMetadata(func(req apischema.FileUploadRequest) bridgeipc.TaskMetadata {
			return bridgeipc.TaskMetadata{Identity: []string{req.TargetPath}, Path: req.TargetPath, Label: req.TargetPath}
		})).Run(func(ctx context.Context, task *bridgeipc.Task, req apischema.FileUploadRequest) (FileUploadResult, error) {
			return transferResult[FileUploadResult](runUploadTask(ctx, task, req, owner))
		}, bridgeipc.TaskDefault),
		apischema.TaskRunner[apischema.FileUploadBatchRequest, FileUploadBatchResult](routeUploadBatch, apischema.SessionTask(), apischema.WithTaskProgress[BatchUploadProgress](), apischema.WithTaskMetadata(func(req apischema.FileUploadBatchRequest) bridgeipc.TaskMetadata {
			identity := []string{req.Destination}
			for _, file := range req.Files {
				identity = append(identity, "file", file.Path, "size", file.Size)
			}
			for _, directory := range req.Directories {
				identity = append(identity, "directory", directory)
			}
			if req.Overwrite != nil && *req.Overwrite {
				identity = append(identity, "overwrite", "true")
			} else {
				identity = append(identity, "overwrite", "false")
			}
			return bridgeipc.TaskMetadata{Identity: identity, Path: req.Destination, Label: req.Destination}
		})).Run(func(ctx context.Context, task *bridgeipc.Task, req apischema.FileUploadBatchRequest) (FileUploadBatchResult, error) {
			return transferResult[FileUploadBatchResult](runUploadBatchTask(ctx, task, req, owner))
		}, bridgeipc.TaskDefault),
		apischema.TaskRunner[apischema.FileArchiveRequest, FileArchiveResult](routeArchive, apischema.SessionTask(), apischema.WithTaskProgress[FileProgress](), apischema.WithTaskMetadata(func(req apischema.FileArchiveRequest) bridgeipc.TaskMetadata {
			return bridgeipc.TaskMetadata{Identity: append([]string{req.Format}, req.Paths...), Label: batchTaskLabel(req.Paths)}
		})).Run(
			func(ctx context.Context, task *bridgeipc.Task, req apischema.FileArchiveRequest) (FileArchiveResult, error) {
				return transferResult[FileArchiveResult](runArchiveTask(ctx, task, store, req))
			},
			bridgeipc.TaskDefault,
		),
		apischema.TaskRunner[apischema.FileChmodBatchRequest, FileBatchResult]("filebrowser.chmod_batch", apischema.SessionTask(), apischema.WithTaskProgress[CountProgress](), apischema.WithTaskMetadata(func(req apischema.FileChmodBatchRequest) bridgeipc.TaskMetadata {
			return bridgeipc.TaskMetadata{Identity: append([]string{req.Mode, req.Owner, req.Group}, req.Paths...), Label: batchTaskLabel(req.Paths)}
		})).Run(
			func(ctx context.Context, task *bridgeipc.Task, req apischema.FileChmodBatchRequest) (FileBatchResult, error) {
				return runChmodBatchTask(ctx, task, store, req)
			},
			bridgeipc.TaskDefault,
		),
	)
}

func pathBatchMetadata(req apischema.BatchPathRequest) bridgeipc.TaskMetadata {
	return bridgeipc.TaskMetadata{Identity: append([]string{}, req.Paths...), Label: batchTaskLabel(req.Paths)}
}

func RegisterTaskRoutes(router *bridgeipc.Router, store *userconfig.UserStore, owner uploadOwner) {
	fileTaskBindings(store, owner).Register(router)
	router.TaskService().RegisterTaskDataAttacher(routeUpload, attachFileTransferData)
	router.TaskService().RegisterTaskDataAttacher(routeUploadBatch, attachFileTransferData)
	router.TaskService().RegisterTaskDataAttacher(routeArchive, attachFileTransferData)
}
