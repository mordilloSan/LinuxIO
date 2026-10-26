import type { ROUTE_MODES, RouteName } from "@/api/generated/route-metadata";

// Backend task type identifiers: a Task's `type` is the name of the generated
// task route that started it, so a renamed or removed route fails to compile.

type TaskRoute = {
  [R in RouteName]: (typeof ROUTE_MODES)[R] extends "task" ? R : never;
}[RouteName];

const taskType = <R extends TaskRoute>(route: R): R => route;

export const TASK_TYPE_FILE_COMPRESS = taskType("filebrowser.compress");
export const TASK_TYPE_FILE_EXTRACT = taskType("filebrowser.extract");
export const TASK_TYPE_FILE_COPY_BATCH = taskType("filebrowser.copy_batch");
export const TASK_TYPE_FILE_MOVE_BATCH = taskType("filebrowser.move_batch");
export const TASK_TYPE_FILE_DELETE_BATCH = taskType("filebrowser.delete_batch");
export const TASK_TYPE_FILE_TRASH_BATCH = taskType("filebrowser.trash_batch");
export const TASK_TYPE_FILE_TRASH_RESTORE = taskType(
  "filebrowser.trash_restore",
);
export const TASK_TYPE_FILE_TRASH_PURGE = taskType("filebrowser.trash_purge");
export const TASK_TYPE_FILE_INDEXER = taskType("filebrowser.index");
export const TASK_TYPE_FILE_UPLOAD = taskType("filebrowser.upload");
export const TASK_TYPE_FILE_UPLOAD_BATCH = taskType("filebrowser.upload_batch");
export const TASK_TYPE_FILE_ARCHIVE = taskType("filebrowser.archive");
export const TASK_TYPE_FILE_CHMOD_BATCH = taskType("filebrowser.chmod_batch");
export const TASK_TYPE_DOCKER_COMPOSE = taskType("docker.compose");
export const TASK_TYPE_DOCKER_UPDATE = taskType("docker.update_container");
export const TASK_TYPE_PACKAGE_UPDATE = taskType("packages.update");
export const TASK_TYPE_STORAGE_SMART_TEST = taskType("storage.run_smart_test");
export const TASK_TYPE_STORAGE_BTRFS_ROLLBACK = taskType(
  "storage.btrfs_snapshot_rollback",
);
export const TASK_TYPE_STORAGE_BTRFS_REPLICATION = taskType(
  "storage.btrfs_replication_run",
);
export const TASK_TYPE_STORAGE_BTRFS_BALANCE = taskType(
  "storage.btrfs_balance",
);
export const TASK_TYPE_STORAGE_PARTITION_CREATE = taskType(
  "storage.partition_create",
);
export const TASK_TYPE_STORAGE_PARTITION_FORMAT = taskType(
  "storage.partition_format",
);
export const TASK_TYPE_SYSTEM_INSTALL_CAPABILITY = taskType(
  "system.install_capability",
);
