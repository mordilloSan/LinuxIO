import { memo, type ChangeEvent, type RefObject } from "react";

import type { CountProgress, TaskProgress } from "@/api";
import type { FileEditorHandle } from "@/components/editor/FileEditor";
import AppConfirmDialog from "@/components/ui/AppConfirmDialog";
import AppUnsavedChangesDialog from "@/components/ui/AppUnsavedChangesDialog";

import CompressFormatDialog from "./CompressFormatDialog";
import ContextMenu from "./ContextMenu";
import {
  FileBrowserConflictDialog,
  FileBrowserDetailsDialog,
  FileBrowserEditorDialog,
  FileBrowserSaveConflictDialog,
  FileBrowserUploadDialog,
  type MultiItemsStats,
} from "./FileBrowserPanels";
import InputDialog from "./InputDialog";
import PermissionsDialog from "./PermissionsDialog";
import type { DroppedEntry } from "../-fileUpload";
import type { EditorSaveConflict } from "../-hooks/useFileBrowserEditorActions";
import type {
  ConflictDecision,
  ConflictPrompt,
} from "../-hooks/useFileConflicts";
import type { PermissionsDialogState } from "../-hooks/useFileDialogs";
import type { UploadSummary } from "../-hooks/useFileUpload";
import type { FileResource, ResourceStatData, ViewMode } from "../-types";

interface CompressFormatDialogState {
  baseName: string;
  paths: string[];
}

interface FileBrowserEditorDialogsProps {
  closeEditorDialog: boolean;
  editingFileResource?: FileResource;
  editingFileError: unknown;
  editingPath: string | null;
  editorRef: RefObject<FileEditorHandle | null>;
  isDirty: boolean;
  isEditingFileLoading: boolean;
  isSaving: boolean;
  onClose: () => void;
  onCancelConflict: () => void;
  onDirtyChange: (isDirty: boolean) => void;
  onDiscardAndExit: () => void;
  onKeepEditing: () => void;
  onOverwriteConflict: () => Promise<void> | void;
  onReloadConflict: () => Promise<void> | void;
  onRequestSave: () => Promise<void>;
  onSaveContent: (
    content: string,
    expectedVersion?: string,
  ) => Promise<boolean>;
  onSaveAndExit: () => Promise<void> | void;
  saveConflict: EditorSaveConflict | null;
  onSearchChange: (value: string) => void;
  onSwitchView: () => void;
  onToggleHiddenFiles: () => void;
  searchQuery: string;
  showHiddenFiles: boolean;
  showQuickSave: boolean;
  viewMode: ViewMode;
}

