import { useCallback, useMemo, type MouseEvent } from "react";

import { useBackgroundTaskActions } from "@/tasks/useBackgroundTaskActions";
import { parentPath, stripTrailingSlash } from "@/utils/path";

import type { FileBrowserListingQueryOptions } from "./fileBrowserListingQueryOptions";
import { useFileBrowserArchiveActions } from "./useFileBrowserArchiveActions";
import { useFileBrowserClipboardShortcuts } from "./useFileBrowserClipboardShortcuts";
import { useFileBrowserEditorActions } from "./useFileBrowserEditorActions";
import { useFileBrowserFilteredResource } from "./useFileBrowserFilteredResource";
import { useFileBrowserItemActions } from "./useFileBrowserItemActions";
import { useFileBrowserNavigation } from "./useFileBrowserNavigation";
import { useFileBrowserUploadActions } from "./useFileBrowserUploadActions";
import { useFileConflictResolution } from "./useFileConflicts";
import { useFileDialogs } from "./useFileDialogs";
import { useFileDragAndDrop } from "./useFileDragAndDrop";
import { useFileEditor } from "./useFileEditor";
import { useFileMutations } from "./useFileMutations";
import { useFileQueries } from "./useFileQueries";
import { isSearchableQuery } from "./useFileSearch";
import { useFileSelection, useFileSelectionState } from "./useFileSelection";
import { useFileUpload } from "./useFileUpload";
import { useFileViewState } from "./useFileViewState";
import { useListingInvalidation } from "./useListingInvalidation";

