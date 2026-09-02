import { Icon } from "@iconify/react";
import {
  lazy,
  Suspense,
  useState,
  type ChangeEvent,
  type RefObject,
} from "react";

import FileBrowserDialog from "@/components/dialog/GeneralDialog";
import FileBrowserHeader from "@/components/filebrowser/FileBrowserHeader";
import FileDetail from "@/components/filebrowser/FileDetail";
import type { FileEditorHandle } from "@/components/filebrowser/FileEditor";
import MultiFileDetail from "@/components/filebrowser/MultiFileDetail";
import ComponentLoader from "@/components/loaders/ComponentLoader";
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
import type { EditorSaveConflict } from "@/hooks/filebrowser/useFileBrowserEditorActions";
import type {
  ConflictDecision,
  ConflictPrompt,
} from "@/hooks/filebrowser/useFileConflicts";
import type { DroppedEntry } from "@/hooks/filebrowser/useFileDroppedEntries";
import type { UploadSummary } from "@/hooks/filebrowser/useFileUpload";
import type {
  FileResource,
  MultiStatsItem,
  ResourceStatData,
  ViewMode,
} from "@/types/filebrowser";

const FileEditor = lazy(() => import("@/components/filebrowser/FileEditor"));

interface FileDropOverlayProps {
  normalizedPath: string;
}

export const FileDropOverlay = ({ normalizedPath }: FileDropOverlayProps) => {
  return (
    <div
      style={{
        position: "absolute",
        inset: 0,
        border: "2px dashed var(--app-palette-primary-main)",
        backgroundColor: "rgba(var(--app-palette-primary-mainChannel) / 0.08)",
        zIndex: 5,
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        pointerEvents: "none",
        gap: "var(--app-space-4)",
      }}
    >
      <AppTypography variant="h6">Drop to upload</AppTypography>
      <AppTypography color="text.secondary" variant="body2">
        Files and folders will be copied to {normalizedPath}
      </AppTypography>
    </div>
  );
};

interface FileBrowserEditorDialogProps {
  editingFileResource?: FileResource;
  editingFileError: unknown;
  editingPath: string | null;
  editorRef: RefObject<FileEditorHandle | null>;
  isDirty: boolean;
  isEditingFileLoading: boolean;
  isSaving: boolean;
  onCloseEditor: () => void;
  onDirtyChange: (isDirty: boolean) => void;
  onSaveContent: (
    content: string,
    expectedVersion?: string,
  ) => Promise<boolean>;
  onSaveFile: () => Promise<void>;
  onSearchChange: (value: string) => void;
  onSwitchView: () => void;
  onToggleHiddenFiles: () => void;
  searchQuery: string;
  showHiddenFiles: boolean;
  showQuickSave: boolean;
  viewMode: ViewMode;
}

export const FileBrowserEditorDialog = ({
  editingFileResource,
  editingFileError,
  editingPath,
  editorRef,
  isDirty,
  isEditingFileLoading,
  isSaving,
  onCloseEditor,
  onDirtyChange,
  onSaveContent,
  onSaveFile,
  onSearchChange,
  onSwitchView,
  onToggleHiddenFiles,
  searchQuery,
  showHiddenFiles,
  showQuickSave,
  viewMode,
}: FileBrowserEditorDialogProps) => {
  const readOnly = editingFileResource?.canSave !== true;

  return (
    <AppFullscreenDialog
      contentStyle={{
        backgroundColor: "var(--app-palette-background-default)",
      }}
      onClose={onCloseEditor}
      open={Boolean(editingPath)}
    >
      <FileBrowserHeader
        editingFileName={editingFileResource?.name}
        editingFilePath={editingPath || undefined}
        isDirty={isDirty}
        readOnly={readOnly}
        isSaving={isSaving}
        onCloseEditor={onCloseEditor}
        onSaveFile={onSaveFile}
        onSearchChange={onSearchChange}
        onSwitchView={onSwitchView}
        onToggleHiddenFiles={onToggleHiddenFiles}
        searchQuery={searchQuery}
        showHiddenFiles={showHiddenFiles}
        showQuickSave={showQuickSave}
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
              initialVersion={editingFileResource.version}
              isSaving={isSaving}
              onDirtyChange={onDirtyChange}
              onSave={onSaveContent}
              readOnly={readOnly}
              ref={editorRef}
            />
          </Suspense>
        )}

        {editingPath &&
          !isEditingFileLoading &&
          !editingFileResource &&
          Boolean(editingFileError) && (
            <div
              style={{
                alignItems: "center",
                display: "flex",
                flex: 1,
                flexDirection: "column",
                gap: "var(--app-space-4)",
                justifyContent: "center",
                padding: "var(--app-space-6)",
              }}
            >
              <AppAlert severity="error">
                <AppAlertTitle>Unable to open file</AppAlertTitle>
                Close the editor and try again.
              </AppAlert>
              <AppButton onClick={onCloseEditor}>Close editor</AppButton>
            </div>
          )}
      </div>
    </AppFullscreenDialog>
  );
};

