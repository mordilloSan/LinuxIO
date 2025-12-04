import CloseIcon from "@mui/icons-material/Close";
import GridViewIcon from "@mui/icons-material/GridView";
import ViewListIcon from "@mui/icons-material/ViewList";
import {
  Box,
  Button,
  Dialog,
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
  useMemo,
  useRef,
  useState,
} from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { toast } from "sonner";

import { useDragAndDropUpload } from "../../../hooks/useDragAndDropUpload";
import { useFileBrowserQueries } from "../../../hooks/useFileBrowserQueries";
import { useFileMutations } from "../../../hooks/useFileMutations";

import BreadcrumbsNav from "@/components/filebrowser/Breadcrumbs";
import ConfirmDialog from "@/components/filebrowser/ConfirmDialog";
import ContextMenu from "@/components/filebrowser/ContextMenu";
import DirectoryListing from "@/components/filebrowser/DirectoryListing";
import ErrorState from "@/components/filebrowser/ErrorState";
import FileBrowserHeader from "@/components/filebrowser/FileBrowserHeader";
import FileDetail from "@/components/filebrowser/FileDetail";
import FileEditor from "@/components/filebrowser/FileEditor";
import { FileEditorHandle } from "@/components/filebrowser/FileEditor";
import InputDialog from "@/components/filebrowser/InputDialog";
import MultiFileDetail from "@/components/filebrowser/MultiFileDetail";
import PermissionsDialog from "@/components/filebrowser/PermissionsDialog";
import SortBar, {
  SortField,
  SortOrder,
} from "@/components/filebrowser/SortBar";
import UnsavedChangesDialog from "@/components/filebrowser/UnsavedChangesDialog";
import {
  ensureZipExtension,
  isArchiveFile,
  stripArchiveExtension,
} from "@/components/filebrowser/utils";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import { useConfigValue } from "@/hooks/useConfig";
import { useFileTransfers } from "@/hooks/useFileTransfers";
import { ViewMode, FileItem } from "@/types/filebrowser";
import axios from "@/utils/axios";

const viewModes: ViewMode[] = ["card", "list"];

const viewIconMap: Record<ViewMode, ReactNode> = {
  card: <GridViewIcon fontSize="small" />,
  list: <ViewListIcon fontSize="small" />,
};

type ClipboardOperation = "copy" | "cut";

interface ClipboardData {
  paths: string[];
  operation: ClipboardOperation;
}