export function useFileBrowserController(
  listingQueryOptions: FileBrowserListingQueryOptions,
) {
  // View slice: state plus a stable semantic-action API
  const view = useFileViewState();
  const {
    actions: viewActions,
    contextMenuPosition,
    searchCaseSensitive,
    searchQuery,
    showHiddenFiles,
    sortField,
    sortOrder,
    viewMode,
  } = view;
  // Dialogs slice: state plus a stable semantic-action API
  const dialogs = useFileDialogs();
  const {
    createFileDialog,
    deleteDialog,
    deleteMode,
    detailItems = [],
    detailTarget,
    pendingDeletePaths,
    permissionsDialog,
    permissionsDialogOpen,
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
  const { startDownload, startUpload } = useBackgroundTaskActions();

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
    createPending,
    createFolder,
    deleteItems,
    deletePending,
    deleteProgress,
    trashItems,
    trashPending,
    trashProgress,
    compressItems,
    extractArchive,
    changePermissions,
    permissionsPending,
    permissionsProgress,
    copyItems,
    moveItems,
    renameItem,
    renamePending,
    renameProgress,
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
    detailResource,
    detailError,
    statData,
    isStatLoading,
    multiItemsStats,
    editingFileResource,
    editingFileError,
    isEditingFileLoading,
    shouldShowDetailLoader,
  } = useFileQueries({
    detailItems,
    detailTarget,
    editingPath,
    hasSingleDetailTarget,
    hasMultipleDetailTargets,
    listingQueryOptions,
  });
  const { filteredResource, searchError, isSearchLoading } =
    useFileBrowserFilteredResource({
      caseSensitive: searchCaseSensitive,
      resource,
      searchQuery,
    });

  // Clipboard behaviors on top of the selection slice
  const { handleCopy, handleCut, handlePaste, selectedItems } =
    useFileSelection({
      resource: filteredResource,
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
    handleCloseDeleteDialog,
    handleCloseDetailDialog,
    handleClosePermissionsDialog,
    handleConfirmCreateFile,
    handleConfirmDelete,
    handleConfirmInlineRename,
    handleConfirmPermissions,
    handleContextMenuRename,
    handleCreateFile,
    handleCreateFolder,
    handleDelete,
    handleDeletePermanently,
    handleDoubleClickFile,
    handleDownloadCurrent,
    handleDownloadDetail,
    handleDownloadSelected,
    handleEditFile,
    handleOpenContainingFolder,
    handleShowDetails,
    handleStartInlineRename,
    renamingPath,
  } = useFileBrowserItemActions({
    changePermissions,
    permissionsPending,
    createFile,
    createFolder,
    deleteItems,
    deletePending,
    trashItems,
    trashPending,
    dialogs,
    editor,
    handleOpenDirectory,
    renameItem,
    renamePending,
    resource,
    selectedItems,
    selectedPaths,
    startDownload,
    view,
  });

  const handleNavigateUp = useCallback(() => {
    if (normalizedPath === "/") return;
    handleOpenDirectory(parentPath(normalizedPath));
  }, [handleOpenDirectory, normalizedPath]);

  // Dropping on a folder or breadcrumb moves (Ctrl: copies) into it. The
  // mutations own the conflict prompt, toasts and listing refresh.
  const handleDropItems = useCallback(
    async (paths: string[], destinationDir: string, copy: boolean) => {
      const payload = {
        sourcePaths: paths,
        destinationDir: stripTrailingSlash(destinationDir),
      };
      try {
        if (copy) {
          await copyItems(payload);
        } else {
          await moveItems(payload);
          selectionActions.clear();
        }
      } catch {
        // Error is handled by the mutation
      }
    },
    [copyItems, moveItems, selectionActions],
  );

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
    handleOverwriteConflict,
    handleReloadConflict,
    handleSaveAndExit,
    handleSaveContent,
    handleSaveFile,
    saveConflict,
    handleCancelConflict,
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
  const searchActive = isSearchableQuery(searchQuery);

  const editorDialogs = useMemo(
    () => ({
      closeEditorDialog,
      editingFileResource,
      editingFileError,
      editingPath,
      editorRef,
      isDirty: isEditorDirty,
      isEditingFileLoading,
      isSaving: isSavingFile,
      onClose: handleCloseEditor,
      onCancelConflict: handleCancelConflict,
      onDirtyChange: editorActions.setDirty,
      onDiscardAndExit: handleDiscardAndExit,
      onKeepEditing: handleKeepEditing,
      onOverwriteConflict: handleOverwriteConflict,
      onReloadConflict: handleReloadConflict,
      onRequestSave: handleSaveFile,
      onSaveContent: handleSaveContent,
      onSaveAndExit: handleSaveAndExit,
      saveConflict,
      onSearchChange: viewActions.setSearch,
      onSwitchView: viewActions.switchView,
      onToggleHiddenFiles: viewActions.toggleHiddenFiles,
      searchQuery,
      showHiddenFiles,
      showQuickSave,
      viewMode,
    }),
    [
      closeEditorDialog,
      editingFileResource,
      editingFileError,
      editingPath,
      editorActions,
      editorRef,
      handleCloseEditor,
      handleCancelConflict,
      handleDiscardAndExit,
      handleKeepEditing,
      handleOverwriteConflict,
      handleReloadConflict,
      handleSaveAndExit,
      handleSaveContent,
      handleSaveFile,
      isEditorDirty,
      isEditingFileLoading,
      isSavingFile,
      saveConflict,
      searchQuery,
      showHiddenFiles,
      showQuickSave,
      viewActions,
      viewMode,
    ],
  );

  const contextMenuDialogs = useMemo(
    () => ({
      anchorPosition: contextMenuPosition,
      canCompress: canCompressSelection,
      canExtract: canExtractSelection,
      canOpenContainingFolder: searchActive && selectedPathsCount === 1,
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
      onDeletePermanently: handleDeletePermanently,
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
      handleDeletePermanently,
      handleDownloadSelected,
      handleExtractSelection,
      handleOpenContainingFolder,
      handlePaste,
      handleShowDetails,
      handleUpload,
      searchActive,
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
      isStatLoading,
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
      isStatLoading,
      multiItemsStats,
      shouldShowDetailLoader,
      statData,
    ],
  );

  const createDialogs = useMemo(
    () => ({
      fileOpen: createFileDialog,
      filePending: createPending === "file",
      onCloseFile: handleCloseCreateFileDialog,
      onConfirmFile: handleConfirmCreateFile,
    }),
    [
      createPending,
      createFileDialog,
      handleCloseCreateFileDialog,
      handleConfirmCreateFile,
    ],
  );

  const deleteDialogProps = useMemo(
    () => ({
      onClose: handleCloseDeleteDialog,
      onConfirm: handleConfirmDelete,
      mode: deleteMode,
      open: deleteDialog,
      isPending: deleteMode === "trash" ? trashPending : deletePending,
      pendingDeletePaths,
      progress: deleteMode === "trash" ? trashProgress : deleteProgress,
    }),
    [
      deleteDialog,
      deleteMode,
      deletePending,
      deleteProgress,
      trashPending,
      trashProgress,
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
      onExited: dialogs.actions.clearPermissions,
      open: permissionsDialogOpen,
      isPending: permissionsPending,
      progress: permissionsProgress,
    }),
    [
      handleClosePermissionsDialog,
      handleConfirmPermissions,
      dialogs.actions.clearPermissions,
      permissionsDialog,
      permissionsDialogOpen,
      permissionsPending,
      permissionsProgress,
    ],
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
      onConfirmCompressFormat: handleCompressConfirm,
    }),
    [
      compressFormatDialog,
      handleCloseCompressFormatDialog,
      handleCompressConfirm,
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
      isSavingFile,
      normalizedPath,
      onDropItems: handleDropItems,
      onOpenDirectory: handleOpenDirectory,
      onSearchCaseSensitiveChange: viewActions.setSearchCaseSensitive,
      onSearchChange: viewActions.setSearch,
      onSortChange: viewActions.changeSort,
      onSwitchView: viewActions.switchView,
      onToggleHiddenFiles: viewActions.toggleHiddenFiles,
      searchQuery,
      searchCaseSensitive,
      showHiddenFiles,
      sortOrder,
      viewMode,
    }),
    [
      editingPath,
      handleDropItems,
      handleOpenDirectory,
      isSavingFile,
      normalizedPath,
      searchCaseSensitive,
      searchQuery,
      showHiddenFiles,
      sortOrder,
      viewActions,
      viewMode,
    ],
  );

  const contentData = useMemo(
    () => ({
      filteredResource,
      searchError,
      isSearchLoading,
      resource,
    }),
    [filteredResource, searchError, isSearchLoading, resource],
  );

  const contentListing = useMemo(
    () => ({
      contextMenuOpen: Boolean(contextMenuPosition),
      cutPaths,
      onCancelRename: handleCancelInlineRename,
      onConfirmRename: handleConfirmInlineRename,
      onDelete: handleDelete,
      onDeletePermanently: handleDeletePermanently,
      onDownloadFile: handleDoubleClickFile,
      onDropItems: handleDropItems,
      onNavigateUp: handleNavigateUp,
      onOpenDirectory: handleOpenDirectory,
      onSelectedPathsChange: selectionActions.select,
      onStartRename: handleStartInlineRename,
      renamePendingPath: renamePending ? renamingPath : null,
      renameProgressPct: renameProgress?.percentage,
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
      handleDeletePermanently,
      handleDoubleClickFile,
      handleDropItems,
      handleNavigateUp,
      handleOpenDirectory,
      handleStartInlineRename,
      renamingPath,
      renamePending,
      renameProgress,
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