interface FileBrowserSaveConflictDialogProps {
  isSaving: boolean;
  onCancel: () => void;
  onOverwrite: () => void;
  onReload: () => void;
  conflict: EditorSaveConflict | null;
}

export const FileBrowserSaveConflictDialog = ({
  conflict,
  isSaving,
  onCancel,
  onOverwrite,
  onReload,
}: FileBrowserSaveConflictDialogProps) => (
  <FileBrowserDialog
    disableEscapeKeyDown={isSaving}
    fullWidth
    maxWidth="sm"
    onClose={isSaving ? undefined : onCancel}
    open={Boolean(conflict)}
  >
    <AppDialogTitle>File changed on disk</AppDialogTitle>
    <AppDialogContent
      style={{ borderTop: "1px solid var(--app-palette-divider)" }}
    >
      <AppTypography variant="body1">
        This file changed after you opened it. Reload to discard your edits, or
        overwrite the current file with your edits.
      </AppTypography>
    </AppDialogContent>
    <AppDialogActions>
      <AppButton disabled={isSaving} onClick={onCancel}>
        Cancel
      </AppButton>
      <AppButton disabled={isSaving} onClick={onReload} variant="outlined">
        Reload file
      </AppButton>
      <AppButton
        color="warning"
        disabled={isSaving}
        onClick={onOverwrite}
        variant="contained"
      >
        Overwrite file
      </AppButton>
    </AppDialogActions>
  </FileBrowserDialog>
);

export type MultiFileDetailItem = MultiStatsItem & {
  aggregateSize?: number;
  error?: Error | null;
  isLoading?: boolean;
};

export interface MultiItemsStats {
  isAnyLoading: boolean;
  items: MultiFileDetailItem[];
  totalSize: number;
}

interface FileBrowserDetailsDialogProps {
  detailError: unknown;
  detailResource?: FileResource;
  detailTarget: string[] | null;
  hasMultipleDetailTargets: boolean;
  hasSingleDetailTarget: boolean;
  isStatLoading: boolean;
  multiItemsStats: MultiItemsStats;
  onClose: () => void;
  onDownload: (path: string) => void;
  onEdit: (path: string) => void;
  shouldShowDetailLoader: boolean;
  statData?: ResourceStatData | null;
}