const FileBrowser: React.FC = () => {
  const location = useLocation();
  const navigate = useNavigate();

  const [viewMode, setViewMode] = useState<ViewMode>("card");
  const [showHiddenFiles, setShowHiddenFilesConfig] =
    useConfigValue("showHiddenFiles");
  const [sortField, setSortField] = useState<SortField>("name");
  const [sortOrder, setSortOrder] = useState<SortOrder>("asc");
  const [contextMenuPosition, setContextMenuPosition] = useState<{
    top: number;
    left: number;
  } | null>(null);
  const [selectedPaths, setSelectedPaths] = useState<Set<string>>(new Set());
  const [createFileDialog, setCreateFileDialog] = useState(false);
  const [createFolderDialog, setCreateFolderDialog] = useState(false);
  const [deleteDialog, setDeleteDialog] = useState(false);
  const [pendingDeletePaths, setPendingDeletePaths] = useState<string[]>([]);
  const [detailTarget, setDetailTarget] = useState<string[] | null>(null);
  const [editingPath, setEditingPath] = useState<string | null>(null);
  const [isSavingFile, setIsSavingFile] = useState(false);
  const [isEditorDirty, setIsEditorDirty] = useState(false);
  const [closeEditorDialog, setCloseEditorDialog] = useState(false);
  const [permissionsDialog, setPermissionsDialog] = useState<{
    paths: string[];
    pathLabel: string;
    selectionCount: number;
    mode: string;
    isDirectory: boolean;
    owner?: string;
    group?: string;
  } | null>(null);
  const [clipboard, setClipboard] = useState<ClipboardData | null>(null);
  const editorRef = useRef<FileEditorHandle>(null);

  const queryClient = useQueryClient();
  const { startDownload, startUpload } = useFileTransfers();

  const showQuickSave = editingPath !== null;

  // Extract path from URL: /filebrowser/path/to/dir -> /path/to/dir
  const urlPath = location.pathname.replace(/^\/filebrowser\/?/, "");
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
  } = useFileBrowserQueries({
    normalizedPath,
    detailTarget,
    editingPath,
    hasSingleDetailTarget,
    hasMultipleDetailTargets,
  });

  const viewIcon = useMemo(() => viewIconMap[viewMode], [viewMode]);

  const selectedItems = useMemo(() => {
    if (!resource || resource.type !== "directory" || !resource.items) {
      return [];
    }
    const itemMap = new Map(resource.items.map((item) => [item.path, item]));
    return Array.from(selectedPaths)
      .map((path) => itemMap.get(path))
      .filter(Boolean) as FileItem[];
  }, [resource, selectedPaths]);

  const existingNames = useMemo(
    () => new Set(resource?.items?.map((item) => item.name) ?? []),
    [resource],
  );

  const archiveSelection = useMemo(
    () =>
      selectedItems.length === 1 && isArchiveFile(selectedItems[0].name)
        ? selectedItems[0]
        : null,
    [selectedItems],
  );

  const canExtractSelection = Boolean(archiveSelection);
  const canCompressSelection = selectedPaths.size > 0;

  const handleSwitchView = useCallback(() => {
    setViewMode((current) => {
      const index = viewModes.indexOf(current);
      const next = (index + 1) % viewModes.length;
      return viewModes[next];
    });
  }, []);

  const canShowDetails = selectedPaths.size > 0;

  const handleCloseDetailDialog = useCallback(() => {
    setDetailTarget(null);
  }, []);

  const handleToggleHiddenFiles = useCallback(() => {
    setShowHiddenFilesConfig((prev) => !prev);
  }, [setShowHiddenFilesConfig]);

  const handleSortChange = useCallback((field: SortField) => {
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
  }, []);

  const handleOpenDirectory = useCallback(
    (path: string) => {
      if (path === "/") {
        navigate("/filebrowser");
        return;
      }
      // Remove leading slash for the URL path
      const urlPath = path.startsWith("/") ? path.slice(1) : path;
      navigate(`/filebrowser/${urlPath}`);
    },
    [navigate],
  );

  const handleDoubleClickFile = useCallback((item: FileItem) => {
    setDetailTarget([item.path]);
  }, []);

  const downloadPaths = useCallback(
    async (paths: string[]) => {
      await startDownload(paths);
    },
    [startDownload],
  );

  const getUniqueName = useCallback(
    (baseName: string) => {
      if (!existingNames.size) return baseName;
      let candidate = baseName;
      let counter = 1;

      while (existingNames.has(candidate)) {
        candidate = `${baseName} (${counter})`;
        counter += 1;
      }

      return candidate;
    },
    [existingNames],
  );

  const joinPath = useCallback((base: string, name: string) => {
    if (base.endsWith("/")) {
      return `${base}${name}`;
    }
    return `${base}/${name}`;
  }, []);

  const handleDownloadCurrent = useCallback(
    (path: string) => {
      downloadPaths([path]);
    },
    [downloadPaths],
  );

  const handleContextMenu = useCallback((event: React.MouseEvent) => {
    event.preventDefault();
    setContextMenuPosition({
      top: event.clientY,
      left: event.clientX,
    });
  }, []);

  const handleCloseContextMenu = useCallback(() => {
    setContextMenuPosition(null);
  }, []);

  const handleShowDetails = useCallback(() => {
    handleCloseContextMenu();
    if (selectedPaths.size === 0) return;
    setDetailTarget(Array.from(selectedPaths));
  }, [handleCloseContextMenu, selectedPaths]);

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
  }, [handleCloseContextMenu]);

  const handleCreateFolder = useCallback(() => {
    handleCloseContextMenu();
    setCreateFolderDialog(true);
  }, [handleCloseContextMenu]);

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
      const response = await axios.get("/navigator/api/resources/stat", {
        params: { path: selectedPath },
      });

      const stat = response.data;
      const mode = stat.mode || "0644"; // Default if not available
      const isDirectory = stat.isDir || hasDirectorySelected;
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
  }, [handleCloseContextMenu, selectedPaths, selectedItems]);

  const handleCopy = useCallback(() => {
    handleCloseContextMenu();
    if (selectedPaths.size === 0) return;
    setClipboard({
      paths: Array.from(selectedPaths),
      operation: "copy",
    });
    toast.success(`${selectedPaths.size} item(s) copied to clipboard`);
  }, [handleCloseContextMenu, selectedPaths]);

  const handleCut = useCallback(() => {
    handleCloseContextMenu();
    if (selectedPaths.size === 0) return;
    setClipboard({
      paths: Array.from(selectedPaths),
      operation: "cut",
    });
    toast.success(`${selectedPaths.size} item(s) cut to clipboard`);
  }, [handleCloseContextMenu, selectedPaths]);

  const handlePaste = useCallback(async () => {
    handleCloseContextMenu();
    if (!clipboard) {
      toast.error("Nothing to paste");
      return;
    }

    try {
      if (clipboard.operation === "copy") {
        await copyItems({
          sourcePaths: clipboard.paths,
          destinationDir: normalizedPath,
        });
      } else {
        await moveItems({
          sourcePaths: clipboard.paths,
          destinationDir: normalizedPath,
        });
        // Clear clipboard after cut operation
        setClipboard(null);
        setSelectedPaths(new Set());
      }
    } catch {
      // Error is handled by the mutation
    }
  }, [handleCloseContextMenu, clipboard, copyItems, moveItems, normalizedPath]);

  const handleDelete = useCallback(() => {
    handleCloseContextMenu();
    const paths = Array.from(selectedPaths);
    console.log("Delete clicked, selectedPaths:", paths);
    if (paths.length > 0) {
      setPendingDeletePaths(paths);
      setDeleteDialog(true);
    } else {
      toast.error("No items selected");
    }
  }, [handleCloseContextMenu, selectedPaths]);

  const handleConfirmDelete = useCallback(() => {
    if (!pendingDeletePaths.length) {
      return;
    }
    console.log("Confirming delete for paths:", pendingDeletePaths);
    deleteItems(pendingDeletePaths);
    setPendingDeletePaths([]);
  }, [deleteItems, pendingDeletePaths]);

  const handleCloseDeleteDialog = useCallback(() => {
    setDeleteDialog(false);
    setPendingDeletePaths([]);
  }, []);

  const handleDownloadSelected = useCallback(() => {
    handleCloseContextMenu();
    const paths = Array.from(selectedPaths);
    if (paths.length === 0) return;
    downloadPaths(paths);
  }, [handleCloseContextMenu, selectedPaths, downloadPaths]);

  const handleUpload = useCallback(() => {
    handleCloseContextMenu();
    // TODO: Implement upload dialog
    console.log("Upload clicked");
  }, [handleCloseContextMenu]);

  const handleCompressSelection = useCallback(async () => {
    handleCloseContextMenu();
    const paths = Array.from(selectedPaths);
    if (!paths.length) return;

    const baseName =
      selectedItems.length === 1
        ? stripArchiveExtension(selectedItems[0].name)
        : "archive";
    const archiveName = getUniqueName(
      ensureZipExtension(baseName || "archive"),
    );

    try {
      await compressItems({
        paths,
        archiveName,
        destination: normalizedPath,
      });
    } catch {
      // Errors are surfaced via toast in the mutation
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
  }, []);

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
    [permissionsDialog, changePermissions],
  );

  const handleEditFile = useCallback((filePath: string) => {
    setEditingPath(filePath);
    setDetailTarget(null); // Close the detail dialog
  }, []);

  const handleSaveFile = useCallback(async () => {
    if (!editorRef.current || !editingPath) return;

    try {
      setIsSavingFile(true);
      const content = editorRef.current.getContent();

      console.log(
        "Saving file:",
        editingPath,
        "Content length:",
        content.length,
      );

      const response = await axios.put("/navigator/api/resources", content, {
        params: { path: editingPath },
        headers: { "Content-Type": "text/plain" },
      });

      console.log("Save response:", response);
      toast.success("File saved successfully!");
      setIsEditorDirty(false);

      // Invalidate the file cache so it reloads with new content
      queryClient.invalidateQueries({
        queryKey: ["fileEdit", editingPath],
      });
    } catch (error: any) {
      console.error("Save error:", error);
      toast.error(error.response?.data?.error || "Failed to save file");
    } finally {
      setIsSavingFile(false);
    }
  }, [editingPath, queryClient]);

  const handleCloseEditor = useCallback(() => {
    if (isEditorDirty) {
      setCloseEditorDialog(true);
    } else {
      setEditingPath(null);
      setIsEditorDirty(false);
    }
  }, [isEditorDirty]);

  const handleKeepEditing = useCallback(() => {
    setCloseEditorDialog(false);
  }, []);

  const handleDiscardAndExit = useCallback(() => {
    setEditingPath(null);
    setIsEditorDirty(false);
    setCloseEditorDialog(false);
  }, []);

  const handleSaveAndExit = useCallback(async () => {
    if (!editorRef.current || !editingPath) return;

    try {
      setIsSavingFile(true);
      const content = editorRef.current.getContent();

      await axios.put("/navigator/api/resources", content, {
        params: { path: editingPath },
        headers: { "Content-Type": "text/plain" },
      });

      toast.success("File saved successfully!");
      setIsEditorDirty(false);
      setEditingPath(null);
      setCloseEditorDialog(false);

      queryClient.invalidateQueries({
        queryKey: ["fileEdit", editingPath],
      });
    } catch (error: any) {
      toast.error(error.response?.data?.error || "Failed to save file");
    } finally {
      setIsSavingFile(false);
    }
  }, [editingPath, queryClient]);

  const invalidateListing = useCallback(() => {
    queryClient.invalidateQueries({
      queryKey: ["fileResource", normalizedPath],
    });
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
  } = useDragAndDropUpload({
    normalizedPath,
    resource,
    editingPath,
    startUpload,
    onUploadComplete: invalidateListing,
  });

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
        />
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
              resource &&
              resource.type === "directory" && (
                <DirectoryListing
                  key={normalizedPath}
                  resource={resource}
                  showHiddenFiles={showHiddenFiles}
                  viewMode={viewMode}
                  sortField={sortField}
                  sortOrder={sortOrder}
                  onOpenDirectory={handleOpenDirectory}
                  onDownloadFile={handleDoubleClickFile}
                  selectedPaths={selectedPaths}
                  onSelectedPathsChange={setSelectedPaths}
                  isContextMenuOpen={Boolean(contextMenuPosition)}
                  onDelete={handleDelete}
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
      />

      <Dialog
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
      </Dialog>

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

      <PermissionsDialog
        open={Boolean(permissionsDialog)}
        pathLabel={permissionsDialog?.pathLabel || ""}
        selectionCount={permissionsDialog?.selectionCount || 0}
        currentMode={permissionsDialog?.mode || "0644"}
        isDirectory={permissionsDialog?.isDirectory || false}
        owner={permissionsDialog?.owner}
        group={permissionsDialog?.group}
        onClose={handleClosePermissionsDialog}
        onConfirm={handleConfirmPermissions}
      />

      <Dialog
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
      </Dialog>

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
