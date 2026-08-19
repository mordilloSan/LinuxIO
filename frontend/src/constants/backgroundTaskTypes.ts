// Backend task type identifiers, matching the `TaskType` constants registered
// by the corresponding Go handlers (e.g. `TaskTypeStorageSmartTest`).

export const TASK_TYPE_FILE_COMPRESS = "filebrowser.compress";
export const TASK_TYPE_FILE_EXTRACT = "filebrowser.extract";
export const TASK_TYPE_FILE_COPY_BATCH = "filebrowser.copy_batch";
export const TASK_TYPE_FILE_MOVE_BATCH = "filebrowser.move_batch";
export const TASK_TYPE_FILE_DELETE_BATCH = "filebrowser.delete_batch";
export const TASK_TYPE_FILE_INDEXER = "filebrowser.index";
export const TASK_TYPE_FILE_UPLOAD = "filebrowser.upload";
export const TASK_TYPE_FILE_UPLOAD_BATCH = "filebrowser.upload_batch";
export const TASK_TYPE_FILE_ARCHIVE = "filebrowser.archive";
export const TASK_TYPE_FILE_CHMOD_BATCH = "filebrowser.chmod_batch";
export const TASK_TYPE_DOCKER_COMPOSE = "docker.compose";
export const TASK_TYPE_DOCKER_UPDATE = "docker.update_container";
export const TASK_TYPE_PACKAGE_UPDATE = "packages.update";
export const TASK_TYPE_STORAGE_SMART_TEST = "storage.run_smart_test";
export const TASK_TYPE_SYSTEM_INSTALL_CAPABILITY = "system.install_capability";
