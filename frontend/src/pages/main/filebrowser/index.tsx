import { Icon } from "@iconify/react";
import { useQueryClient } from "@tanstack/react-query";
import React, {
  ReactNode,
  Suspense,
  useCallback,
  useEffect,
  useEffectEvent,
  useMemo,
  useRef,
  useState,
} from "react";
import { useLocation, useNavigate } from "react-router-dom";

import {
  CACHE_TTL_MS,
  isConnected,
  linuxio,
  openJobDataStream,
  STREAM_MULTIPLEXER_CONFIG,
} from "@/api";
import FileBrowserDialog from "@/components/dialog/GeneralDialog";
import BreadcrumbsNav from "@/components/filebrowser/Breadcrumbs";
import CompressFormatDialog from "@/components/filebrowser/CompressFormatDialog";
import ConfirmDialog from "@/components/filebrowser/ConfirmDialog";
import ContextMenu from "@/components/filebrowser/ContextMenu";
import DirectoryListing from "@/components/filebrowser/DirectoryListing";
import ErrorState from "@/components/filebrowser/ErrorState";
import FileBrowserHeader from "@/components/filebrowser/FileBrowserHeader";
import FileDetail from "@/components/filebrowser/FileDetail";
import InputDialog from "@/components/filebrowser/InputDialog";
import MultiFileDetail from "@/components/filebrowser/MultiFileDetail";
import PermissionsDialog from "@/components/filebrowser/PermissionsDialog";
import SortBar, { SortField } from "@/components/filebrowser/SortBar";
import UnsavedChangesDialog from "@/components/filebrowser/UnsavedChangesDialog";
import {
  ensureTarGzExtension,
  ensureZipExtension,
  isArchiveFile,
  isEditableFile,
  stripArchiveExtension,
} from "@/components/filebrowser/utils";
import PageLoader from "@/components/loaders/PageLoader";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppFullscreenDialog from "@/components/ui/AppFullscreenDialog";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTypography from "@/components/ui/AppTypography";
import { useBackgroundJobActions } from "@/hooks/backgroundJobs/useBackgroundJobActions";
import { useFileDialogs } from "@/hooks/filebrowser/useFileDialogs";
import { useFileDragAndDrop } from "@/hooks/filebrowser/useFileDragAndDrop";
import { useFileEditor } from "@/hooks/filebrowser/useFileEditor";
import { useFileMutations } from "@/hooks/filebrowser/useFileMutations";
import { useFilePathUtilities } from "@/hooks/filebrowser/useFilePathUtilities";
import { useFileQueries } from "@/hooks/filebrowser/useFileQueries";
import { useFileSearch } from "@/hooks/filebrowser/useFileSearch";
import { useFileSelection } from "@/hooks/filebrowser/useFileSelection";
import { clearFileSubfoldersCache } from "@/hooks/filebrowser/useFileSubfolders";
import { useFileUpload } from "@/hooks/filebrowser/useFileUpload";
import { useFileViewState } from "@/hooks/filebrowser/useFileViewState";
import { useCapability } from "@/hooks/useCapabilities";
import { useConfig } from "@/hooks/useConfig";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useStreamResult } from "@/hooks/useStreamResult";
import { useAppTheme } from "@/theme";
import { FileItem, ViewMode } from "@/types/filebrowser";
import {
  buildEntriesFromFileList,
  mergeDroppedEntries,
  splitName,
  stripNumericSuffix,
} from "@/utils/fileUpload";
const viewIconMap: Record<ViewMode, ReactNode> = {
  card: <Icon height={20} icon="mdi:view-grid" width={20} />,
  list: <Icon height={20} icon="mdi:view-list" width={20} />,
};
const FileEditor = React.lazy(
  () => import("@/components/filebrowser/FileEditor"),
);
const FileBrowser: React.FC = () => {
  const toast = useScopedToast({ href: "/filebrowser", label: "Open files" });
  const { config } = useConfig();
  const chunkSize =
    (config.appSettings.chunkSizeMB ?? 0) > 0
      ? (config.appSettings.chunkSizeMB as number) * 1024 * 1024
      : STREAM_MULTIPLEXER_CONFIG.uploadChunkSize;
  const theme = useAppTheme();
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
  const [unsupportedEditPath, setUnsupportedEditPath] = useState<string | null>(
    null,
  );
  const [compressFormatDialog, setCompressFormatDialog] = useState<{
    paths: string[];
    baseName: string;
  } | null>(null);
  const queryClient = useQueryClient();
  const { startDownload, startUpload } = useBackgroundJobActions();
  const { isEnabled: indexerEnabled, status: indexerStatus } =
    useCapability("indexerAvailable");
  const { runChunked: runChunkedStreamResult } = useStreamResult();

  // Extract path from URL: /filebrowser/path/to/dir -> /path/to/dir
  // Decode each segment to handle URL-encoded characters (spaces, parentheses, etc.)
  const urlPath = location.pathname
    .replace(/^\/filebrowser\/?/, "")
    .split("/")
    .map((segment) => decodeURIComponent(segment))
    .join("/");
  const normalizedPath = urlPath ? `/${urlPath}` : "/";
  const [prevNormalizedPath, setPrevNormalizedPath] = useState(normalizedPath);
  if (normalizedPath !== prevNormalizedPath) {
    setPrevNormalizedPath(normalizedPath);
    setSearchQuery("");
  }
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
      basePath: "/",
      // Always search from root, not current folder
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
          modTime: result.mod_time || result.modTime || result.modified || "",
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
    // Only handle shortcuts when the file browser owns the interaction.
    const active = document.activeElement;
    const target = e.target;
    if (
      editingPath ||
      renamingPath ||
      document.querySelector(".app-dialog-root") ||
      active instanceof HTMLInputElement ||
      active instanceof HTMLTextAreaElement ||
      active instanceof HTMLSelectElement ||
      (active instanceof HTMLElement && active.isContentEditable) ||
      target instanceof HTMLInputElement ||
      target instanceof HTMLTextAreaElement ||
      target instanceof HTMLSelectElement ||
      (target instanceof HTMLElement && target.isContentEditable)
    )
      return;

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
        setUnsupportedEditPath(item.path);
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
    toast,
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
        await renameItem({
          from: path,
          destination,
        });
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

  const handleContextMenuRename = useCallback(() => {
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
    toast,
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
  const handleOpenContainingFolder = useCallback(() => {
    handleCloseContextMenu();
    const [selectedPath] = Array.from(selectedPaths);
    if (!selectedPath) return;
    const parentDir =
      selectedPath.substring(0, selectedPath.lastIndexOf("/")) || "/";
    setSearchQuery("");
    handleOpenDirectory(parentDir);
  }, [
    handleCloseContextMenu,
    selectedPaths,
    handleOpenDirectory,
    setSearchQuery,
  ]);
  const handleCompressSelection = useCallback(() => {
    handleCloseContextMenu();
    const paths = Array.from(selectedPaths);
    if (!paths.length) return;
    const baseName =
      selectedItems.length === 1
        ? stripArchiveExtension(selectedItems[0].name)
        : "archive";
    setCompressFormatDialog({ paths, baseName: baseName || "archive" });
  }, [handleCloseContextMenu, selectedItems, selectedPaths]);

  const handleCompressConfirm = useCallback(
    async (format: "zip" | "tar.gz") => {
      if (!compressFormatDialog) return;
      const { paths, baseName } = compressFormatDialog;
      const pendingNames = pendingArchiveNamesRef.current;
      const archiveName = getUniqueName(
        format === "tar.gz"
          ? ensureTarGzExtension(baseName)
          : ensureZipExtension(baseName),
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
    },
    [compressFormatDialog, compressItems, getUniqueName, normalizedPath, toast],
  );
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
      const fileName = filePath.split("/").pop() ?? filePath;
      if (isEditableFile(fileName)) {
        setEditingPath(filePath);
        setDetailTarget(null); // Close the detail dialog
      } else {
        setUnsupportedEditPath(filePath);
      }
    },
    [setDetailTarget, setEditingPath],
  );
  const saveContentViaStream = useCallback(
    async (path: string, contentBytes: Uint8Array) => {
      const job = await linuxio.filebrowser.upload({
        targetPath: path,
        size: String(contentBytes.length),
      });
      await runChunkedStreamResult<void>({
        open: () => openJobDataStream(job.id, 0),
        openErrorMessage: "Failed to open save stream",
        data: contentBytes,
        chunkSize: chunkSize,
        yieldMs: 0,
        closeMessage: "Stream closed unexpectedly",
      });
    },
    [chunkSize, runChunkedStreamResult],
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
      await saveContentViaStream(editingPath, contentBytes);
      toast.success("File saved successfully!");
      setIsEditorDirty(false);

      // Invalidate the file cache so it reloads with new content
      queryClient.invalidateQueries({
        queryKey: linuxio.filebrowser.resource_get.queryKey({
          path: editingPath,
          unused: "",
          getContent: "true",
        }),
      });
    } catch (error: any) {
      console.error("Save error:", error);
      toast.error(error.message || "Failed to save file");
    } finally {
      setIsSavingFile(false);
    }
  }, [
    editingPath,
    editorRef,
    queryClient,
    saveContentViaStream,
    setIsEditorDirty,
    setIsSavingFile,
    toast,
  ]);
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
      await saveContentViaStream(editingPath, contentBytes);
      toast.success("File saved successfully!");
      setIsEditorDirty(false);
      setEditingPath(null);
      setCloseEditorDialog(false);
      queryClient.invalidateQueries({
        queryKey: linuxio.filebrowser.resource_get.queryKey({
          path: editingPath,
          unused: "",
          getContent: "true",
        }),
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
    saveContentViaStream,
    toast,
  ]);
  const invalidateListing = useCallback(() => {
    queryClient.invalidateQueries({
      queryKey: linuxio.filebrowser.resource_get.queryKey({
        path: normalizedPath,
      }),
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
    [setUploadEntries, toast],
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
    toast,
  ]);
  return (
    <>
      <div
        data-allow-context-menu="true"
        onContextMenu={handleContextMenu}
        onDragEnter={handleDragEnter}
        onDragLeave={handleDragLeave}
        onDragOver={handleDragOver}
        onDrop={handleDrop}
        style={{
          height: "100%",
          width: "100%",
          display: "flex",
          flexDirection: "column",
          overflow: "hidden",
          position: "relative",
        }}
      >
        {!editingPath && (
          <FileBrowserHeader
            isSaving={isSavingFile}
            onSearchChange={handleSearchChange}
            onSwitchView={handleSwitchView}
            onToggleHiddenFiles={handleToggleHiddenFiles}
            searchQuery={searchQuery}
            showHiddenFiles={showHiddenFiles}
            viewIcon={viewIcon}
            viewMode={viewMode}
          />
        )}

        {/* Indexer unavailable warning */}
        {!indexerEnabled && !editingPath && (
          <AppAlert
            severity="info"
            style={{
              marginLeft: 8,
              marginRight: 8,
              marginTop: 4,
            }}
          >
            <AppAlertTitle>
              {indexerStatus === "unknown"
                ? "Checking Indexer Availability"
                : "Indexer API Unavailable"}
            </AppAlertTitle>
            {indexerStatus === "unknown" ? (
              <AppTypography variant="body2">
                Directory size calculations and file search stay disabled until
                indexer availability is confirmed.
              </AppTypography>
            ) : (
              <AppTypography variant="body2">
                Directory size calculations and file search are disabled. Start
                the <strong>indexer.target</strong> or{" "}
                <strong>indexer.socket</strong> to enable these features.
              </AppTypography>
            )}
          </AppAlert>
        )}

        <div
          style={{
            paddingLeft: editingPath ? 0 : theme.spacing(2),
            paddingRight: editingPath ? 0 : theme.spacing(2),
            flex: 1,
            minHeight: 0,
            display: "flex",
            flexDirection: "column",
          }}
        >
          {!editingPath && (
            <>
              <BreadcrumbsNav
                onNavigate={handleOpenDirectory}
                path={normalizedPath}
              />

              {!isPending &&
                !errorMessage &&
                resource &&
                resource.type === "directory" && (
                  <SortBar
                    onSortChange={handleSortChange}
                    sortOrder={sortOrder}
                  />
                )}
            </>
          )}
          <div
            style={{
              paddingLeft: editingPath ? 0 : theme.spacing(2),
              paddingRight: editingPath ? 0 : theme.spacing(2),
              flex: 1,
              minHeight: 0,
              display: "flex",
              flexDirection: "column",
              position: "relative",
            }}
          >
            {isPending && <PageLoader />}

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
                  cutPaths={cutPaths}
                  isContextMenuOpen={Boolean(contextMenuPosition)}
                  key={normalizedPath}
                  onCancelRename={handleCancelInlineRename}
                  onConfirmRename={handleConfirmInlineRename}
                  onDelete={handleDelete}
                  onDownloadFile={handleDoubleClickFile}
                  onOpenDirectory={handleOpenDirectory}
                  onSelectedPathsChange={setSelectedPaths}
                  onStartRename={handleStartInlineRename}
                  renamingPath={renamingPath}
                  resource={filteredResource}
                  selectedPaths={selectedPaths}
                  showHiddenFiles={showHiddenFiles}
                  sortField={sortField}
                  sortOrder={sortOrder}
                  viewMode={viewMode}
                />
              )}

            {!editingPath &&
              !isPending &&
              !errorMessage &&
              resource &&
              resource.type !== "directory" && (
                <FileDetail
                  onDownload={handleDownloadCurrent}
                  onEdit={handleEditFile}
                  resource={resource}
                />
              )}
          </div>
        </div>

        {isDragOver && !editingPath && resource?.type === "directory" && (
          <div
            style={{
              position: "absolute",
              inset: 0,
              border: `2px dashed ${theme.palette.primary.main}`,
              backgroundColor:
                "rgba(var(--mui-palette-primary-mainChannel) / 0.08)",
              zIndex: 5,
              display: "flex",
              flexDirection: "column",
              alignItems: "center",
              justifyContent: "center",
              pointerEvents: "none",
              gap: theme.spacing(1),
            }}
          >
            <AppTypography variant="h6">Drop to upload</AppTypography>
            <AppTypography color="text.secondary" variant="body2">
              Files and folders will be copied to {normalizedPath}
            </AppTypography>
          </div>
        )}
      </div>

      <AppFullscreenDialog
        contentStyle={{ backgroundColor: theme.palette.background.default }}
        onClose={handleCloseEditor}
        open={Boolean(editingPath)}
      >
        <FileBrowserHeader
          editingFileName={editingFileResource?.name}
          editingFilePath={editingPath || undefined}
          isDirty={isEditorDirty}
          isSaving={isSavingFile}
          onCloseEditor={handleCloseEditor}
          onSaveFile={handleSaveFile}
          onSearchChange={handleSearchChange}
          onSwitchView={handleSwitchView}
          onToggleHiddenFiles={handleToggleHiddenFiles}
          searchQuery={searchQuery}
          showHiddenFiles={showHiddenFiles}
          showQuickSave={showQuickSave}
          viewIcon={viewIcon}
          viewMode={viewMode}
        />
        <div
          style={{
            flex: 1,
            minHeight: 0,
            display: "flex",
            flexDirection: "column",
            overflow: "hidden",
          }}
        >
          {editingPath && isEditingFileLoading && (
            <div
              style={{
                flex: 1,
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
              }}
            >
              <PageLoader />
            </div>
          )}

          {editingPath && !isEditingFileLoading && editingFileResource && (
            <Suspense fallback={<PageLoader />}>
              <FileEditor
                fileName={editingFileResource.name}
                filePath={editingPath}
                initialContent={editingFileResource.content || ""}
                isSaving={isSavingFile}
                onDirtyChange={setIsEditorDirty}
                onSave={handleSaveFile}
                ref={editorRef}
              />
            </Suspense>
          )}
        </div>
      </AppFullscreenDialog>

      <ContextMenu
        anchorPosition={contextMenuPosition}
        canCompress={canCompressSelection}
        canExtract={canExtractSelection}
        canOpenContainingFolder={
          Boolean(searchQuery) && selectedPaths.size === 1
        }
        canRename={selectedPaths.size === 1}
        canShowDetails={canShowDetails}
        hasClipboard={clipboard !== null}
        hasSelection={selectedPaths.size > 0}
        onChangePermissions={handleChangePermissions}
        onClose={handleCloseContextMenu}
        onCompress={handleCompressSelection}
        onCopy={handleCopy}
        onCreateFile={handleCreateFile}
        onCreateFolder={handleCreateFolder}
        onCut={handleCut}
        onDelete={handleDelete}
        onDownload={handleDownloadSelected}
        onExtract={handleExtractSelection}
        onOpenContainingFolder={handleOpenContainingFolder}
        onPaste={handlePaste}
        onRename={handleContextMenuRename}
        onShowDetails={handleShowDetails}
        onUpload={handleUpload}
      />

      <CompressFormatDialog
        onClose={() => setCompressFormatDialog(null)}
        onConfirm={handleCompressConfirm}
        open={Boolean(compressFormatDialog)}
      />

      <FileBrowserDialog
        fullWidth
        maxWidth="md"
        onClose={handleCloseDetailDialog}
        open={Boolean(detailTarget)}
      >
        <AppDialogTitle
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            paddingRight: 8,
          }}
        >
          {detailTarget && detailTarget.length > 1
            ? "Multiple Items Details"
            : "File Details"}
          <AppIconButton onClick={handleCloseDetailDialog} size="small">
            <Icon height={18} icon="mdi:close" width={18} />
          </AppIconButton>
        </AppDialogTitle>
        <AppDialogContent
          style={{
            minHeight: 200,
            borderTop: `1px solid ${theme.palette.divider}`,
          }}
        >
          {shouldShowDetailLoader && <PageLoader />}
          {!shouldShowDetailLoader && hasSingleDetailTarget && detailError && (
            <AppTypography color="error">
              {detailError instanceof Error
                ? detailError.message
                : "Failed to load details"}
            </AppTypography>
          )}
          {detailResource && (
            <FileDetail
              isLoadingStat={isStatPending}
              onDownload={handleDownloadDetail}
              onEdit={handleEditFile}
              resource={detailResource}
              statData={statData}
            />
          )}
          {hasMultipleDetailTargets && multiItemsStats.items.length > 0 && (
            <MultiFileDetail
              isLoadingDetails={multiItemsStats.isAnyLoading}
              multiItems={multiItemsStats.items}
              onDownload={handleDownloadDetail}
              totalSize={multiItemsStats.totalSize}
            />
          )}
        </AppDialogContent>
      </FileBrowserDialog>

      <InputDialog
        label="File Name"
        onClose={() => setCreateFileDialog(false)}
        onConfirm={handleConfirmCreateFile}
        open={createFileDialog}
        title="Create File"
      />

      <InputDialog
        label="Folder Name"
        onClose={() => setCreateFolderDialog(false)}
        onConfirm={handleConfirmCreateFolder}
        open={createFolderDialog}
        title="Create Folder"
      />

      <ConfirmDialog
        confirmText="Delete"
        message={`Are you sure you want to delete ${pendingDeletePaths.length} item${pendingDeletePaths.length !== 1 ? "s" : ""}?`}
        onClose={handleCloseDeleteDialog}
        onConfirm={handleConfirmDelete}
        open={deleteDialog}
        title="Delete Items"
      />

      <ConfirmDialog
        confirmText="Edit Anyway"
        message={`"${unsupportedEditPath?.split("/").pop() ?? ""}" is not a recognized text file. Opening it in the editor may show garbled content, and saving could corrupt binary files. Edit anyway?`}
        onClose={() => setUnsupportedEditPath(null)}
        onConfirm={() => {
          if (unsupportedEditPath) {
            setEditingPath(unsupportedEditPath);
          }
          setUnsupportedEditPath(null);
          setDetailTarget(null);
        }}
        open={Boolean(unsupportedEditPath)}
        title="Edit Unsupported File?"
      />

      {permissionsDialog && (
        <PermissionsDialog
          currentMode={permissionsDialog.mode}
          group={permissionsDialog.group}
          isDirectory={permissionsDialog.isDirectory}
          onClose={handleClosePermissionsDialog}
          onConfirm={handleConfirmPermissions}
          open
          owner={permissionsDialog.owner}
          pathLabel={permissionsDialog.pathLabel}
          selectionCount={permissionsDialog.selectionCount}
        />
      )}

      <FileBrowserDialog
        disableEscapeKeyDown={isUploadProcessing}
        fullWidth
        maxWidth="sm"
        onClose={handleCloseUploadDialog}
        open={uploadDialogOpen}
      >
        <AppDialogTitle>Upload files or folders</AppDialogTitle>
        <AppDialogContent
          style={{ borderTop: `1px solid ${theme.palette.divider}` }}
        >
          <AppTypography color="text.secondary" variant="body2">
            Items will be uploaded to {normalizedPath}
          </AppTypography>
          <div
            style={{
              display: "flex",
              gap: theme.spacing(1.5),
              marginTop: theme.spacing(2),
              flexWrap: "wrap",
            }}
          >
            <AppButton onClick={handlePickFiles} variant="outlined">
              Select files
            </AppButton>
            <AppButton onClick={handlePickFolder} variant="outlined">
              Select folders
            </AppButton>
          </div>
          <input
            multiple
            onChange={handleUploadInputChange}
            ref={fileInputRef}
            style={{
              display: "none",
            }}
            type="file"
          />
          <input
            multiple
            onChange={handleUploadInputChange}
            ref={folderInputRef}
            style={{
              display: "none",
            }}
            type="file"
            {...({
              webkitdirectory: true,
              mozdirectory: true,
            } as any)}
          />
          <AppTypography
            style={{
              marginTop: 8,
            }}
            variant="body2"
          >
            {uploadEntries.length
              ? `Selected ${uploadSummary.files} file${uploadSummary.files === 1 ? "" : "s"} and ${uploadSummary.folders} folder${uploadSummary.folders === 1 ? "" : "s"}.`
              : "No items selected yet."}
          </AppTypography>
          {uploadEntries.length > 0 && (
            <ul
              className="custom-scrollbar"
              style={{
                margin: 0,
                marginTop: 6,
                paddingLeft: 20,
                maxHeight: 240,
                overflowY: "auto",
              }}
            >
              {uploadEntries.map((entry) => (
                <li
                  key={`${entry.isDirectory ? "dir" : "file"}-${entry.relativePath}`}
                  style={{ marginBottom: 6 }}
                >
                  <AppTypography variant="body2">
                    {entry.relativePath}
                  </AppTypography>
                  <AppTypography color="text.secondary" variant="caption">
                    {entry.isDirectory ? "Folder" : "File"}
                  </AppTypography>
                </li>
              ))}
            </ul>
          )}
        </AppDialogContent>
        <AppDialogActions>
          <AppButton
            disabled={!uploadEntries.length || isUploadProcessing}
            onClick={handleClearUploadSelection}
          >
            Clear
          </AppButton>
          <AppButton
            disabled={isUploadProcessing}
            onClick={handleCloseUploadDialog}
          >
            Cancel
          </AppButton>
          <AppButton
            disabled={!uploadEntries.length || isUploadProcessing}
            onClick={handleStartUpload}
            variant="contained"
          >
            {isUploadProcessing ? "Uploading..." : "Upload"}
          </AppButton>
        </AppDialogActions>
      </FileBrowserDialog>

      <FileBrowserDialog
        fullWidth
        maxWidth="sm"
        onClose={handleCancelOverwrite}
        open={Boolean(overwriteTargets?.length)}
      >
        <AppDialogTitle>Overwrite existing items?</AppDialogTitle>
        <AppDialogContent
          style={{ borderTop: `1px solid ${theme.palette.divider}` }}
        >
          <AppTypography
            style={{
              marginBottom: 4,
            }}
            variant="body2"
          >
            These items already exist in {normalizedPath}. Do you want to
            overwrite them?
          </AppTypography>
          <ul
            style={{
              margin: 0,
              paddingLeft: 20,
            }}
          >
            {overwriteTargets?.map(({ relativePath }) => (
              <li key={relativePath}>
                <AppTypography variant="body2">{relativePath}</AppTypography>
              </li>
            ))}
          </ul>
        </AppDialogContent>
        <AppDialogActions>
          <AppButton onClick={handleCancelOverwrite}>Skip</AppButton>
          <AppButton
            color="warning"
            onClick={handleConfirmOverwrite}
            variant="contained"
          >
            Overwrite
          </AppButton>
        </AppDialogActions>
      </FileBrowserDialog>

      <UnsavedChangesDialog
        isSaving={isSavingFile}
        onDiscardAndExit={handleDiscardAndExit}
        onKeepEditing={handleKeepEditing}
        onSaveAndExit={handleSaveAndExit}
        open={closeEditorDialog}
      />
    </>
  );
};
export type { ViewMode };
export default FileBrowser;