interface FileBrowserContextMenuProps {
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

interface FileBrowserDetailsDialogsProps {
  detailError: unknown;
  detailResource?: FileResource;
  detailTarget: string[] | null;
  hasMultipleTargets: boolean;
  hasSingleTarget: boolean;
  isStatLoading: boolean;
  multiItemsStats: MultiItemsStats;
  onClose: () => void;
  onDownload: (path: string) => void;
  onEdit: (path: string) => void;
  shouldShowLoader: boolean;
  statData?: ResourceStatData | null;
}

interface FileBrowserCreateDialogsProps {
  filePending?: boolean;
  fileOpen: boolean;
  folderPending?: boolean;
  folderOpen: boolean;
  onCloseFile: () => void;
  onCloseFolder: () => void;
  onConfirmFile: (fileName: string) => Promise<void> | void;
  onConfirmFolder: (folderName: string) => Promise<void> | void;
}

interface FileBrowserDeleteDialogProps {
  onClose: () => void;
  onConfirm: () => void;
  open: boolean;
  pendingDeletePaths: string[];
  isPending?: boolean;
  progress?: TaskProgress<CountProgress> | null;
}

interface FileBrowserPermissionsDialogProps {
  dialog: PermissionsDialogState | null;
  onExited: () => void;
  onClose: () => void;
  onConfirm: (
    mode: string,
    recursive: boolean,
    owner?: string,
    group?: string,
  ) => Promise<void> | void;
  isPending?: boolean;
  open: boolean;
  progress?: TaskProgress<CountProgress> | null;
}

export interface FileBrowserUploadDialogProps {
  entries: DroppedEntry[];
  fileInputRef: RefObject<HTMLInputElement | null>;
  folderInputRef: RefObject<HTMLInputElement | null>;
  isProcessing: boolean;
  normalizedPath: string;
  onChangeInput: (event: ChangeEvent<HTMLInputElement>) => void;
  onClearSelection: () => void;
  onClose: () => void;
  onPickFiles: () => void;
  onPickFolder: () => void;
  onStart: () => Promise<void> | void;
  open: boolean;
  summary: UploadSummary;
}

interface FileBrowserArchiveDialogsProps {
  compressFormatDialog: CompressFormatDialogState | null;
  onCloseCompressFormatDialog: () => void;
  onConfirmCompressFormat: (format: "zip" | "tar.gz") => Promise<void> | void;
}

export interface FileBrowserConflictDialogProps {
  onCancel: () => void;
  onResolve: (decisions: Record<string, ConflictDecision>) => void;
  prompt: ConflictPrompt | null;
}

interface FileBrowserDialogsProps {
  archive: FileBrowserArchiveDialogsProps;
  conflict: FileBrowserConflictDialogProps;
  contextMenu: FileBrowserContextMenuProps;
  create: FileBrowserCreateDialogsProps;
  deleteDialog: FileBrowserDeleteDialogProps;
  details: FileBrowserDetailsDialogsProps;
  editor: FileBrowserEditorDialogsProps;
  permissions: FileBrowserPermissionsDialogProps;
  upload: FileBrowserUploadDialogProps;
}

const FileBrowserDialogs = ({
  archive,
  conflict,
  contextMenu,
  create,
  deleteDialog,
  details,
  editor,
  permissions,
  upload,
}: FileBrowserDialogsProps) => {
  return (
    <>
      <FileBrowserEditorDialog
        editingFileResource={editor.editingFileResource}
        editingFileError={editor.editingFileError}
        editingPath={editor.editingPath}
        editorRef={editor.editorRef}
        isDirty={editor.isDirty}
        isEditingFileLoading={editor.isEditingFileLoading}
        isSaving={editor.isSaving}
        onCloseEditor={editor.onClose}
        onDirtyChange={editor.onDirtyChange}
        onSaveContent={editor.onSaveContent}
        onSaveFile={editor.onRequestSave}
        onSearchChange={editor.onSearchChange}
        onSwitchView={editor.onSwitchView}
        onToggleHiddenFiles={editor.onToggleHiddenFiles}
        searchQuery={editor.searchQuery}
        showHiddenFiles={editor.showHiddenFiles}
        showQuickSave={editor.showQuickSave}
        viewMode={editor.viewMode}
      />

      <FileBrowserSaveConflictDialog
        conflict={editor.saveConflict}
        isSaving={editor.isSaving}
        onCancel={editor.onCancelConflict}
        onOverwrite={editor.onOverwriteConflict}
        onReload={editor.onReloadConflict}
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
        isStatLoading={details.isStatLoading}
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
        isPending={create.filePending}
        open={create.fileOpen}
        title="Create File"
      />

      <InputDialog
        label="Folder Name"
        onClose={create.onCloseFolder}
        onConfirm={create.onConfirmFolder}
        isPending={create.folderPending}
        open={create.folderOpen}
        title="Create Folder"
      />

      <AppConfirmDialog
        confirmText="Delete"
        message={`Are you sure you want to delete ${deleteDialog.pendingDeletePaths.length} item${deleteDialog.pendingDeletePaths.length !== 1 ? "s" : ""}?`}
        onClose={deleteDialog.onClose}
        onConfirm={deleteDialog.onConfirm}
        open={deleteDialog.open}
        title="Delete Items"
        destructive
        isPending={deleteDialog.isPending}
        progress={deleteDialog.progress}
      />

      {permissions.dialog && (
        <PermissionsDialog
          currentMode={permissions.dialog.mode}
          group={permissions.dialog.group}
          isDirectory={permissions.dialog.isDirectory}
          onClose={permissions.onClose}
          onConfirm={permissions.onConfirm}
          onExited={permissions.onExited}
          open={permissions.open}
          owner={permissions.dialog.owner}
          pathLabel={permissions.dialog.pathLabel}
          isPending={permissions.isPending}
          progress={permissions.progress}
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

      <FileBrowserConflictDialog
        onCancel={conflict.onCancel}
        onResolve={conflict.onResolve}
        prompt={conflict.prompt}
      />

      <AppUnsavedChangesDialog
        isSaving={editor.isSaving}
        onDiscardAndExit={editor.onDiscardAndExit}
        onKeepEditing={editor.onKeepEditing}
        onSaveAndExit={editor.onSaveAndExit}
        open={editor.closeEditorDialog}
      />
    </>
  );
};

export default memo(FileBrowserDialogs);
