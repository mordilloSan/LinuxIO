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

export interface FileBrowserEditorDialogsProps {
  closeEditorDialog: boolean;
  editingFileResource?: FileResource;
  editingPath: string | null;
  editorRef: React.RefObject<FileEditorHandle | null>;
  isDirty: boolean;
  isEditingFileLoading: boolean;
  isSaving: boolean;
  onClose: () => void;
  onDirtyChange: (isDirty: boolean) => void;
  onDiscardAndExit: () => void;
  onKeepEditing: () => void;
  onSave: () => Promise<void>;
  onSaveAndExit: () => Promise<void> | void;
  onSearchChange: (value: string) => void;
  onSwitchView: () => void;
  onToggleHiddenFiles: () => void;
  searchQuery: string;
  showHiddenFiles: boolean;
  showQuickSave: boolean;
  viewIcon: ReactNode;
  viewMode: ViewMode;
}

export interface FileBrowserContextMenuProps {
  anchorPosition: { top: number; left: number } | null;
  canCompress: boolean;
  canExtract: boolean;
  canOpenContainingFolder: boolean;
  canRename: boolean;
  canShowDetails: boolean;
  hasClipboard: boolean;
  hasSelection: boolean;
  onChangePermissions: () => void;
  onClose: () => void;
  onCompress: () => void;
  onCopy: () => void;
  onCreateFile: () => void;
  onCreateFolder: () => void;
  onCut: () => void;
  onDelete: () => void;
  onDownload: () => void;
  onExtract: () => Promise<void> | void;
  onOpenContainingFolder: () => void;
  onPaste: () => void;
  onRename: () => void;
  onShowDetails: () => void;
  onUpload: () => void;
}

export interface FileBrowserDetailsDialogsProps {
  detailError: unknown;
  detailResource?: FileResource;
  detailTarget: string[] | null;
  hasMultipleTargets: boolean;
  hasSingleTarget: boolean;
  isStatPending: boolean;
  multiItemsStats: MultiItemsStats;
  onClose: () => void;
  onDownload: (path: string) => void;
  onEdit: (path: string) => void;
  shouldShowLoader: boolean;
  statData?: ResourceStatData | null;
}

export interface FileBrowserCreateDialogsProps {
  fileOpen: boolean;
  folderOpen: boolean;
  onCloseFile: () => void;
  onCloseFolder: () => void;
  onConfirmFile: (fileName: string) => void;
  onConfirmFolder: (folderName: string) => void;
}

export interface FileBrowserDeleteDialogProps {
  onClose: () => void;
  onConfirm: () => void;
  open: boolean;
  pendingDeletePaths: string[];
}

export interface FileBrowserPermissionsDialogProps {
  dialog: PermissionsDialogState | null;
  onClose: () => void;
  onConfirm: (
    mode: string,
    recursive: boolean,
    owner?: string,
    group?: string,
  ) => Promise<void> | void;
}

export interface FileBrowserUploadDialogProps {
  entries: DroppedEntry[];
  fileInputRef: React.RefObject<HTMLInputElement | null>;
  folderInputRef: React.RefObject<HTMLInputElement | null>;
  isProcessing: boolean;
  normalizedPath: string;
  onChangeInput: (event: React.ChangeEvent<HTMLInputElement>) => void;
  onClearSelection: () => void;
  onClose: () => void;
  onPickFiles: () => void;
  onPickFolder: () => void;
  onStart: () => Promise<void> | void;
  open: boolean;
  summary: UploadSummary;
}

export interface FileBrowserArchiveDialogsProps {
  compressFormatDialog: CompressFormatDialogState | null;
  onCloseCompressFormatDialog: () => void;
  onCloseUnsupportedEditDialog: () => void;
  onConfirmCompressFormat: (format: "zip" | "tar.gz") => Promise<void> | void;
  onConfirmOverwrite: () => Promise<void> | void;
  onConfirmUnsupportedEdit: () => void;
  onOverwriteCancel: () => void;
  overwriteTargets: DroppedEntry[] | null;
  unsupportedEditPath: string | null;
}

export interface FileBrowserDialogsProps {
  archive: FileBrowserArchiveDialogsProps;
  contextMenu: FileBrowserContextMenuProps;
  create: FileBrowserCreateDialogsProps;
  deleteDialog: FileBrowserDeleteDialogProps;
  details: FileBrowserDetailsDialogsProps;
  editor: FileBrowserEditorDialogsProps;
  permissions: FileBrowserPermissionsDialogProps;
  upload: FileBrowserUploadDialogProps;
}