export const FileBrowserDetailsDialog = ({
  detailError,
  detailResource,
  detailTarget,
  hasMultipleDetailTargets,
  hasSingleDetailTarget,
  isStatLoading,
  multiItemsStats,
  onClose,
  onDownload,
  onEdit,
  shouldShowDetailLoader,
  statData,
}: FileBrowserDetailsDialogProps) => {
  return (
    <FileBrowserDialog
      fullWidth
      maxWidth="md"
      onClose={onClose}
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
        <AppIconButton aria-label="Close dialog" onClick={onClose} size="small">
          <Icon height={18} icon="mdi:close" width={18} />
        </AppIconButton>
      </AppDialogTitle>
      <AppDialogContent
        style={{
          minHeight: 200,
          borderTop: "1px solid var(--app-palette-divider)",
        }}
      >
        {shouldShowDetailLoader && <ComponentLoader />}
        {!shouldShowDetailLoader &&
          hasSingleDetailTarget &&
          Boolean(detailError) && (
            <AppTypography color="error">
              {detailError instanceof Error
                ? detailError.message
                : "Failed to load details"}
            </AppTypography>
          )}
        {detailResource && (
          <FileDetail
            isLoadingStat={isStatLoading}
            onDownload={onDownload}
            onEdit={onEdit}
            resource={detailResource}
            statData={statData}
          />
        )}
        {hasMultipleDetailTargets && multiItemsStats.items.length > 0 && (
          <MultiFileDetail
            isLoadingDetails={multiItemsStats.isAnyLoading}
            multiItems={multiItemsStats.items}
            onDownload={onDownload}
            totalSize={multiItemsStats.totalSize}
          />
        )}
      </AppDialogContent>
    </FileBrowserDialog>
  );
};

interface FileBrowserUploadDialogProps {
  fileInputRef: RefObject<HTMLInputElement | null>;
  folderInputRef: RefObject<HTMLInputElement | null>;
  isUploadProcessing: boolean;
  normalizedPath: string;
  onChangeUploadInput: (event: ChangeEvent<HTMLInputElement>) => void;
  onClearUploadSelection: () => void;
  onClose: () => void;
  onPickFiles: () => void;
  onPickFolder: () => void;
  onStartUpload: () => Promise<void> | void;
  open: boolean;
  uploadEntries: DroppedEntry[];
  uploadSummary: UploadSummary;
}

