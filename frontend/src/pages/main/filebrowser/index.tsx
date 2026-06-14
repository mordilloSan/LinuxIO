import { Icon } from "@iconify/react";
import { useQueryClient } from "@tanstack/react-query";
import React, { ReactNode, useCallback, useMemo } from "react";

import FileBrowserContent from "./FileBrowserContent";
import FileBrowserDialogs from "./FileBrowserDialogs";

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
const FileBrowser: React.FC = () => {
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
  return (
    <>
      <FileBrowserContent
        contextMenuOpen={Boolean(contextMenuPosition)}
        cutPaths={cutPaths}
        editingPath={editingPath}
        errorMessage={errorMessage}
        filteredResource={filteredResource}
        indexerEnabled={indexerEnabled}
        indexerStatus={indexerStatus}
        isDragOver={isDragOver}
        isPending={isPending}
        isSavingFile={isSavingFile}
        normalizedPath={normalizedPath}
        onCancelRename={handleCancelInlineRename}
        onConfirmRename={handleConfirmInlineRename}
        onContextMenu={handleContextMenu}
        onDelete={handleDelete}
        onDownloadCurrent={handleDownloadCurrent}
        onDownloadFile={handleDoubleClickFile}
        onDragEnter={handleDragEnter}
        onDragLeave={handleDragLeave}
        onDragOver={handleDragOver}
        onDrop={handleDrop}
        onEditFile={handleEditFile}
        onOpenDirectory={handleOpenDirectory}
        onSearchChange={handleSearchChange}
        onSelectedPathsChange={setSelectedPaths}
        onSortChange={handleSortChange}
        onStartRename={handleStartInlineRename}
        onSwitchView={handleSwitchView}
        onToggleHiddenFiles={handleToggleHiddenFiles}
        renamingPath={renamingPath}
        resource={resource}
        searchQuery={searchQuery}
        selectedPaths={selectedPaths}
        showHiddenFiles={showHiddenFiles}
        sortField={sortField}
        sortOrder={sortOrder}
        viewIcon={viewIcon}
        viewMode={viewMode}
      />

      <FileBrowserDialogs
        canCompressSelection={canCompressSelection}
        canExtractSelection={canExtractSelection}
        canShowDetails={canShowDetails}
        clipboardAvailable={clipboard !== null}
        closeEditorDialog={closeEditorDialog}
        compressFormatDialog={compressFormatDialog}
        contextMenuPosition={contextMenuPosition}
        createFileDialog={createFileDialog}
        createFolderDialog={createFolderDialog}
        deleteDialog={deleteDialog}
        detailError={detailError}
        detailResource={detailResource}
        detailTarget={detailTarget}
        editingFileResource={editingFileResource}
        editingPath={editingPath}
        editorRef={editorRef}
        fileInputRef={fileInputRef}
        folderInputRef={folderInputRef}
        hasMultipleDetailTargets={hasMultipleDetailTargets}
        hasSingleDetailTarget={hasSingleDetailTarget}
        isEditorDirty={isEditorDirty}
        isEditingFileLoading={isEditingFileLoading}
        isSavingFile={isSavingFile}
        isStatPending={isStatPending}
        isUploadProcessing={isUploadProcessing}
        multiItemsStats={multiItemsStats}
        normalizedPath={normalizedPath}
        onChangePermissions={handleChangePermissions}
        onChangeUploadInput={handleUploadInputChange}
        onClearUploadSelection={handleClearUploadSelection}
        onCloseCompressFormatDialog={handleCloseCompressFormatDialog}
        onCloseContextMenu={handleCloseContextMenu}
        onCloseCreateFileDialog={handleCloseCreateFileDialog}
        onCloseCreateFolderDialog={handleCloseCreateFolderDialog}
        onCloseDeleteDialog={handleCloseDeleteDialog}
        onCloseDetailDialog={handleCloseDetailDialog}
        onCloseEditor={handleCloseEditor}
        onClosePermissionsDialog={handleClosePermissionsDialog}
        onCloseUnsupportedEditDialog={handleCloseUnsupportedEditDialog}
        onCloseUploadDialog={handleCloseUploadDialog}
        onCompressSelection={handleCompressSelection}
        onConfirmCompressFormat={handleCompressConfirm}
        onConfirmCreateFile={handleConfirmCreateFile}
        onConfirmCreateFolder={handleConfirmCreateFolder}
        onConfirmDelete={handleConfirmDelete}
        onConfirmOverwrite={handleConfirmOverwrite}
        onConfirmPermissions={handleConfirmPermissions}
        onConfirmUnsupportedEdit={handleConfirmUnsupportedEdit}
        onCopy={handleCopy}
        onCreateFile={handleCreateFile}
        onCreateFolder={handleCreateFolder}
        onCut={handleCut}
        onDelete={handleDelete}
        onDirtyChange={setIsEditorDirty}
        onDiscardAndExit={handleDiscardAndExit}
        onDownloadDetail={handleDownloadDetail}
        onDownloadSelected={handleDownloadSelected}
        onEditFile={handleEditFile}
        onExtractSelection={handleExtractSelection}
        onKeepEditing={handleKeepEditing}
        onOpenContainingFolder={handleOpenContainingFolder}
        onOverwriteCancel={handleCancelOverwrite}
        onPaste={handlePaste}
        onPickFiles={handlePickFiles}
        onPickFolder={handlePickFolder}
        onRename={handleContextMenuRename}
        onSaveAndExit={handleSaveAndExit}
        onSaveFile={handleSaveFile}
        onSearchChange={handleSearchChange}
        onShowDetails={handleShowDetails}
        onStartUpload={handleStartUpload}
        onSwitchView={handleSwitchView}
        onToggleHiddenFiles={handleToggleHiddenFiles}
        onUpload={handleUpload}
        overwriteTargets={overwriteTargets}
        pendingDeletePaths={pendingDeletePaths}
        permissionsDialog={permissionsDialog}
        searchQuery={searchQuery}
        selectedPathsCount={selectedPaths.size}
        shouldShowDetailLoader={shouldShowDetailLoader}
        showHiddenFiles={showHiddenFiles}
        showQuickSave={showQuickSave}
        statData={statData}
        unsupportedEditPath={unsupportedEditPath}
        uploadDialogOpen={uploadDialogOpen}
        uploadEntries={uploadEntries}
        uploadSummary={uploadSummary}
        viewIcon={viewIcon}
        viewMode={viewMode}
      />
    </>
  );
};
export type { ViewMode };
export default FileBrowser;
