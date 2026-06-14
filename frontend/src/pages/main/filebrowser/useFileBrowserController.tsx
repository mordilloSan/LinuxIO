import { Icon } from "@iconify/react";
import { useQueryClient } from "@tanstack/react-query";
import React, { ReactNode, useCallback, useMemo } from "react";

import type { FileBrowserContentProps } from "./FileBrowserContent";
import type { FileBrowserDialogsProps } from "./FileBrowserDialogs";

import { linuxio } from "@/api";
import { useBackgroundJobActions } from "@/hooks/backgroundJobs/useBackgroundJobActions";
import { useFileBrowserArchiveActions } from "@/hooks/filebrowser/useFileBrowserArchiveActions";
import { useFileBrowserClipboardShortcuts } from "@/hooks/filebrowser/useFileBrowserClipboardShortcuts";
import { useFileBrowserEditorActions } from "@/hooks/filebrowser/useFileBrowserEditorActions";
import { useFileBrowserFilteredResource } from "@/hooks/filebrowser/useFileBrowserFilteredResource";
import { useFileBrowserItemActions } from "@/hooks/filebrowser/useFileBrowserItemActions";
import { useFileBrowserNavigation } from "@/hooks/filebrowser/useFileBrowserNavigation";
import { useFileBrowserSearchAndSort } from "@/hooks/filebrowser/useFileBrowserSearchAndSort";
import { useFileBrowserUploadActions } from "@/hooks/filebrowser/useFileBrowserUploadActions";
import { useFileDialogs } from "@/hooks/filebrowser/useFileDialogs";
import { useFileDragAndDrop } from "@/hooks/filebrowser/useFileDragAndDrop";
import { useFileEditor } from "@/hooks/filebrowser/useFileEditor";
import { useFileMutations } from "@/hooks/filebrowser/useFileMutations";
import { useFileQueries } from "@/hooks/filebrowser/useFileQueries";
import { useFileSelection } from "@/hooks/filebrowser/useFileSelection";
import { clearFileSubfoldersCache } from "@/hooks/filebrowser/useFileSubfolders";
import { useFileUpload } from "@/hooks/filebrowser/useFileUpload";
import { useFileViewState } from "@/hooks/filebrowser/useFileViewState";
import { useCapability } from "@/hooks/useCapabilities";
import { ViewMode } from "@/types/filebrowser";

const viewIconMap: Record<ViewMode, ReactNode> = {
  card: <Icon height={20} icon="mdi:card-multiple" width={20} />,
  list: <Icon height={20} icon="mdi:view-list" width={20} />,
};

export interface FileBrowserController {
  contentProps: FileBrowserContentProps;
  dialogsProps: FileBrowserDialogsProps;
}

