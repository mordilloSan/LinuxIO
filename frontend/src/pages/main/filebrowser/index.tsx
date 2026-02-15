import CloseIcon from "@mui/icons-material/Close";
import GridViewIcon from "@mui/icons-material/GridView";
import ViewListIcon from "@mui/icons-material/ViewList";
import {
  Alert,
  AlertTitle,
  Box,
  Button,
  DialogActions,
  DialogContent,
  DialogTitle,
  IconButton,
  List,
  ListItem,
  ListItemText,
  Typography,
} from "@mui/material";
import { useQueryClient } from "@tanstack/react-query";
import React, {
  ReactNode,
  useCallback,
  useEffect,
  useEffectEvent,
  useMemo,
  useRef,
  useState,
} from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { toast } from "sonner";

import {
  linuxio,
  CACHE_TTL_MS,
  isConnected,
  openFileUploadStream,
  STREAM_CHUNK_SIZE,
  type ResultFrame,
} from "@/api";
import BreadcrumbsNav from "@/components/filebrowser/Breadcrumbs";
import ConfirmDialog from "@/components/filebrowser/ConfirmDialog";
import ContextMenu from "@/components/filebrowser/ContextMenu";
import DirectoryListing from "@/components/filebrowser/DirectoryListing";
import ErrorState from "@/components/filebrowser/ErrorState";
import FileBrowserDialog from "@/components/filebrowser/FileBrowserDialog";
import FileBrowserHeader from "@/components/filebrowser/FileBrowserHeader";
import FileDetail from "@/components/filebrowser/FileDetail";
import FileEditor from "@/components/filebrowser/FileEditor";
import InputDialog from "@/components/filebrowser/InputDialog";
import MultiFileDetail from "@/components/filebrowser/MultiFileDetail";
import PermissionsDialog from "@/components/filebrowser/PermissionsDialog";
import SortBar, { SortField } from "@/components/filebrowser/SortBar";
import UnsavedChangesDialog from "@/components/filebrowser/UnsavedChangesDialog";
import {
  ensureZipExtension,
  isArchiveFile,
  isEditableFile,
  stripArchiveExtension,
} from "@/components/filebrowser/utils";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import useAuth from "@/hooks/useAuth";
import { useFileDialogs } from "@/hooks/useFileDialogs";
import { useFileDragAndDrop } from "@/hooks/useFileDragAndDrop";
import { useFileEditor } from "@/hooks/useFileEditor";
import { useFileMutations } from "@/hooks/useFileMutations";
import { useFilePathUtilities } from "@/hooks/useFilePathUtilities";
import { useFileQueries } from "@/hooks/useFileQueries";
import { useFileSearch } from "@/hooks/useFileSearch";
import { useFileSelection } from "@/hooks/useFileSelection";
import { clearFileSubfoldersCache } from "@/hooks/useFileSubfolders";
import { useFileTransfers } from "@/hooks/useFileTransfers";
import { useFileUpload } from "@/hooks/useFileUpload";
import { useFileViewState } from "@/hooks/useFileViewState";
import { ViewMode, FileItem } from "@/types/filebrowser";
import {
  buildEntriesFromFileList,
  mergeDroppedEntries,
  splitName,
  stripNumericSuffix,
} from "@/utils/fileUpload";

const viewIconMap: Record<ViewMode, ReactNode> = {
  card: <GridViewIcon fontSize="small" />,
  list: <ViewListIcon fontSize="small" />,
};

