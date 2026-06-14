import React, { ReactNode } from "react";

import {
  FileBrowserDetailsDialog,
  FileBrowserEditorDialog,
  FileBrowserOverwriteDialog,
  FileBrowserUploadDialog,
  type MultiItemsStats,
} from "./FileBrowserPanels";

import CompressFormatDialog from "@/components/filebrowser/CompressFormatDialog";
import ConfirmDialog from "@/components/filebrowser/ConfirmDialog";
import ContextMenu from "@/components/filebrowser/ContextMenu";
import type { FileEditorHandle } from "@/components/filebrowser/FileEditor";
import InputDialog from "@/components/filebrowser/InputDialog";
import PermissionsDialog from "@/components/filebrowser/PermissionsDialog";
import UnsavedChangesDialog from "@/components/filebrowser/UnsavedChangesDialog";
import type { PermissionsDialogState } from "@/hooks/filebrowser/useFileDialogs";
import type { DroppedEntry } from "@/hooks/filebrowser/useFileDragAndDrop";
import type { UploadSummary } from "@/hooks/filebrowser/useFileUpload";
import type {
  FileResource,
  ResourceStatData,
  ViewMode,
} from "@/types/filebrowser";

interface CompressFormatDialogState {
  baseName: string;
  paths: string[];
}

interface FileBrowserDialogsProps {
  canCompressSelection: boolean;
  canExtractSelection: boolean;
  canShowDetails: boolean;
  clipboardAvailable: boolean;
  closeEditorDialog: boolean;
  compressFormatDialog: CompressFormatDialogState | null;
  contextMenuPosition: { top: number; left: number } | null;
  createFileDialog: boolean;
  createFolderDialog: boolean;
  deleteDialog: boolean;
  detailError: unknown;
  detailResource?: FileResource;
  detailTarget: string[] | null;
  editingFileResource?: FileResource;
  editingPath: string | null;
  editorRef: React.RefObject<FileEditorHandle | null>;
  fileInputRef: React.RefObject<HTMLInputElement | null>;
  folderInputRef: React.RefObject<HTMLInputElement | null>;
  hasMultipleDetailTargets: boolean;
  hasSingleDetailTarget: boolean;
  isEditorDirty: boolean;
  isEditingFileLoading: boolean;
  isSavingFile: boolean;
  isStatPending: boolean;
  isUploadProcessing: boolean;
  multiItemsStats: MultiItemsStats;
  normalizedPath: string;
  onChangePermissions: () => void;
  onChangeUploadInput: (event: React.ChangeEvent<HTMLInputElement>) => void;
  onClearUploadSelection: () => void;
  onCloseCompressFormatDialog: () => void;
  onCloseContextMenu: () => void;
  onCloseCreateFileDialog: () => void;
  onCloseCreateFolderDialog: () => void;
  onCloseDeleteDialog: () => void;
  onCloseDetailDialog: () => void;
  onCloseEditor: () => void;
  onClosePermissionsDialog: () => void;
  onCloseUnsupportedEditDialog: () => void;
  onCloseUploadDialog: () => void;
  onCompressSelection: () => void;
  onConfirmCompressFormat: (format: "zip" | "tar.gz") => Promise<void> | void;
  onConfirmCreateFile: (fileName: string) => void;
  onConfirmCreateFolder: (folderName: string) => void;
  onConfirmDelete: () => void;
  onConfirmOverwrite: () => Promise<void> | void;
  onConfirmPermissions: (
    mode: string,
    recursive: boolean,
    owner?: string,
    group?: string,
  ) => Promise<void> | void;
  onConfirmUnsupportedEdit: () => void;
  onCopy: () => void;
  onCreateFile: () => void;
  onCreateFolder: () => void;
  onCut: () => void;
  onDelete: () => void;
  onDiscardAndExit: () => void;
  onDownloadDetail: (path: string) => void;
  onDownloadSelected: () => void;
  onDirtyChange: (isDirty: boolean) => void;
  onEditFile: (path: string) => void;
  onExtractSelection: () => Promise<void> | void;
  onKeepEditing: () => void;
  onOpenContainingFolder: () => void;
  onPaste: () => void;
  onPickFiles: () => void;
  onPickFolder: () => void;
  onRename: () => void;
  onSaveAndExit: () => Promise<void> | void;
  onSaveFile: () => Promise<void>;
  onSearchChange: (value: string) => void;
  onShowDetails: () => void;
  onStartUpload: () => Promise<void> | void;
  onSwitchView: () => void;
  onToggleHiddenFiles: () => void;
  onUpload: () => void;
  onOverwriteCancel: () => void;
  overwriteTargets: DroppedEntry[] | null;
  pendingDeletePaths: string[];
  permissionsDialog: PermissionsDialogState | null;
  searchQuery: string;
  selectedPathsCount: number;
  shouldShowDetailLoader: boolean;
  showHiddenFiles: boolean;
  showQuickSave: boolean;
  statData?: ResourceStatData | null;
  unsupportedEditPath: string | null;
  uploadDialogOpen: boolean;
  uploadEntries: DroppedEntry[];
  uploadSummary: UploadSummary;
  viewIcon: ReactNode;
  viewMode: ViewMode;
}