const FileBrowserDialogs: React.FC<FileBrowserDialogsProps> = ({
  archive,
  contextMenu,
  create,
  deleteDialog,
  details,
  editor,
  permissions,
  upload,
}) => (
  <>
    <FileBrowserEditorDialog
      editingFileResource={editor.editingFileResource}
      editingPath={editor.editingPath}
      editorRef={editor.editorRef}
      isDirty={editor.isDirty}
      isEditingFileLoading={editor.isEditingFileLoading}
      isSaving={editor.isSaving}
      onCloseEditor={editor.onClose}
      onDirtyChange={editor.onDirtyChange}
      onSaveFile={editor.onSave}
      onSearchChange={editor.onSearchChange}
      onSwitchView={editor.onSwitchView}
      onToggleHiddenFiles={editor.onToggleHiddenFiles}
      searchQuery={editor.searchQuery}
      showHiddenFiles={editor.showHiddenFiles}
      showQuickSave={editor.showQuickSave}
      viewIcon={editor.viewIcon}
      viewMode={editor.viewMode}
    />

    <ContextMenu
      anchorPosition={contextMenu.anchorPosition}
      canCompress={contextMenu.canCompress}
      canExtract={contextMenu.canExtract}
      canOpenContainingFolder={contextMenu.canOpenContainingFolder}
      canRename={contextMenu.canRename}
      canShowDetails={contextMenu.canShowDetails}
      hasClipboard={contextMenu.hasClipboard}
      hasSelection={contextMenu.hasSelection}
      onChangePermissions={contextMenu.onChangePermissions}
      onClose={contextMenu.onClose}
      onCompress={contextMenu.onCompress}
      onCopy={contextMenu.onCopy}
      onCreateFile={contextMenu.onCreateFile}
      onCreateFolder={contextMenu.onCreateFolder}
      onCut={contextMenu.onCut}
      onDelete={contextMenu.onDelete}
      onDownload={contextMenu.onDownload}
      onExtract={contextMenu.onExtract}
      onOpenContainingFolder={contextMenu.onOpenContainingFolder}
      onPaste={contextMenu.onPaste}
      onRename={contextMenu.onRename}
      onShowDetails={contextMenu.onShowDetails}
      onUpload={contextMenu.onUpload}
    />

    <CompressFormatDialog
      onClose={archive.onCloseCompressFormatDialog}
      onConfirm={archive.onConfirmCompressFormat}
      open={Boolean(archive.compressFormatDialog)}
    />

    <FileBrowserDetailsDialog
      detailError={details.detailError}
      detailResource={details.detailResource}
      detailTarget={details.detailTarget}
      hasMultipleDetailTargets={details.hasMultipleTargets}
      hasSingleDetailTarget={details.hasSingleTarget}
      isStatPending={details.isStatPending}
      multiItemsStats={details.multiItemsStats}
      onClose={details.onClose}
      onDownload={details.onDownload}
      onEdit={details.onEdit}
      shouldShowDetailLoader={details.shouldShowLoader}
      statData={details.statData}
    />

    <InputDialog
      label="File Name"
      onClose={create.onCloseFile}
      onConfirm={create.onConfirmFile}
      open={create.fileOpen}
      title="Create File"
    />

    <InputDialog
      label="Folder Name"
      onClose={create.onCloseFolder}
      onConfirm={create.onConfirmFolder}
      open={create.folderOpen}
      title="Create Folder"
    />

    <ConfirmDialog
      confirmText="Delete"
      message={`Are you sure you want to delete ${deleteDialog.pendingDeletePaths.length} item${deleteDialog.pendingDeletePaths.length !== 1 ? "s" : ""}?`}
      onClose={deleteDialog.onClose}
      onConfirm={deleteDialog.onConfirm}
      open={deleteDialog.open}
      title="Delete Items"
    />

    <ConfirmDialog
      confirmText="Edit Anyway"
      message={`"${archive.unsupportedEditPath?.split("/").pop() ?? ""}" is not a recognized text file. Opening it in the editor may show garbled content, and saving could corrupt binary files. Edit anyway?`}
      onClose={archive.onCloseUnsupportedEditDialog}
      onConfirm={archive.onConfirmUnsupportedEdit}
      open={Boolean(archive.unsupportedEditPath)}
      title="Edit Unsupported File?"
    />

    {permissions.dialog && (
      <PermissionsDialog
        currentMode={permissions.dialog.mode}
        group={permissions.dialog.group}
        isDirectory={permissions.dialog.isDirectory}
        onClose={permissions.onClose}
        onConfirm={permissions.onConfirm}
        open
        owner={permissions.dialog.owner}
        pathLabel={permissions.dialog.pathLabel}
        selectionCount={permissions.dialog.selectionCount}
      />
    )}

    <FileBrowserUploadDialog
      fileInputRef={upload.fileInputRef}
      folderInputRef={upload.folderInputRef}
      isUploadProcessing={upload.isProcessing}
      normalizedPath={upload.normalizedPath}
      onChangeUploadInput={upload.onChangeInput}
      onClearUploadSelection={upload.onClearSelection}
      onClose={upload.onClose}
      onPickFiles={upload.onPickFiles}
      onPickFolder={upload.onPickFolder}
      onStartUpload={upload.onStart}
      open={upload.open}
      uploadEntries={upload.entries}
      uploadSummary={upload.summary}
    />

    <FileBrowserOverwriteDialog
      normalizedPath={upload.normalizedPath}
      onCancel={archive.onOverwriteCancel}
      onConfirm={archive.onConfirmOverwrite}
      overwriteTargets={archive.overwriteTargets}
    />

    <UnsavedChangesDialog
      isSaving={editor.isSaving}
      onDiscardAndExit={editor.onDiscardAndExit}
      onKeepEditing={editor.onKeepEditing}
      onSaveAndExit={editor.onSaveAndExit}
      open={editor.closeEditorDialog}
    />
  </>
);

export default React.memo(FileBrowserDialogs);
