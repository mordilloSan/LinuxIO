# FileBrowser Architecture

This document describes the organized architecture of the filebrowser package.

## Domain Organization

### A. Folder and File Operations & Information
**Location**: `services/` package

Services handle core business logic for file system operations and metadata retrieval:

- **`metadata_service.go`** - File and directory information
  - `FileInfoFaster(opts)` - Get file/directory metadata quickly
  - `GetDirInfo(path)` - Retrieve directory contents with metadata
  - Features: Symlink resolution, type detection, size calculation

- **`move_copy_service.go`** - File and folder operations
  - `MoveResource(src, dst)` - Move/rename files and directories
  - `CopyResource(src, dst)` - Copy files and directories
  - `ValidateMoveDestination(src, dst)` - Validate operations are safe
  - Features: Circular reference prevention, validation

- **`file_service.go`** - File I/O operations
  - `WriteFile(opts, reader)` - Write/create files
  - `WriteDirectory(opts)` - Create directories
  - `DeleteFiles(path)` - Delete files and directories
  - `GetContent(path)` - Read file content (with binary detection)
  - Features: Permission handling (0o664 files, 0o775 dirs), conflict resolution

**Low-level file operations**: `fileops/` package
- `operations.go` - Atomic file operations (MoveFile, CopyFile, etc.)

---

### B. Folder and File Download/Upload & Compression
**Location**: `raw.go` handler + `archive_service.go`

HTTP handlers and services for downloading/uploading and compressing files:

- **`raw.go`** - HTTP handler for raw file/folder downloads
  - `rawHandler(c *gin.Context)` - Main endpoint for file downloads
  - `rawFilesHandler(c, d, fileList)` - Handles single files and archives
  - Features: Single file streaming, archive generation, size calculation

- **`archive_service.go`** - Archive creation service
  - `CreateZip(path, files)` - Create ZIP archives
  - `CreateTarGz(path, files)` - Create TAR.GZ archives
  - `ComputeArchiveSize(files)` - Calculate archive sizes
  - Features: Recursive directory archiving, permission preservation

**Used by B**:
- `metadataSvc` - Get file information
- `archiveSvc` - Create archives

---

### C. Folder and File Metadata & Raw Access
**Location**: `resource.go` handlers

HTTP handlers for metadata retrieval and raw file access:

- **`resource.go`** - HTTP handlers for file operations
  - `resourceGetHandler(c)` - GET /api/resources - List files/folders
  - `resourceStatHandler(c)` - GET /api/resources/stat - Get metadata
  - `resourcePostHandler(c)` - POST /api/resources - Create files/folders
  - `resourcePutHandler(c)` - PUT /api/resources - Replace file content
  - `resourcePatchHandler(c)` - PATCH /api/resources - Move/copy/delete operations
  - `resourceDeleteHandler(c)` - DELETE /api/resources - Delete files/folders

**Used by C**:
- `metadataService` - Get file information
- `fileService` - Create/write files and directories
- `moveCopyService` - Move/copy/delete operations
- `fileops` - Low-level file operations

---

### D. API Routes
**Location**: `api.go`

Central route registration:

```go
r.GET("/api/resources", resourceGetHandler)           // C
r.GET("/api/resources/stat", resourceStatHandler)     // C
r.DELETE("/api/resources", resourceDeleteHandler)     // C
r.POST("/api/resources", resourcePostHandler)         // C
r.PUT("/api/resources", resourcePutHandler)           // C
r.PATCH("/api/resources", resourcePatchHandler)       // C
r.GET("/api/raw", rawHandler)                         // B
```

---

## Separation of Concerns

```
┌─────────────────────────────────────────────────────────┐
│                    API Routes (D)                       │
│                     api.go                              │
└──────────┬──────────────────────────────┬───────────────┘
           │                              │
    ┌──────▼──────────┐          ┌────────▼────────┐
    │  HTTP Handlers  │          │  HTTP Handlers  │
    │  (C)            │          │  (B)            │
    │  resource.go    │          │  raw.go         │
    └──────┬──────────┘          └────────┬────────┘
           │                              │
    ┌──────▼──────────────────────────────▼────────┐
    │           Services Layer (A)                  │
    │                                              │
    │  ┌─────────────────────────────────────────┐ │
    │  │  MetadataService  (file info)           │ │
    │  │  FileService      (I/O operations)      │ │
    │  │  MoveCopyService  (move/copy/delete)    │ │
    │  │  ArchiveService   (ZIP/TAR.GZ)          │ │
    │  └─────────────────────────────────────────┘ │
    │                                              │
    │  ┌─────────────────────────────────────────┐ │
    │  │  FileOps Package                        │ │
    │  │  (MoveFile, CopyFile, CommonPrefix)     │ │
    │  └─────────────────────────────────────────┘ │
    └──────────────┬─────────────────────────────┘
                   │
    ┌──────────────▼───────────────┐
    │   OS File System              │
    │   (os, filepath, etc.)        │
    └───────────────────────────────┘
```

---

## Key Design Decisions

1. **Services are business-logic focused**: They handle file operations, validation, and metadata retrieval independently of HTTP concerns.

2. **FileOps is low-level**: Direct filesystem operations (MoveFile, CopyFile) that services use.

3. **HTTP handlers are thin**: They orchestrate services and handle HTTP concerns (status codes, headers, request parsing).

4. **No facades**: Direct imports of services throughout the codebase, no wrapper functions.

5. **Permission handling**: Centralized in FileOps (0o664 for files, 0o775 for directories).

6. **Error handling**: Services return errors, handlers translate to HTTP responses.

---

## Usage Examples

### Getting file information (A)
```go
metadataSvc := services.NewMetadataService()
info, err := metadataSvc.FileInfoFaster(utils.FileOptions{
    Username: "user",
    Path: "/path/to/file",
    Content: true,
})
```

### Performing file operations (A)
```go
moveCopySvc := services.NewMoveCopyService()
err := moveCopySvc.MoveResource(false, "/src/file.txt", "/dst/file.txt")
```

### Creating archives (B)
```go
archiveSvc := services.NewArchiveService()
err := archiveSvc.CreateZip("archive.zip", "/path1", "/path2")
```

### HTTP handler for downloads (B)
```go
// raw.go uses archiveSvc to create archives and serve them
```

### HTTP handler for metadata (C)
```go
// resource.go uses metadataSvc to get file information and return JSON
```