const FileBrowserDialogs: React.FC<FileBrowserDialogsProps> = ({
  canCompressSelection,
  canExtractSelection,
  canShowDetails,
  clipboardAvailable,
  closeEditorDialog,
  compressFormatDialog,
  contextMenuPosition,
  createFileDialog,
  createFolderDialog,
  deleteDialog,
  detailError,
  detailResource,
  detailTarget,
  editingFileResource,
  editingPath,
  editorRef,
  fileInputRef,
  folderInputRef,
  hasMultipleDetailTargets,
  hasSingleDetailTarget,
  isEditorDirty,
  isEditingFileLoading,
  isSavingFile,
  isStatPending,
  isUploadProcessing,
  multiItemsStats,
  normalizedPath,
  onChangePermissions,
  onChangeUploadInput,
  onClearUploadSelection,
  onCloseCompressFormatDialog,
  onCloseContextMenu,
  onCloseCreateFileDialog,
  onCloseCreateFolderDialog,
  onCloseDeleteDialog,
  onCloseDetailDialog,
  onCloseEditor,
  onClosePermissionsDialog,
  onCloseUnsupportedEditDialog,
  onCloseUploadDialog,
  onCompressSelection,
  onConfirmCompressFormat,
  onConfirmCreateFile,
  onConfirmCreateFolder,
  onConfirmDelete,
  onConfirmOverwrite,
  onConfirmPermissions,
  onConfirmUnsupportedEdit,
  onCopy,
  onCreateFile,
  onCreateFolder,
  onCut,
  onDelete,
  onDiscardAndExit,
  onDownloadDetail,
  onDownloadSelected,
  onDirtyChange,
  onEditFile,
  onExtractSelection,
  onKeepEditing,
  onOpenContainingFolder,
  onOverwriteCancel,
  onPaste,
  onPickFiles,
  onPickFolder,
  onRename,
  onSaveAndExit,
  onSaveFile,
  onSearchChange,
  onShowDetails,
  onStartUpload,
  onSwitchView,
  onToggleHiddenFiles,
  onUpload,
  overwriteTargets,
  pendingDeletePaths,
  permissionsDialog,
  searchQuery,
  selectedPathsCount,
  shouldShowDetailLoader,
  showHiddenFiles,
  showQuickSave,
  statData,
  unsupportedEditPath,
  uploadDialogOpen,
  uploadEntries,
  uploadSummary,
  viewIcon,
  viewMode,
}) => (
  <>
    <FileBrowserEditorDialog
      editingFileResource={editingFileResource}
      editingPath={editingPath}
      editorRef={editorRef}
      isDirty={isEditorDirty}
      isEditingFileLoading={isEditingFileLoading}
      isSaving={isSavingFile}
      onCloseEditor={onCloseEditor}
      onDirtyChange={onDirtyChange}
      onSaveFile={onSaveFile}
      onSearchChange={onSearchChange}
      onSwitchView={onSwitchView}
      onToggleHiddenFiles={onToggleHiddenFiles}
      searchQuery={searchQuery}
      showHiddenFiles={showHiddenFiles}
      showQuickSave={showQuickSave}
      viewIcon={viewIcon}
      viewMode={viewMode}
    />

    <ContextMenu
      anchorPosition={contextMenuPosition}
      canCompress={canCompressSelection}
      canExtract={canExtractSelection}
      canOpenContainingFolder={Boolean(searchQuery) && selectedPathsCount === 1}
      canRename={selectedPathsCount === 1}
      canShowDetails={canShowDetails}
      hasClipboard={clipboardAvailable}
      hasSelection={selectedPathsCount > 0}
      onChangePermissions={onChangePermissions}
      onClose={onCloseContextMenu}
      onCompress={onCompressSelection}
      onCopy={onCopy}
      onCreateFile={onCreateFile}
      onCreateFolder={onCreateFolder}
      onCut={onCut}
      onDelete={onDelete}
      onDownload={onDownloadSelected}
      onExtract={onExtractSelection}
      onOpenContainingFolder={onOpenContainingFolder}
      onPaste={onPaste}
      onRename={onRename}
      onShowDetails={onShowDetails}
      onUpload={onUpload}
    />

    <CompressFormatDialog
      onClose={onCloseCompressFormatDialog}
      onConfirm={onConfirmCompressFormat}
      open={Boolean(compressFormatDialog)}
    />

    <FileBrowserDetailsDialog
      detailError={detailError}
      detailResource={detailResource}
      detailTarget={detailTarget}
      hasMultipleDetailTargets={hasMultipleDetailTargets}
      hasSingleDetailTarget={hasSingleDetailTarget}
      isStatPending={isStatPending}
      multiItemsStats={multiItemsStats}
      onClose={onCloseDetailDialog}
      onDownload={onDownloadDetail}
      onEdit={onEditFile}
      shouldShowDetailLoader={shouldShowDetailLoader}
      statData={statData}
    />

    <InputDialog
      label="File Name"
      onClose={onCloseCreateFileDialog}
      onConfirm={onConfirmCreateFile}
      open={createFileDialog}
      title="Create File"
    />

    <InputDialog
      label="Folder Name"
      onClose={onCloseCreateFolderDialog}
      onConfirm={onConfirmCreateFolder}
      open={createFolderDialog}
      title="Create Folder"
    />

    <ConfirmDialog
      confirmText="Delete"
      message={`Are you sure you want to delete ${pendingDeletePaths.length} item${pendingDeletePaths.length !== 1 ? "s" : ""}?`}
      onClose={onCloseDeleteDialog}
      onConfirm={onConfirmDelete}
      open={deleteDialog}
      title="Delete Items"
    />

    <ConfirmDialog
      confirmText="Edit Anyway"
      message={`"${unsupportedEditPath?.split("/").pop() ?? ""}" is not a recognized text file. Opening it in the editor may show garbled content, and saving could corrupt binary files. Edit anyway?`}
      onClose={onCloseUnsupportedEditDialog}
      onConfirm={onConfirmUnsupportedEdit}
      open={Boolean(unsupportedEditPath)}
      title="Edit Unsupported File?"
    />

    {permissionsDialog && (
      <PermissionsDialog
        currentMode={permissionsDialog.mode}
        group={permissionsDialog.group}
        isDirectory={permissionsDialog.isDirectory}
        onClose={onClosePermissionsDialog}
        onConfirm={onConfirmPermissions}
        open
        owner={permissionsDialog.owner}
        pathLabel={permissionsDialog.pathLabel}
        selectionCount={permissionsDialog.selectionCount}
      />
    )}

    <FileBrowserUploadDialog
      fileInputRef={fileInputRef}
      folderInputRef={folderInputRef}
      isUploadProcessing={isUploadProcessing}
      normalizedPath={normalizedPath}
      onChangeUploadInput={onChangeUploadInput}
      onClearUploadSelection={onClearUploadSelection}
      onClose={onCloseUploadDialog}
      onPickFiles={onPickFiles}
      onPickFolder={onPickFolder}
      onStartUpload={onStartUpload}
      open={uploadDialogOpen}
      uploadEntries={uploadEntries}
      uploadSummary={uploadSummary}
    />

    <FileBrowserOverwriteDialog
      normalizedPath={normalizedPath}
      onCancel={onOverwriteCancel}
      onConfirm={onConfirmOverwrite}
      overwriteTargets={overwriteTargets}
    />

    <UnsavedChangesDialog
      isSaving={isSavingFile}
      onDiscardAndExit={onDiscardAndExit}
      onKeepEditing={onKeepEditing}
      onSaveAndExit={onSaveAndExit}
      open={closeEditorDialog}
    />
  </>
);

export default FileBrowserDialogs;
