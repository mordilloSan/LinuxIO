import { Icon } from "@iconify/react";
import { useCallback, useMemo, type MouseEvent, type ReactNode } from "react";

import type { FileBrowserContentProps } from "@/components/filebrowser/FileBrowserContent";
import type { FileBrowserDialogsProps } from "@/components/filebrowser/FileBrowserDialogs";
import { useBackgroundJobActions } from "@/hooks/backgroundJobs/useBackgroundJobActions";
import { useFileBrowserArchiveActions } from "@/hooks/filebrowser/useFileBrowserArchiveActions";
import { useFileBrowserClipboardShortcuts } from "@/hooks/filebrowser/useFileBrowserClipboardShortcuts";
import { useFileBrowserEditorActions } from "@/hooks/filebrowser/useFileBrowserEditorActions";
import { useFileBrowserFilteredResource } from "@/hooks/filebrowser/useFileBrowserFilteredResource";
import { useFileBrowserItemActions } from "@/hooks/filebrowser/useFileBrowserItemActions";
import { useFileBrowserNavigation } from "@/hooks/filebrowser/useFileBrowserNavigation";
import { useFileBrowserUploadActions } from "@/hooks/filebrowser/useFileBrowserUploadActions";
import { useFileConflictResolution } from "@/hooks/filebrowser/useFileConflicts";
import { useFileDialogs } from "@/hooks/filebrowser/useFileDialogs";
import { useFileDragAndDrop } from "@/hooks/filebrowser/useFileDragAndDrop";
import { useFileEditor } from "@/hooks/filebrowser/useFileEditor";
import { useFileMutations } from "@/hooks/filebrowser/useFileMutations";
import { useFileQueries } from "@/hooks/filebrowser/useFileQueries";
import {
  useFileSelection,
  useFileSelectionState,
} from "@/hooks/filebrowser/useFileSelection";
import { useFileUpload } from "@/hooks/filebrowser/useFileUpload";
import { useFileViewState } from "@/hooks/filebrowser/useFileViewState";
import { useListingInvalidation } from "@/hooks/filebrowser/useListingInvalidation";
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
  // View slice: state plus a stable semantic-action API
  const view = useFileViewState();
  const {
    actions: viewActions,
    contextMenuPosition,
    searchQuery,
    showHiddenFiles,
    sortField,
    sortOrder,
    viewMode,
  } = view;
  const viewIcon = useMemo(() => viewIconMap[viewMode], [viewMode]);

  // Dialogs slice: state plus a stable semantic-action API
  const dialogs = useFileDialogs();
  const {
    createFileDialog,
    createFolderDialog,
    deleteDialog,
    detailTarget,
    pendingDeletePaths,
    permissionsDialog,
  } = dialogs;

  // Editor slice: state plus a stable semantic-action API
  const editor = useFileEditor();
  const {
    actions: editorActions,
    closeEditorDialog,
    editingPath,
    editorRef,
    isEditorDirty,
    isSavingFile,
    showQuickSave,
  } = editor;

  // Upload slice: state plus a stable semantic-action API
  const upload = useFileUpload();
  const {
    fileInputRef,
    folderInputRef,
    isUploadProcessing,
    uploadDialogOpen,
    uploadEntries,
    uploadSummary,
  } = upload;

  const { handleOpenDirectory, normalizedPath } = useFileBrowserNavigation({
    onPathChange: viewActions.clearSearch,
  });

  // Selection is directory-scoped; clipboard state survives navigation.
  const selection = useFileSelectionState(normalizedPath);
  const {
    actions: selectionActions,
    clipboard,
    cutPaths,
    selectedPaths,
  } = selection;
  const { startDownload, startUpload } = useBackgroundJobActions();
  const { isEnabled: indexerEnabled, status: indexerStatus } =
    useCapability("indexerAvailable");

  // Collision handling shared by paste and uploads: pre-check destinations,
  // prompt per conflicting item, never overwrite without an explicit choice.
  const {
    applyConflictDecisions,
    cancelConflictPrompt,
    conflictPrompt,
    resolveCollisions,
  } = useFileConflictResolution();

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
    onDeleteSuccess: selectionActions.clear,
    resolveCollisions,
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

  // Clipboard behaviors on top of the selection slice
  const { handleCopy, handleCut, handlePaste, selectedItems } =
    useFileSelection({
      resource,
      normalizedPath,
      copyItems,
      moveItems,
      onContextMenuClose: viewActions.closeContextMenu,
      selection,
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
    dialogs,
    editor,
    handleOpenDirectory,
    renameItem,
    resource,
    selectedItems,
    selectedPaths,
    startDownload,
    view,
  });

  useFileBrowserClipboardShortcuts({
    editingPath,
    onCopy: handleCopy,
    onCut: handleCut,
    onPaste: handlePaste,
    renamingPath,
  });

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
    onContextMenuClose: viewActions.closeContextMenu,
    resource,
    selectedItems,
    selectedPaths,
  });

  const handleContextMenu = useCallback(
    (event: MouseEvent) => {
      event.preventDefault();
      viewActions.openContextMenu({
        top: event.clientY,
        left: event.clientX,
      });
    },
    [viewActions],
  );

  const {
    handleCloseEditor,
    handleDiscardAndExit,
    handleKeepEditing,
    handleSaveAndExit,
    handleSaveContent,
    handleSaveFile,
  } = useFileBrowserEditorActions({ editor });
  const invalidateListing = useListingInvalidation(normalizedPath);
  const {
    isDragOver,
    handleDragEnter,
    handleDragOver,
    handleDragLeave,
    handleDrop,
  } = useFileDragAndDrop({
    normalizedPath,
    resource,
    editingPath,
    resolveCollisions,
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
    invalidateListing,
    normalizedPath,
    onContextMenuClose: viewActions.closeContextMenu,
    resolveCollisions,
    startUpload,
    upload,
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
      onDirtyChange: editorActions.setDirty,
      onDiscardAndExit: handleDiscardAndExit,
      onKeepEditing: handleKeepEditing,
      onRequestSave: handleSaveFile,
      onSaveContent: handleSaveContent,
      onSaveAndExit: handleSaveAndExit,
      onSearchChange: viewActions.setSearch,
      onSwitchView: viewActions.switchView,
      onToggleHiddenFiles: viewActions.toggleHiddenFiles,
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
      editorActions,
      editorRef,
      handleCloseEditor,
      handleDiscardAndExit,
      handleKeepEditing,
      handleSaveAndExit,
      handleSaveContent,
      handleSaveFile,
      isEditorDirty,
      isEditingFileLoading,
      isSavingFile,
      searchQuery,
      showHiddenFiles,
      showQuickSave,
      viewActions,
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
      onClose: viewActions.closeContextMenu,
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
      viewActions,
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
      onConfirmUnsupportedEdit: handleConfirmUnsupportedEdit,
      unsupportedEditPath,
    }),
    [
      compressFormatDialog,
      handleCloseCompressFormatDialog,
      handleCloseUnsupportedEditDialog,
      handleCompressConfirm,
      handleConfirmUnsupportedEdit,
      unsupportedEditPath,
    ],
  );

  const conflictDialog = useMemo(
    () => ({
      onCancel: cancelConflictPrompt,
      onResolve: applyConflictDecisions,
      prompt: conflictPrompt,
    }),
    [applyConflictDecisions, cancelConflictPrompt, conflictPrompt],
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
      onSearchChange: viewActions.setSearch,
      onSortChange: viewActions.changeSort,
      onSwitchView: viewActions.switchView,
      onToggleHiddenFiles: viewActions.toggleHiddenFiles,
      searchQuery,
      showHiddenFiles,
      sortOrder,
      viewIcon,
      viewMode,
    }),
    [
      editingPath,
      handleOpenDirectory,
      indexerEnabled,
      indexerStatus,
      isSavingFile,
      normalizedPath,
      searchQuery,
      showHiddenFiles,
      sortOrder,
      viewActions,
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
      onSelectedPathsChange: selectionActions.select,
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
      selectionActions,
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
      conflict: conflictDialog,
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
      conflictDialog,
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