const FileBrowser: React.FC = () => {
  const location = useLocation();
  const navigate = useNavigate();

  // View state management
  const {
    viewMode,
    showHiddenFiles,
    sortField,
    setSortField,
    sortOrder,
    setSortOrder,
    contextMenuPosition,
    setContextMenuPosition,
    handleSwitchView,
    handleToggleHiddenFiles,
  } = useFileViewState();

  const viewIcon = useMemo(() => viewIconMap[viewMode], [viewMode]);

  // Dialog states
  const {
    createFileDialog,
    setCreateFileDialog,
    createFolderDialog,
    setCreateFolderDialog,
    deleteDialog,
    setDeleteDialog,
    pendingDeletePaths,
    setPendingDeletePaths,
    detailTarget,
    setDetailTarget,
    permissionsDialog,
    setPermissionsDialog,
  } = useFileDialogs();

  // Editor state
  const {
    editingPath,
    setEditingPath,
    isSavingFile,
    setIsSavingFile,
    isEditorDirty,
    setIsEditorDirty,
    closeEditorDialog,
    setCloseEditorDialog,
    editorRef,
    showQuickSave,
  } = useFileEditor();

  // Upload state
  const {
    uploadDialogOpen,
    setUploadDialogOpen,
    isUploadProcessing,
    setIsUploadProcessing,
    uploadEntries,
    setUploadEntries,
    fileInputRef,
    folderInputRef,
    uploadSummary,
  } = useFileUpload();

  const [searchQuery, setSearchQuery] = useState("");
  const [renamingPath, setRenamingPath] = useState<string | null>(null);

  const queryClient = useQueryClient();
  const { startDownload, startUpload } = useFileTransfers();
  const { indexerAvailable } = useAuth();

  // Extract path from URL: /filebrowser/path/to/dir -> /path/to/dir
  // Decode each segment to handle URL-encoded characters (spaces, parentheses, etc.)
  const urlPath = location.pathname
    .replace(/^\/filebrowser\/?/, "")
    .split("/")
    .map((segment) => decodeURIComponent(segment))
    .join("/");
  const normalizedPath = urlPath ? `/${urlPath}` : "/";

  const {
    createFile,
    createFolder,
    deleteItems,
    compressItems,
    extractArchive,
    changePermissions,
    copyItems,
    moveItems,
    renameItem,
  } = useFileMutations({
    normalizedPath,
    queryClient,
    onDeleteSuccess: () => setSelectedPaths(new Set()),
  });

  const detailTargetCount = detailTarget?.length ?? 0;
  const hasSingleDetailTarget = detailTargetCount === 1;
  const hasMultipleDetailTargets = detailTargetCount > 1;

  const {
    resource,
    isPending,
    errorMessage,
    detailResource,
    detailError,
    statData,
    isStatPending,
    multiItemsStats,
    editingFileResource,
    isEditingFileLoading,
    shouldShowDetailLoader,
  } = useFileQueries({
    normalizedPath,
    detailTarget,
    editingPath,
    hasSingleDetailTarget,
    hasMultipleDetailTargets,
  });

  const existingNames = useMemo(
    () => new Set(resource?.items?.map((item) => item.name) ?? []),
    [resource],
  );

  // Use indexer search when query is present (always from root)
  const { results: searchResults, isUnavailable: isSearchUnavailable } =
    useFileSearch({
      query: searchQuery,
      basePath: "/", // Always search from root, not current folder
      enabled: searchQuery.trim().length >= 2,
    });

  // Convert search results to FileItem format and create filtered resource
  const filteredResource = useMemo(() => {
    if (!resource || !searchQuery.trim()) {
      return resource;
    }

    if (resource.type !== "directory" || !resource.items) {
      return resource;
    }

    // If search is unavailable, fall back to client-side filtering
    if (isSearchUnavailable) {
      const lowerQuery = searchQuery.toLowerCase().trim();
      const filteredItems = resource.items.filter((item) =>
        item.name.toLowerCase().includes(lowerQuery),
      );
      return {
        ...resource,
        items: filteredItems,
      };
    }

    // Use search results from indexer
    if (searchResults.length > 0) {
      const items = searchResults.map((result) => {
        const normalizedType =
          typeof result.type === "string" ? result.type.toLowerCase() : "";
        const isDirectory =
          normalizedType === "directory" ||
          normalizedType === "dir" ||
          normalizedType === "folder" ||
          Boolean(result.isDir) ||
          result.path.endsWith("/");
        return {
          name: result.name,
          path: result.path,
          size: result.size,
          type: isDirectory
            ? "directory"
            : normalizedType && normalizedType !== "file"
              ? (result.type ?? "file")
              : "file",
          modTime: result.modTime || "",
          isDirectory,
          extension: isDirectory ? "" : result.name.split(".").pop() || "",
          showFullPath: true, // Show directory path in search results
        };
      });

      return {
        ...resource,
        items,
      };
    }

    // No results from search
    return {
      ...resource,
      items: [],
    };
  }, [resource, searchQuery, searchResults, isSearchUnavailable]);

  // Clear search when navigating to a different directory
  useEffect(() => {
    setSearchQuery("");
  }, [normalizedPath]);

  const handleCloseContextMenu = useCallback(() => {
    setContextMenuPosition(null);
  }, [setContextMenuPosition]);

  // Selection and clipboard management
  const {
    selectedPaths,
    setSelectedPaths,
    selectedItems,
    clipboard,
    handleCopy,
    handleCut,
    handlePaste,
  } = useFileSelection({
    resource,
    normalizedPath,
    copyItems,
    moveItems,
    onContextMenuClose: handleCloseContextMenu,
  });
  // Add keyboard shortcuts for copy/cut/paste operations
  const handleClipboardKeyDown = useEffectEvent((e: KeyboardEvent) => {
    // Only handle shortcuts when not editing a file
    if (editingPath) return;

    // Check if Ctrl (or Cmd on Mac) is pressed
    const isCtrlOrCmd = e.ctrlKey || e.metaKey;

    if (isCtrlOrCmd && e.key === "c") {
      e.preventDefault();
      handleCopy();
    } else if (isCtrlOrCmd && e.key === "x") {
      e.preventDefault();
      handleCut();
    } else if (isCtrlOrCmd && e.key === "v") {
      e.preventDefault();
      handlePaste();
    }
  });

  useEffect(() => {
    document.addEventListener("keydown", handleClipboardKeyDown);
    return () =>
      document.removeEventListener("keydown", handleClipboardKeyDown);
  }, []);

  // Derive cut paths from clipboard for visual dimming
  const cutPaths = useMemo(() => {
    if (clipboard?.operation === "cut") {
      return new Set(clipboard.paths);
    }
    return new Set<string>();
  }, [clipboard]);

  const pendingArchiveNamesRef = useRef<Set<string>>(new Set());
  const pendingArchiveConflictNamesRef = useRef<Set<string>>(new Set());

  const archiveSelection = useMemo(
    () =>
      selectedItems.length === 1 && isArchiveFile(selectedItems[0].name)
        ? selectedItems[0]
        : null,
    [selectedItems],
  );

  const canExtractSelection = Boolean(archiveSelection);
  const canCompressSelection = selectedPaths.size > 0;

  const canShowDetails = selectedPaths.size > 0;

  const handleCloseDetailDialog = useCallback(() => {
    setDetailTarget(null);
  }, [setDetailTarget]);

  const handleSearchChange = useCallback((value: string) => {
    setSearchQuery(value);
  }, []);

  const handleSortChange = useCallback(
    (field: SortField) => {
      setSortField((currentField) => {
        if (currentField === field) {
          // Toggle sort order if clicking the same field
          setSortOrder((currentOrder) =>
            currentOrder === "asc" ? "desc" : "asc",
          );
          return field;
        } else {
          // Reset to ascending when changing fields
          setSortOrder("asc");
          return field;
        }
      });
    },
    [setSortField, setSortOrder],
  );

  const handleOpenDirectory = useCallback(
    (path: string) => {
      if (path === "/") {
        navigate("/filebrowser");
        return;
      }
      // Encode each path segment for URL safety (handles spaces, parentheses, etc.)
      const urlPath = path
        .split("/")
        .filter(Boolean)
        .map((segment) => encodeURIComponent(segment))
        .join("/");
      navigate(`/filebrowser/${urlPath}`);
    },
    [navigate],
  );

  const handleDoubleClickFile = useCallback(
    (item: FileItem) => {
      if (isEditableFile(item.name)) {
        setEditingPath(item.path);
      } else {
        toast.warning("This file type cannot be edited");
      }
    },
    [setEditingPath],
  );

  const downloadPaths = useCallback(
    async (paths: string[]) => {
      await startDownload(paths);
    },
    [startDownload],
  );

  const getUniqueName = useCallback(
    (baseName: string, additionalNames?: Set<string>) => {
      const nameSet = new Set(existingNames);
      additionalNames?.forEach((name) => nameSet.add(name));
      pendingArchiveConflictNamesRef.current.forEach((name) =>
        nameSet.add(name),
      );

      const { base, ext } = splitName(baseName);
      const { root } = stripNumericSuffix(base);

      let hasPlain = false;
      let maxSuffix = 0;

      nameSet.forEach((name) => {
        const { base: candidateBase, ext: candidateExt } = splitName(name);
        if (candidateExt !== ext) {
          return;
        }
        const { root: candidateRoot, suffix } =
          stripNumericSuffix(candidateBase);
        if (candidateRoot !== root) {
          return;
        }
        if (suffix === null) {
          hasPlain = true;
        } else {
          if (suffix > maxSuffix) {
            maxSuffix = suffix;
          }
        }
      });

      if (!hasPlain && !nameSet.has(baseName)) {
        return baseName;
      }

      return `${root} (${maxSuffix + 1})${ext}`;
    },
    [existingNames],
  );

  useEffect(() => {
    const conflicts = pendingArchiveConflictNamesRef.current;
    const toRemove: string[] = [];
    conflicts.forEach((name) => {
      if (existingNames.has(name)) {
        toRemove.push(name);
      }
    });
    toRemove.forEach((name) => conflicts.delete(name));
  }, [existingNames]);

  // Path utilities
  const { joinPath, getParentPath } = useFilePathUtilities();

  const handleDownloadCurrent = useCallback(
    (path: string) => {
      downloadPaths([path]);
    },
    [downloadPaths],
  );

  const handleContextMenu = useCallback(
    (event: React.MouseEvent) => {
      event.preventDefault();
      setContextMenuPosition({
        top: event.clientY,
        left: event.clientX,
      });
    },
    [setContextMenuPosition],
  );

  const handleShowDetails = useCallback(() => {
    handleCloseContextMenu();
    if (selectedPaths.size === 0) return;
    setDetailTarget(Array.from(selectedPaths));
  }, [handleCloseContextMenu, selectedPaths, setDetailTarget]);

  const handleDownloadDetail = useCallback(
    (path: string) => {
      downloadPaths([path]);
    },
    [downloadPaths],
  );

  // Context menu action handlers
  const handleCreateFile = useCallback(() => {
    handleCloseContextMenu();
    setCreateFileDialog(true);
  }, [handleCloseContextMenu, setCreateFileDialog]);

  const handleCreateFolder = useCallback(() => {
    handleCloseContextMenu();
    setCreateFolderDialog(true);
  }, [handleCloseContextMenu, setCreateFolderDialog]);

  const handleConfirmCreateFile = useCallback(
    (fileName: string) => {
      createFile(fileName);
    },
    [createFile],
  );

  const handleConfirmCreateFolder = useCallback(
    (folderName: string) => {
      createFolder(folderName);
    },
    [createFolder],
  );

  const handleChangePermissions = useCallback(async () => {
    handleCloseContextMenu();

    if (selectedPaths.size === 0) return;

    const selectedPathList = Array.from(selectedPaths);
    const selectedPath = selectedPathList[0];
    const selectionCount = selectedPathList.length;
    const hasDirectorySelected = selectedItems.some(
      (item) => item.type === "directory",
    );

    try {
      // Fetch stat info to get current permissions (use first item as reference)
      // Args: [path]
      const stat = await queryClient.fetchQuery(
        linuxio.filebrowser.resource_stat.queryOptions(selectedPath, {
          staleTime: CACHE_TTL_MS.FIVE_SECONDS,
        }),
      );
      const mode = stat.mode || "0644"; // Default if not available
      const isDirectory = stat.mode?.startsWith("d") || hasDirectorySelected;
      const owner = stat.owner || undefined;
      const group = stat.group || undefined;

      setPermissionsDialog({
        paths: selectedPathList,
        pathLabel:
          selectionCount > 1 ? `${selectionCount} items` : selectedPath,
        selectionCount,
        mode: mode,
        isDirectory: isDirectory,
        owner: owner,
        group: group,
      });
    } catch (error) {
      console.error("Failed to fetch file stat:", error);
      toast.error("Failed to fetch file permissions");
    }
  }, [
    handleCloseContextMenu,
    queryClient,
    selectedPaths,
    selectedItems,
    setPermissionsDialog,
  ]);

  const handleStartInlineRename = useCallback(() => {
    handleCloseContextMenu();
    if (selectedPaths.size !== 1) {
      return;
    }
    const selectedPath = Array.from(selectedPaths)[0];
    setRenamingPath(selectedPath);
  }, [handleCloseContextMenu, selectedPaths]);

  const handleConfirmInlineRename = useCallback(
    async (path: string, newName: string) => {
      const trimmed = newName.trim();
      if (!trimmed) {
        setRenamingPath(null);
        return;
      }

      const target = resource?.items?.find((item) => item.path === path);
      const isDirectory = target?.type === "directory" || path.endsWith("/");
      const parent = getParentPath(path);
      let destination = joinPath(parent, trimmed);
      if (isDirectory && !destination.endsWith("/")) {
        destination += "/";
      }

      try {
        await renameItem({ from: path, destination });
        setRenamingPath(null);
      } catch {
        // errors handled by mutation toast
        setRenamingPath(null);
      }
    },
    [joinPath, getParentPath, renameItem, resource?.items],
  );

  const handleCancelInlineRename = useCallback(() => {
    setRenamingPath(null);
  }, []);

  // Keep dialog-based rename for backward compatibility (context menu)
  const handleRename = useCallback(() => {
    handleStartInlineRename();
  }, [handleStartInlineRename]);

  const handleDelete = useCallback(() => {
    handleCloseContextMenu();
    const paths = Array.from(selectedPaths);
    if (paths.length > 0) {
      setPendingDeletePaths(paths);
      setDeleteDialog(true);
    } else {
      toast.error("No items selected");
    }
  }, [
    handleCloseContextMenu,
    selectedPaths,
    setDeleteDialog,
    setPendingDeletePaths,
  ]);

  const handleConfirmDelete = useCallback(() => {
    if (!pendingDeletePaths.length) {
      return;
    }
    deleteItems(pendingDeletePaths);
    setPendingDeletePaths([]);
  }, [deleteItems, pendingDeletePaths, setPendingDeletePaths]);

  const handleCloseDeleteDialog = useCallback(() => {
    setDeleteDialog(false);
    setPendingDeletePaths([]);
  }, [setDeleteDialog, setPendingDeletePaths]);

  const handleDownloadSelected = useCallback(() => {
    handleCloseContextMenu();
    const paths = Array.from(selectedPaths);
    if (paths.length === 0) return;
    downloadPaths(paths);
  }, [handleCloseContextMenu, selectedPaths, downloadPaths]);

  const handleUpload = useCallback(() => {
    handleCloseContextMenu();
    setUploadEntries([]);
    setUploadDialogOpen(true);
  }, [handleCloseContextMenu, setUploadDialogOpen, setUploadEntries]);

  const handleCompressSelection = useCallback(async () => {
    handleCloseContextMenu();
    const paths = Array.from(selectedPaths);
    if (!paths.length) return;

    const baseName =
      selectedItems.length === 1
        ? stripArchiveExtension(selectedItems[0].name)
        : "archive";
    const pendingNames = pendingArchiveNamesRef.current;
    const archiveName = getUniqueName(
      ensureZipExtension(baseName || "archive"),
      pendingNames,
    );
    pendingNames.add(archiveName);

    try {
      await compressItems({
        paths,
        archiveName,
        destination: normalizedPath,
      });
    } catch (err: any) {
      const isConflict = err?.response?.status === 409;
      if (isConflict) {
        const message =
          err?.response?.data?.error || `${archiveName} already exists`;
        toast.error(message);
        pendingArchiveConflictNamesRef.current.add(archiveName);
      } else if (
        err?.name !== "CanceledError" &&
        err?.name !== "AbortError" &&
        err?.message !== "canceled"
      ) {
        const message =
          err?.response?.data?.error ||
          err?.message ||
          "Failed to create archive";
        toast.error(message);
      }
    } finally {
      pendingArchiveNamesRef.current.delete(archiveName);
    }
  }, [
    compressItems,
    getUniqueName,
    handleCloseContextMenu,
    normalizedPath,
    selectedItems,
    selectedPaths,
  ]);

  const handleExtractSelection = useCallback(async () => {
    handleCloseContextMenu();
    if (!archiveSelection) return;

    const targetFolder = getUniqueName(
      stripArchiveExtension(archiveSelection.name) || "extracted",
    );
    const destination = joinPath(normalizedPath, targetFolder);

    try {
      await extractArchive({
        archivePath: archiveSelection.path,
        destination,
      });
    } catch {
      // Errors are surfaced via toast in the mutation
    }
  }, [
    archiveSelection,
    extractArchive,
    getUniqueName,
    handleCloseContextMenu,
    joinPath,
    normalizedPath,
  ]);

  const handleClosePermissionsDialog = useCallback(() => {
    setPermissionsDialog(null);
  }, [setPermissionsDialog]);

  const handleConfirmPermissions = useCallback(
    async (
      mode: string,
      recursive: boolean,
      owner?: string,
      group?: string,
    ) => {
      if (!permissionsDialog) return;

      try {
        await Promise.all(
          permissionsDialog.paths.map((path) =>
            changePermissions({
              path: path,
              mode: mode,
              recursive: recursive,
              owner,
              group,
            }),
          ),
        );
        setPermissionsDialog(null);
      } catch {
        // Errors are surfaced via toast in the mutation
      }
    },
    [permissionsDialog, changePermissions, setPermissionsDialog],
  );

  const handleEditFile = useCallback(
    (filePath: string) => {
      setEditingPath(filePath);
      setDetailTarget(null); // Close the detail dialog
    },
    [setDetailTarget, setEditingPath],
  );

  const handleSaveFile = useCallback(async () => {
    if (!editorRef.current || !editingPath) return;

    if (!isConnected()) {
      toast.error("Stream connection not ready");
      return;
    }

    try {
      setIsSavingFile(true);
      const content = editorRef.current.getContent();
      const encoder = new TextEncoder();
      const contentBytes = encoder.encode(content);
      const contentSize = contentBytes.length;

      const stream = openFileUploadStream(editingPath, contentSize);

      if (!stream) {
        toast.error("Failed to open save stream");
        return;
      }

      await new Promise<void>((resolve, reject) => {
        stream.onResult = (result: ResultFrame) => {
          if (result.status === "ok") {
            resolve();
          } else {
            reject(new Error(result.error || "Save failed"));
          }
        };

        stream.onClose = () => {
          reject(new Error("Stream closed unexpectedly"));
        };

        // Send content in chunks
        let offset = 0;
        const sendNextChunk = () => {
          if (stream.status !== "open") return;

          if (offset >= contentSize) {
            stream.close();
            return;
          }

          const chunk = contentBytes.slice(offset, offset + STREAM_CHUNK_SIZE);
          stream.write(chunk);
          offset += chunk.length;

          // Continue sending
          if (offset < contentSize) {
            setTimeout(sendNextChunk, 0);
          } else {
            stream.close();
          }
        };

        sendNextChunk();
      });

      toast.success("File saved successfully!");
      setIsEditorDirty(false);

      // Invalidate the file cache so it reloads with new content
      queryClient.invalidateQueries({
        queryKey: linuxio.filebrowser.resource_get.queryKey(
          editingPath,
          "",
          "true",
        ),
      });
    } catch (error: any) {
      console.error("Save error:", error);
      toast.error(error.message || "Failed to save file");
    } finally {
      setIsSavingFile(false);
    }
  }, [editingPath, queryClient, editorRef, setIsEditorDirty, setIsSavingFile]);

  const handleCloseEditor = useCallback(() => {
    if (isEditorDirty) {
      setCloseEditorDialog(true);
    } else {
      setEditingPath(null);
      setIsEditorDirty(false);
    }
  }, [isEditorDirty, setCloseEditorDialog, setEditingPath, setIsEditorDirty]);

  const handleKeepEditing = useCallback(() => {
    setCloseEditorDialog(false);
  }, [setCloseEditorDialog]);

  const handleDiscardAndExit = useCallback(() => {
    setEditingPath(null);
    setIsEditorDirty(false);
    setCloseEditorDialog(false);
  }, [setCloseEditorDialog, setEditingPath, setIsEditorDirty]);

  const handleSaveAndExit = useCallback(async () => {
    if (!editorRef.current || !editingPath) return;

    if (!isConnected()) {
      toast.error("Stream connection not ready");
      return;
    }

    try {
      setIsSavingFile(true);
      const content = editorRef.current.getContent();
      const encoder = new TextEncoder();
      const contentBytes = encoder.encode(content);
      const contentSize = contentBytes.length;

      const stream = openFileUploadStream(editingPath, contentSize);

      if (!stream) {
        toast.error("Failed to open save stream");
        return;
      }

      await new Promise<void>((resolve, reject) => {
        stream.onResult = (result: ResultFrame) => {
          if (result.status === "ok") {
            resolve();
          } else {
            reject(new Error(result.error || "Save failed"));
          }
        };

        stream.onClose = () => {
          reject(new Error("Stream closed unexpectedly"));
        };

        // Send content in chunks
        let offset = 0;
        const sendNextChunk = () => {
          if (stream.status !== "open") return;

          if (offset >= contentSize) {
            stream.close();
            return;
          }

          const chunk = contentBytes.slice(offset, offset + STREAM_CHUNK_SIZE);
          stream.write(chunk);
          offset += chunk.length;

          if (offset < contentSize) {
            setTimeout(sendNextChunk, 0);
          } else {
            stream.close();
          }
        };

        sendNextChunk();
      });

      toast.success("File saved successfully!");
      setIsEditorDirty(false);
      setEditingPath(null);
      setCloseEditorDialog(false);

      queryClient.invalidateQueries({
        queryKey: linuxio.filebrowser.resource_get.queryKey(
          editingPath,
          "",
          "true",
        ),
      });
    } catch (error: any) {
      toast.error(error.message || "Failed to save file");
    } finally {
      setIsSavingFile(false);
    }
  }, [
    editingPath,
    queryClient,
    editorRef,
    setCloseEditorDialog,
    setEditingPath,
    setIsEditorDirty,
    setIsSavingFile,
  ]);

  const invalidateListing = useCallback(() => {
    queryClient.invalidateQueries({
      queryKey: linuxio.filebrowser.resource_get.queryKey(normalizedPath),
    });
    clearFileSubfoldersCache(queryClient);
  }, [normalizedPath, queryClient]);

  const {
    isDragOver,
    overwriteTargets,
    handleDragEnter,
    handleDragOver,
    handleDragLeave,
    handleDrop,
    handleConfirmOverwrite,
    handleCancelOverwrite,
    setOverwriteTargets: setOverwriteTargetsForDialog,
  } = useFileDragAndDrop({
    normalizedPath,
    resource,
    editingPath,
    startUpload,
    onUploadComplete: invalidateListing,
  });

  const handleUploadInputChange = useCallback(
    (event: React.ChangeEvent<HTMLInputElement>) => {
      const files = event.target.files;
      if (!files?.length) {
        event.target.value = "";
        return;
      }

      const entries = buildEntriesFromFileList(files);
      if (!entries.length) {
        event.target.value = "";
        toast.error("No files detected in selection");
        return;
      }

      setUploadEntries((prev) => mergeDroppedEntries(prev, entries));
      event.target.value = "";
    },
    [setUploadEntries],
  );

  const handleCloseUploadDialog = useCallback(() => {
    if (isUploadProcessing) return;
    setUploadDialogOpen(false);
    setUploadEntries([]);
  }, [isUploadProcessing, setUploadDialogOpen, setUploadEntries]);

  const handleClearUploadSelection = useCallback(() => {
    if (isUploadProcessing) return;
    setUploadEntries([]);
  }, [isUploadProcessing, setUploadEntries]);

  const handlePickFiles = useCallback(() => {
    fileInputRef.current?.click();
  }, [fileInputRef]);

  const handlePickFolder = useCallback(() => {
    folderInputRef.current?.click();
  }, [folderInputRef]);

  const handleStartUpload = useCallback(async () => {
    if (uploadEntries.length === 0) {
      toast.error("Select files or folders to upload");
      return;
    }

    setIsUploadProcessing(true);
    try {
      const result = await startUpload(uploadEntries, normalizedPath);
      if (result.conflicts.length) {
        setOverwriteTargetsForDialog(result.conflicts);
        toast.warning(
          `${result.conflicts.length} item${result.conflicts.length === 1 ? " is" : "s are"} already present. Overwrite them?`,
        );
      }
      if (result.uploaded > 0) {
        invalidateListing();
      }
      if (!result.conflicts.length) {
        setUploadDialogOpen(false);
        setUploadEntries([]);
      }
    } catch (err) {
      console.error("Upload failed", err);
      toast.error("Upload failed");
    } finally {
      setIsUploadProcessing(false);
    }
  }, [
    invalidateListing,
    normalizedPath,
    setOverwriteTargetsForDialog,
    startUpload,
    uploadEntries,
    setIsUploadProcessing,
    setUploadDialogOpen,
    setUploadEntries,
  ]);

  return (
    <>
      <Box
        data-allow-context-menu="true"
        sx={{
          height: "100%",
          width: "100%",
          display: "flex",
          flexDirection: "column",
          overflow: "hidden",
          position: "relative",
        }}
        onContextMenu={handleContextMenu}
        onDragEnter={handleDragEnter}
        onDragOver={handleDragOver}
        onDragLeave={handleDragLeave}
        onDrop={handleDrop}
      >
        <FileBrowserHeader
          viewMode={viewMode}
          showHiddenFiles={showHiddenFiles}
          showQuickSave={showQuickSave}
          onSwitchView={handleSwitchView}
          onToggleHiddenFiles={handleToggleHiddenFiles}
          onSaveFile={handleSaveFile}
          onCloseEditor={handleCloseEditor}
          isSaving={isSavingFile}
          viewIcon={viewIcon}
          editingFileName={editingFileResource?.name}
          editingFilePath={editingPath || undefined}
          isDirty={isEditorDirty}
          searchQuery={searchQuery}
          onSearchChange={handleSearchChange}
        />

        {/* Indexer unavailable warning */}
        {indexerAvailable === false && !editingPath && (
          <Alert severity="info" sx={{ mx: 2, mt: 1 }}>
            <AlertTitle>Indexer Service Unavailable</AlertTitle>
            <Typography variant="body2">
              Directory size calculations and file search are disabled. Start
              the <strong>linuxio-indexer.service</strong> to enable these
              features.
            </Typography>
          </Alert>
        )}

        <Box
          sx={{
            px: editingPath ? 0 : 2,
            flex: 1,
            minHeight: 0,
            display: "flex",
            flexDirection: "column",
          }}
        >
          {!editingPath && (
            <>
              <BreadcrumbsNav
                path={normalizedPath}
                onNavigate={handleOpenDirectory}
              />

              {!isPending &&
                !errorMessage &&
                resource &&
                resource.type === "directory" && (
                  <SortBar
                    sortOrder={sortOrder}
                    onSortChange={handleSortChange}
                  />
                )}
            </>
          )}
          <Box
            sx={{
              px: editingPath ? 0 : 2,
              flex: 1,
              minHeight: 0,
              display: "flex",
              flexDirection: "column",
            }}
          >
            {isPending && <ComponentLoader />}

            {!isPending && errorMessage && (
              <ErrorState
                message={errorMessage}
                onReset={() => handleOpenDirectory("/")}
              />
            )}

            {!editingPath &&
              !isPending &&
              !errorMessage &&
              filteredResource &&
              filteredResource.type === "directory" && (
                <DirectoryListing
                  key={normalizedPath}
                  resource={filteredResource}
                  showHiddenFiles={showHiddenFiles}
                  viewMode={viewMode}
                  sortField={sortField}
                  sortOrder={sortOrder}
                  onOpenDirectory={handleOpenDirectory}
                  onDownloadFile={handleDoubleClickFile}
                  selectedPaths={selectedPaths}
                  cutPaths={cutPaths}
                  onSelectedPathsChange={setSelectedPaths}
                  isContextMenuOpen={Boolean(contextMenuPosition)}
                  onDelete={handleDelete}
                  renamingPath={renamingPath}
                  onStartRename={handleStartInlineRename}
                  onConfirmRename={handleConfirmInlineRename}
                  onCancelRename={handleCancelInlineRename}
                />
              )}

            {editingPath && isEditingFileLoading && <ComponentLoader />}

            {!isPending &&
              !errorMessage &&
              editingPath &&
              !isEditingFileLoading &&
              editingFileResource && (
                <FileEditor
                  ref={editorRef}
                  filePath={editingPath}
                  fileName={editingFileResource.name}
                  initialContent={editingFileResource.content || ""}
                  onSave={handleSaveFile}
                  isSaving={isSavingFile}
                  onDirtyChange={setIsEditorDirty}
                />
              )}

            {!editingPath &&
              !isPending &&
              !errorMessage &&
              resource &&
              resource.type !== "directory" && (
                <FileDetail
                  resource={resource}
                  onDownload={handleDownloadCurrent}
                  onEdit={handleEditFile}
                />
              )}
          </Box>
        </Box>

        {isDragOver && !editingPath && resource?.type === "directory" && (
          <Box
            sx={{
              position: "absolute",
              inset: 0,
              border: "2px dashed",
              borderColor: "primary.main",
              bgcolor: "rgba(25,118,210,0.08)",
              zIndex: 5,
              display: "flex",
              flexDirection: "column",
              alignItems: "center",
              justifyContent: "center",
              pointerEvents: "none",
              gap: 1,
            }}
          >
            <Typography variant="h6">Drop to upload</Typography>
            <Typography variant="body2" color="text.secondary">
              Files and folders will be copied to {normalizedPath}
            </Typography>
          </Box>
        )}
      </Box>

      <ContextMenu
        anchorPosition={contextMenuPosition}
        hasSelection={selectedPaths.size > 0}
        hasClipboard={clipboard !== null}
        onClose={handleCloseContextMenu}
        onCreateFile={handleCreateFile}
        onCreateFolder={handleCreateFolder}
        onChangePermissions={handleChangePermissions}
        onCopy={handleCopy}
        onCut={handleCut}
        onRename={handleRename}
        onPaste={handlePaste}
        onDelete={handleDelete}
        onDownload={handleDownloadSelected}
        onUpload={handleUpload}
        onShowDetails={handleShowDetails}
        onCompress={handleCompressSelection}
        onExtract={handleExtractSelection}
        canShowDetails={canShowDetails}
        canCompress={canCompressSelection}
        canExtract={canExtractSelection}
        canRename={selectedPaths.size === 1}
      />

      <FileBrowserDialog
        open={Boolean(detailTarget)}
        onClose={handleCloseDetailDialog}
        maxWidth="md"
        fullWidth
      >
        <DialogTitle
          sx={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            pr: 2,
          }}
        >
          {detailTarget && detailTarget.length > 1
            ? "Multiple Items Details"
            : "File Details"}
          <IconButton onClick={handleCloseDetailDialog} size="small">
            <CloseIcon fontSize="small" />
          </IconButton>
        </DialogTitle>
        <DialogContent dividers sx={{ minHeight: 200 }}>
          {shouldShowDetailLoader && <ComponentLoader />}
          {!shouldShowDetailLoader && hasSingleDetailTarget && detailError && (
            <Typography color="error">
              {detailError instanceof Error
                ? detailError.message
                : "Failed to load details"}
            </Typography>
          )}
          {detailResource && (
            <FileDetail
              resource={detailResource}
              onDownload={handleDownloadDetail}
              onEdit={handleEditFile}
              statData={statData}
              isLoadingStat={isStatPending}
            />
          )}
          {hasMultipleDetailTargets && multiItemsStats.items.length > 0 && (
            <MultiFileDetail
              multiItems={multiItemsStats.items}
              totalSize={multiItemsStats.totalSize}
              onDownload={handleDownloadDetail}
              isLoadingDetails={multiItemsStats.isAnyLoading}
            />
          )}
        </DialogContent>
      </FileBrowserDialog>

      <InputDialog
        open={createFileDialog}
        title="Create File"
        label="File Name"
        onClose={() => setCreateFileDialog(false)}
        onConfirm={handleConfirmCreateFile}
      />

      <InputDialog
        open={createFolderDialog}
        title="Create Folder"
        label="Folder Name"
        onClose={() => setCreateFolderDialog(false)}
        onConfirm={handleConfirmCreateFolder}
      />

      <ConfirmDialog
        open={deleteDialog}
        title="Delete Items"
        message={`Are you sure you want to delete ${pendingDeletePaths.length} item${pendingDeletePaths.length !== 1 ? "s" : ""}?`}
        confirmText="Delete"
        onClose={handleCloseDeleteDialog}
        onConfirm={handleConfirmDelete}
      />

      {permissionsDialog && (
        <PermissionsDialog
          open
          pathLabel={permissionsDialog.pathLabel}
          selectionCount={permissionsDialog.selectionCount}
          currentMode={permissionsDialog.mode}
          isDirectory={permissionsDialog.isDirectory}
          owner={permissionsDialog.owner}
          group={permissionsDialog.group}
          onClose={handleClosePermissionsDialog}
          onConfirm={handleConfirmPermissions}
        />
      )}

      <FileBrowserDialog
        open={uploadDialogOpen}
        onClose={handleCloseUploadDialog}
        maxWidth="sm"
        fullWidth
        disableEscapeKeyDown={isUploadProcessing}
      >
        <DialogTitle>Upload files or folders</DialogTitle>
        <DialogContent dividers>
          <Typography variant="body2" color="text.secondary">
            Items will be uploaded to {normalizedPath}
          </Typography>
          <Box sx={{ display: "flex", gap: 1.5, mt: 2, flexWrap: "wrap" }}>
            <Button variant="outlined" onClick={handlePickFiles}>
              Select files
            </Button>
            <Button variant="outlined" onClick={handlePickFolder}>
              Select folders
            </Button>
          </Box>
          <input
            ref={fileInputRef}
            type="file"
            multiple
            style={{ display: "none" }}
            onChange={handleUploadInputChange}
          />
          <input
            ref={folderInputRef}
            type="file"
            multiple
            style={{ display: "none" }}
            onChange={handleUploadInputChange}
            {...({ webkitdirectory: true, mozdirectory: true } as any)}
          />
          <Typography variant="body2" sx={{ mt: 2 }}>
            {uploadEntries.length
              ? `Selected ${uploadSummary.files} file${uploadSummary.files === 1 ? "" : "s"} and ${uploadSummary.folders} folder${uploadSummary.folders === 1 ? "" : "s"}.`
              : "No items selected yet."}
          </Typography>
          {uploadEntries.length > 0 && (
            <List dense sx={{ mt: 1.5, maxHeight: 240, overflowY: "auto" }}>
              {uploadEntries.map((entry) => (
                <ListItem
                  key={`${entry.isDirectory ? "dir" : "file"}-${entry.relativePath}`}
                >
                  <ListItemText
                    primary={entry.relativePath}
                    secondary={entry.isDirectory ? "Folder" : "File"}
                  />
                </ListItem>
              ))}
            </List>
          )}
        </DialogContent>
        <DialogActions>
          <Button
            onClick={handleClearUploadSelection}
            disabled={!uploadEntries.length || isUploadProcessing}
          >
            Clear
          </Button>
          <Button
            onClick={handleCloseUploadDialog}
            disabled={isUploadProcessing}
          >
            Cancel
          </Button>
          <Button
            onClick={handleStartUpload}
            variant="contained"
            disabled={!uploadEntries.length || isUploadProcessing}
          >
            {isUploadProcessing ? "Uploading..." : "Upload"}
          </Button>
        </DialogActions>
      </FileBrowserDialog>

      <FileBrowserDialog
        open={Boolean(overwriteTargets?.length)}
        onClose={handleCancelOverwrite}
        maxWidth="sm"
        fullWidth
      >
        <DialogTitle>Overwrite existing items?</DialogTitle>
        <DialogContent dividers>
          <Typography variant="body2" sx={{ mb: 1 }}>
            These items already exist in {normalizedPath}. Do you want to
            overwrite them?
          </Typography>
          <List dense disablePadding>
            {overwriteTargets?.map(({ relativePath }) => (
              <ListItem key={relativePath} disableGutters>
                <ListItemText primary={relativePath} />
              </ListItem>
            ))}
          </List>
        </DialogContent>
        <DialogActions>
          <Button onClick={handleCancelOverwrite}>Skip</Button>
          <Button
            onClick={handleConfirmOverwrite}
            variant="contained"
            color="warning"
          >
            Overwrite
          </Button>
        </DialogActions>
      </FileBrowserDialog>

      <UnsavedChangesDialog
        open={closeEditorDialog}
        onKeepEditing={handleKeepEditing}
        onDiscardAndExit={handleDiscardAndExit}
        onSaveAndExit={handleSaveAndExit}
        isSaving={isSavingFile}
      />
    </>
  );
};

export type { ViewMode };
export default FileBrowser;
