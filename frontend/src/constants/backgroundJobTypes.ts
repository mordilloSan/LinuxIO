// Backend job type identifiers, matching the `JobType` constants registered
// by the corresponding Go handlers (e.g. `JobTypeStorageSmartTest`).

export const JOB_TYPE_FILE_COMPRESS = "filebrowser.compress";
export const JOB_TYPE_FILE_EXTRACT = "filebrowser.extract";
export const JOB_TYPE_FILE_COPY = "filebrowser.copy";
export const JOB_TYPE_FILE_MOVE = "filebrowser.move";
export const JOB_TYPE_FILE_INDEXER = "filebrowser.index";
export const JOB_TYPE_FILE_UPLOAD = "filebrowser.upload";
export const JOB_TYPE_FILE_DOWNLOAD = "filebrowser.download";
export const JOB_TYPE_FILE_ARCHIVE = "filebrowser.archive";
export const JOB_TYPE_FILE_CHMOD = "filebrowser.chmod";
export const JOB_TYPE_FILE_DELETE = "filebrowser.resource_delete";
export const JOB_TYPE_DOCKER_COMPOSE = "docker.compose";
export const JOB_TYPE_DOCKER_INDEXER = "docker.indexer";
export const JOB_TYPE_PACKAGE_UPDATE = "packages.update";
export const JOB_TYPE_STORAGE_SMART_TEST = "storage.run_smart_test";
export const JOB_TYPE_SYSTEM_INSTALL_CAPABILITY = "system.install_capability";