export function useFileBrowserController(): FileBrowserController {
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
  const {
    handlePathChange,
    handleSearchChange,
    handleSortChange,
    searchQuery,
    setSearchQuery,
  } = useFileBrowserSearchAndSort({
    setSortField,
    setSortOrder,
  });
  const { handleOpenDirectory, normalizedPath } = useFileBrowserNavigation({
    onPathChange: handlePathChange,
  });
  const queryClient = useQueryClient();
  const { startDownload, startUpload } = useBackgroundJobActions();
  const { isEnabled: indexerEnabled, status: indexerStatus } =
    useCapability("indexerAvailable");

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
  const filteredResource = useFileBrowserFilteredResource({
    resource,
    searchQuery,
  });

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

  const {
    canShowDetails,
    handleCancelInlineRename,
    handleChangePermissions,
    handleCloseCreateFileDialog,
    handleCloseCreateFolderDialog,
    handleCloseDeleteDialog,
    handleCloseDetailDialog,
    handleClosePermissionsDialog,
    handleCloseUnsupportedEditDialog,
    handleConfirmCreateFile,
    handleConfirmCreateFolder,
    handleConfirmDelete,
    handleConfirmInlineRename,
    handleConfirmPermissions,
    handleConfirmUnsupportedEdit,
    handleContextMenuRename,
    handleCreateFile,
    handleCreateFolder,
    handleDelete,
    handleDoubleClickFile,
    handleDownloadCurrent,
    handleDownloadDetail,
    handleDownloadSelected,
    handleEditFile,
    handleOpenContainingFolder,
    handleShowDetails,
    handleStartInlineRename,
    renamingPath,
    unsupportedEditPath,
  } = useFileBrowserItemActions({
    changePermissions,
    createFile,
    createFolder,
    deleteItems,
    handleOpenDirectory,
    onContextMenuClose: handleCloseContextMenu,
    pendingDeletePaths,
    permissionsDialog,
    renameItem,
    resource,
    selectedItems,
    selectedPaths,
    setCreateFileDialog,
    setCreateFolderDialog,
    setDeleteDialog,
    setDetailTarget,
    setEditingPath,
    setPendingDeletePaths,
    setPermissionsDialog,
    setSearchQuery,
    startDownload,
  });

  useFileBrowserClipboardShortcuts({
    editingPath,
    onCopy: handleCopy,
    onCut: handleCut,
    onPaste: handlePaste,
    renamingPath,
  });

  // Derive cut paths from clipboard for visual dimming
  const cutPaths = useMemo(() => {
    if (clipboard?.operation === "cut") {
      return new Set(clipboard.paths);
    }
    return new Set<string>();
  }, [clipboard]);

  const {
    canCompressSelection,
    canExtractSelection,
    compressFormatDialog,
    handleCloseCompressFormatDialog,
    handleCompressConfirm,
    handleCompressSelection,
    handleExtractSelection,
  } = useFileBrowserArchiveActions({
    compressItems,
    extractArchive,
    normalizedPath,
    onContextMenuClose: handleCloseContextMenu,
    resource,
    selectedItems,
    selectedPaths,
  });

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

  const {
    handleCloseEditor,
    handleDiscardAndExit,
    handleKeepEditing,
    handleSaveAndExit,
    handleSaveFile,
  } = useFileBrowserEditorActions({
    editingPath,
    editorRef,
    isEditorDirty,
    setCloseEditorDialog,
    setEditingPath,
    setIsEditorDirty,
    setIsSavingFile,
  });
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

  const {
    handleClearUploadSelection,
    handleCloseUploadDialog,
    handlePickFiles,
    handlePickFolder,
    handleStartUpload,
    handleUpload,
    handleUploadInputChange,
  } = useFileBrowserUploadActions({
    fileInputRef,
    folderInputRef,
    invalidateListing,
    isUploadProcessing,
    normalizedPath,
    onContextMenuClose: handleCloseContextMenu,
    setIsUploadProcessing,
    setOverwriteTargets: setOverwriteTargetsForDialog,
    setUploadDialogOpen,
    setUploadEntries,
    startUpload,
    uploadEntries,
  });

  const selectedPathsCount = selectedPaths.size;
  const clipboardAvailable = clipboard !== null;

  const editorDialogs = useMemo(
    () => ({
      closeEditorDialog,
      editingFileResource,
      editingPath,
      editorRef,
      isDirty: isEditorDirty,
      isEditingFileLoading,
      isSaving: isSavingFile,
      onClose: handleCloseEditor,
      onDirtyChange: setIsEditorDirty,
      onDiscardAndExit: handleDiscardAndExit,
      onKeepEditing: handleKeepEditing,
      onSave: handleSaveFile,
      onSaveAndExit: handleSaveAndExit,
      onSearchChange: handleSearchChange,
      onSwitchView: handleSwitchView,
      onToggleHiddenFiles: handleToggleHiddenFiles,
      searchQuery,
      showHiddenFiles,
      showQuickSave,
      viewIcon,
      viewMode,
    }),
    [
      closeEditorDialog,
      editingFileResource,
      editingPath,
      editorRef,
      handleCloseEditor,
      handleDiscardAndExit,
      handleKeepEditing,
      handleSaveAndExit,
      handleSaveFile,
      handleSearchChange,
      handleSwitchView,
      handleToggleHiddenFiles,
      isEditorDirty,
      isEditingFileLoading,
      isSavingFile,
      searchQuery,
      setIsEditorDirty,
      showHiddenFiles,
      showQuickSave,
      viewIcon,
      viewMode,
    ],
  );

  const contextMenuDialogs = useMemo(
    () => ({
      anchorPosition: contextMenuPosition,
      canCompress: canCompressSelection,
      canExtract: canExtractSelection,
      canOpenContainingFolder: Boolean(searchQuery) && selectedPathsCount === 1,
      canRename: selectedPathsCount === 1,
      canShowDetails,
      hasClipboard: clipboardAvailable,
      hasSelection: selectedPathsCount > 0,
      onChangePermissions: handleChangePermissions,
      onClose: handleCloseContextMenu,
      onCompress: handleCompressSelection,
      onCopy: handleCopy,
      onCreateFile: handleCreateFile,
      onCreateFolder: handleCreateFolder,
      onCut: handleCut,
      onDelete: handleDelete,
      onDownload: handleDownloadSelected,
      onExtract: handleExtractSelection,
      onOpenContainingFolder: handleOpenContainingFolder,
      onPaste: handlePaste,
      onRename: handleContextMenuRename,
      onShowDetails: handleShowDetails,
      onUpload: handleUpload,
    }),
    [
      canCompressSelection,
      canExtractSelection,
      canShowDetails,
      clipboardAvailable,
      contextMenuPosition,
      handleChangePermissions,
      handleCloseContextMenu,
      handleCompressSelection,
      handleContextMenuRename,
      handleCopy,
      handleCreateFile,
      handleCreateFolder,
      handleCut,
      handleDelete,
      handleDownloadSelected,
      handleExtractSelection,
      handleOpenContainingFolder,
      handlePaste,
      handleShowDetails,
      handleUpload,
      searchQuery,
      selectedPathsCount,
    ],
  );

  const detailsDialogs = useMemo(
    () => ({
      detailError,
      detailResource,
      detailTarget,
      hasMultipleTargets: hasMultipleDetailTargets,
      hasSingleTarget: hasSingleDetailTarget,
      isStatPending,
      multiItemsStats,
      onClose: handleCloseDetailDialog,
      onDownload: handleDownloadDetail,
      onEdit: handleEditFile,
      shouldShowLoader: shouldShowDetailLoader,
      statData,
    }),
    [
      detailError,
      detailResource,
      detailTarget,
      handleCloseDetailDialog,
      handleDownloadDetail,
      handleEditFile,
      hasMultipleDetailTargets,
      hasSingleDetailTarget,
      isStatPending,
      multiItemsStats,
      shouldShowDetailLoader,
      statData,
    ],
  );

  const createDialogs = useMemo(
    () => ({
      fileOpen: createFileDialog,
      folderOpen: createFolderDialog,
      onCloseFile: handleCloseCreateFileDialog,
      onCloseFolder: handleCloseCreateFolderDialog,
      onConfirmFile: handleConfirmCreateFile,
      onConfirmFolder: handleConfirmCreateFolder,
    }),
    [
      createFileDialog,
      createFolderDialog,
      handleCloseCreateFileDialog,
      handleCloseCreateFolderDialog,
      handleConfirmCreateFile,
      handleConfirmCreateFolder,
    ],
  );

  const deleteDialogProps = useMemo(
    () => ({
      onClose: handleCloseDeleteDialog,
      onConfirm: handleConfirmDelete,
      open: deleteDialog,
      pendingDeletePaths,
    }),
    [
      deleteDialog,
      handleCloseDeleteDialog,
      handleConfirmDelete,
      pendingDeletePaths,
    ],
  );

  const permissionsDialogs = useMemo(
    () => ({
      dialog: permissionsDialog,
      onClose: handleClosePermissionsDialog,
      onConfirm: handleConfirmPermissions,
    }),
    [handleClosePermissionsDialog, handleConfirmPermissions, permissionsDialog],
  );

  const uploadDialogs = useMemo(
    () => ({
      entries: uploadEntries,
      fileInputRef,
      folderInputRef,
      isProcessing: isUploadProcessing,
      normalizedPath,
      onChangeInput: handleUploadInputChange,
      onClearSelection: handleClearUploadSelection,
      onClose: handleCloseUploadDialog,
      onPickFiles: handlePickFiles,
      onPickFolder: handlePickFolder,
      onStart: handleStartUpload,
      open: uploadDialogOpen,
      summary: uploadSummary,
    }),
    [
      fileInputRef,
      folderInputRef,
      handleClearUploadSelection,
      handleCloseUploadDialog,
      handlePickFiles,
      handlePickFolder,
      handleStartUpload,
      handleUploadInputChange,
      isUploadProcessing,
      normalizedPath,
      uploadDialogOpen,
      uploadEntries,
      uploadSummary,
    ],
  );

  const archiveDialogs = useMemo(
    () => ({
      compressFormatDialog,
      onCloseCompressFormatDialog: handleCloseCompressFormatDialog,
      onCloseUnsupportedEditDialog: handleCloseUnsupportedEditDialog,
      onConfirmCompressFormat: handleCompressConfirm,
      onConfirmOverwrite: handleConfirmOverwrite,
      onConfirmUnsupportedEdit: handleConfirmUnsupportedEdit,
      onOverwriteCancel: handleCancelOverwrite,
      overwriteTargets,
      unsupportedEditPath,
    }),
    [
      compressFormatDialog,
      handleCancelOverwrite,
      handleCloseCompressFormatDialog,
      handleCloseUnsupportedEditDialog,
      handleCompressConfirm,
      handleConfirmOverwrite,
      handleConfirmUnsupportedEdit,
      overwriteTargets,
      unsupportedEditPath,
    ],
  );

  const contentSurface = useMemo(
    () => ({
      isDragOver,
      onContextMenu: handleContextMenu,
      onDragEnter: handleDragEnter,
      onDragLeave: handleDragLeave,
      onDragOver: handleDragOver,
      onDrop: handleDrop,
    }),
    [
      handleContextMenu,
      handleDragEnter,
      handleDragLeave,
      handleDragOver,
      handleDrop,
      isDragOver,
    ],
  );

  const contentChrome = useMemo(
    () => ({
      editingPath,
      indexerEnabled,
      indexerStatus,
      isSavingFile,
      normalizedPath,
      onOpenDirectory: handleOpenDirectory,
      onSearchChange: handleSearchChange,
      onSortChange: handleSortChange,
      onSwitchView: handleSwitchView,
      onToggleHiddenFiles: handleToggleHiddenFiles,
      searchQuery,
      showHiddenFiles,
      sortOrder,
      viewIcon,
      viewMode,
    }),
    [
      editingPath,
      handleOpenDirectory,
      handleSearchChange,
      handleSortChange,
      handleSwitchView,
      handleToggleHiddenFiles,
      indexerEnabled,
      indexerStatus,
      isSavingFile,
      normalizedPath,
      searchQuery,
      showHiddenFiles,
      sortOrder,
      viewIcon,
      viewMode,
    ],
  );

  const contentData = useMemo(
    () => ({
      errorMessage,
      filteredResource,
      isPending,
      resource,
    }),
    [errorMessage, filteredResource, isPending, resource],
  );

  const contentListing = useMemo(
    () => ({
      contextMenuOpen: Boolean(contextMenuPosition),
      cutPaths,
      onCancelRename: handleCancelInlineRename,
      onConfirmRename: handleConfirmInlineRename,
      onDelete: handleDelete,
      onDownloadFile: handleDoubleClickFile,
      onOpenDirectory: handleOpenDirectory,
      onSelectedPathsChange: setSelectedPaths,
      onStartRename: handleStartInlineRename,
      renamingPath,
      selectedPaths,
      showHiddenFiles,
      sortField,
      sortOrder,
      viewMode,
    }),
    [
      contextMenuPosition,
      cutPaths,
      handleCancelInlineRename,
      handleConfirmInlineRename,
      handleDelete,
      handleDoubleClickFile,
      handleOpenDirectory,
      handleStartInlineRename,
      renamingPath,
      selectedPaths,
      setSelectedPaths,
      showHiddenFiles,
      sortField,
      sortOrder,
      viewMode,
    ],
  );

  const contentFile = useMemo(
    () => ({
      onDownloadCurrent: handleDownloadCurrent,
      onEditFile: handleEditFile,
    }),
    [handleDownloadCurrent, handleEditFile],
  );

  const contentProps = useMemo(
    () => ({
      chrome: contentChrome,
      data: contentData,
      file: contentFile,
      listing: contentListing,
      surface: contentSurface,
    }),
    [contentChrome, contentData, contentFile, contentListing, contentSurface],
  );

  const dialogsProps = useMemo(
    () => ({
      archive: archiveDialogs,
      contextMenu: contextMenuDialogs,
      create: createDialogs,
      deleteDialog: deleteDialogProps,
      details: detailsDialogs,
      editor: editorDialogs,
      permissions: permissionsDialogs,
      upload: uploadDialogs,
    }),
    [
      archiveDialogs,
      contextMenuDialogs,
      createDialogs,
      deleteDialogProps,
      detailsDialogs,
      editorDialogs,
      permissionsDialogs,
      uploadDialogs,
    ],
  );

  return { contentProps, dialogsProps };
}