export const FileBrowserUploadDialog = ({
  fileInputRef,
  folderInputRef,
  isUploadProcessing,
  normalizedPath,
  onChangeUploadInput,
  onClearUploadSelection,
  onClose,
  onPickFiles,
  onPickFolder,
  onStartUpload,
  open,
  uploadEntries,
  uploadSummary,
}: FileBrowserUploadDialogProps) => {
  return (
    <FileBrowserDialog
      disableEscapeKeyDown={isUploadProcessing}
      fullWidth
      maxWidth="sm"
      onClose={onClose}
      open={open}
    >
      <AppDialogTitle>Upload files or folders</AppDialogTitle>
      <AppDialogContent
        style={{ borderTop: "1px solid var(--app-palette-divider)" }}
      >
        <AppTypography color="text.secondary" variant="body2">
          Items will be uploaded to {normalizedPath}
        </AppTypography>
        <div
          style={{
            display: "flex",
            gap: "var(--app-space-6)",
            marginTop: "var(--app-space-8)",
            flexWrap: "wrap",
          }}
        >
          <AppButton onClick={onPickFiles} variant="outlined">
            Select files
          </AppButton>
          <AppButton onClick={onPickFolder} variant="outlined">
            Select folders
          </AppButton>
        </div>
        <input
          multiple
          onChange={onChangeUploadInput}
          ref={fileInputRef}
          style={{ display: "none" }}
          type="file"
        />
        <input
          multiple
          onChange={onChangeUploadInput}
          ref={folderInputRef}
          style={{ display: "none" }}
          type="file"
          {...({
            webkitdirectory: true,
            mozdirectory: true,
          } as Record<string, boolean>)}
        />
        <AppTypography style={{ marginTop: 8 }} variant="body2">
          {uploadEntries.length
            ? `Selected ${uploadSummary.files} file${uploadSummary.files === 1 ? "" : "s"} and ${uploadSummary.folders} folder${uploadSummary.folders === 1 ? "" : "s"}.`
            : "No items selected yet."}
        </AppTypography>
        {uploadEntries.length > 0 && (
          <ul
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
          onClick={onClearUploadSelection}
        >
          Clear
        </AppButton>
        <AppButton disabled={isUploadProcessing} onClick={onClose}>
          Cancel
        </AppButton>
        <AppButton
          disabled={!uploadEntries.length || isUploadProcessing}
          onClick={onStartUpload}
          variant="contained"
        >
          {isUploadProcessing ? "Uploading..." : "Upload"}
        </AppButton>
      </AppDialogActions>
    </FileBrowserDialog>
  );
};

interface FileBrowserConflictDialogProps {
  onCancel: () => void;
  onResolve: (decisions: Record<string, ConflictDecision>) => void;
  prompt: ConflictPrompt | null;
}

export const FileBrowserConflictDialog = ({
  onCancel,
  onResolve,
  prompt,
}: FileBrowserConflictDialogProps) => {
  // Decisions are stored together with the prompt they belong to, so a new
  // prompt implicitly starts from the safe default (skip everything) — no
  // effect-based state reset needed.
  const [decisionState, setDecisionState] = useState<{
    decisions: Record<string, ConflictDecision>;
    prompt: ConflictPrompt | null;
  }>({ decisions: {}, prompt: null });
  const decisions =
    decisionState.prompt === prompt ? decisionState.decisions : {};

  const conflicts = prompt?.conflicts ?? [];
  const decisionFor = (path: string): ConflictDecision =>
    decisions[path] ?? "skip";
  const setOne = (path: string, choice: ConflictDecision) =>
    setDecisionState({ decisions: { ...decisions, [path]: choice }, prompt });
  const setAll = (choice: ConflictDecision) =>
    setDecisionState({
      decisions: Object.fromEntries(conflicts.map((c) => [c.path, choice])),
      prompt,
    });
  const handleContinue = () =>
    onResolve(
      Object.fromEntries(conflicts.map((c) => [c.path, decisionFor(c.path)])),
    );

  const choiceButton = (
    path: string,
    choice: ConflictDecision,
    label: string,
  ) => (
    <AppButton
      color={choice === "overwrite" ? "warning" : undefined}
      onClick={() => setOne(path, choice)}
      size="small"
      variant={decisionFor(path) === choice ? "contained" : "outlined"}
    >
      {label}
    </AppButton>
  );

  return (
    <FileBrowserDialog
      fullWidth
      maxWidth="sm"
      onClose={onCancel}
      open={Boolean(prompt)}
    >
      <AppDialogTitle>
        {conflicts.length} item{conflicts.length === 1 ? "" : "s"} already exist
        {conflicts.length === 1 ? "s" : ""}
      </AppDialogTitle>
      <AppDialogContent
        style={{ borderTop: "1px solid var(--app-palette-divider)" }}
      >
        <AppTypography style={{ marginBottom: 8 }} variant="body2">
          Choose what to do with each item in {prompt?.destination}. Skipped
          items are left untouched.
        </AppTypography>
        <div
          style={{
            alignItems: "center",
            display: "flex",
            gap: 8,
            marginBottom: 8,
          }}
        >
          <AppTypography color="text.secondary" variant="caption">
            Apply to all:
          </AppTypography>
          <AppButton onClick={() => setAll("skip")} size="small">
            Skip all
          </AppButton>
          <AppButton
            color="warning"
            onClick={() => setAll("overwrite")}
            size="small"
          >
            Overwrite all
          </AppButton>
        </div>
        <ul
          style={{
            listStyle: "none",
            margin: 0,
            maxHeight: 280,
            overflowY: "auto",
            padding: 0,
          }}
        >
          {conflicts.map(({ path, name, isDir }) => (
            <li
              key={path}
              style={{
                alignItems: "center",
                display: "flex",
                gap: 8,
                padding: "4px 0",
              }}
            >
              <AppTypography
                style={{
                  flex: 1,
                  minWidth: 0,
                  overflow: "hidden",
                  textOverflow: "ellipsis",
                  whiteSpace: "nowrap",
                }}
                variant="body2"
              >
                {name}
                {isDir ? "/" : ""}
              </AppTypography>
              {choiceButton(path, "skip", "Skip")}
              {choiceButton(path, "overwrite", "Overwrite")}
            </li>
          ))}
        </ul>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={onCancel}>Cancel</AppButton>
        <AppButton onClick={handleContinue} variant="contained">
          Continue
        </AppButton>
      </AppDialogActions>
    </FileBrowserDialog>
  );
};
