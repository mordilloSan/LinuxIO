import { Icon } from "@iconify/react";
import React, { Suspense } from "react";

import FileBrowserDialog from "@/components/dialog/GeneralDialog";
import FileBrowserHeader from "@/components/filebrowser/FileBrowserHeader";
import FileDetail from "@/components/filebrowser/FileDetail";
import type { FileEditorHandle } from "@/components/filebrowser/FileEditor";
import MultiFileDetail from "@/components/filebrowser/MultiFileDetail";
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
import type { DroppedEntry } from "@/hooks/filebrowser/useFileDragAndDrop";
import type { UploadSummary } from "@/hooks/filebrowser/useFileUpload";
import { useAppTheme } from "@/theme";
import type {
  FileResource,
  MultiStatsItem,
  ResourceStatData,
  ViewMode,
} from "@/types/filebrowser";

const FileEditor = React.lazy(
  () => import("@/components/filebrowser/FileEditor"),
);

interface IndexerUnavailableAlertProps {
  status: string;
}

export const IndexerUnavailableAlert: React.FC<
  IndexerUnavailableAlertProps
> = ({ status }) => (
  <AppAlert
    severity="info"
    style={{
      marginLeft: 8,
      marginRight: 8,
      marginTop: 4,
    }}
  >
    <AppAlertTitle>
      {status === "unknown"
        ? "Checking Indexer Availability"
        : "Indexer API Unavailable"}
    </AppAlertTitle>
    {status === "unknown" ? (
      <AppTypography variant="body2">
        Directory size calculations and file search stay disabled until indexer
        availability is confirmed.
      </AppTypography>
    ) : (
      <AppTypography variant="body2">
        Directory size calculations and file search are disabled. Start the{" "}
        <strong>indexer.target</strong> or <strong>indexer.socket</strong> to
        enable these features.
      </AppTypography>
    )}
  </AppAlert>
);

interface FileDropOverlayProps {
  normalizedPath: string;
}

export const FileDropOverlay: React.FC<FileDropOverlayProps> = ({
  normalizedPath,
}) => {
  const theme = useAppTheme();

  return (
    <div
      style={{
        position: "absolute",
        inset: 0,
        border: `2px dashed ${theme.palette.primary.main}`,
        backgroundColor: "rgba(var(--app-palette-primary-mainChannel) / 0.08)",
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
  );
};

interface FileBrowserEditorDialogProps {
  editingFileResource?: FileResource;
  editingPath: string | null;
  editorRef: React.RefObject<FileEditorHandle | null>;
  isDirty: boolean;
  isEditingFileLoading: boolean;
  isSaving: boolean;
  onCloseEditor: () => void;
  onDirtyChange: (isDirty: boolean) => void;
  onSaveFile: () => Promise<void>;
  onSearchChange: (value: string) => void;
  onSwitchView: () => void;
  onToggleHiddenFiles: () => void;
  searchQuery: string;
  showHiddenFiles: boolean;
  showQuickSave: boolean;
  viewIcon: React.ReactNode;
  viewMode: ViewMode;
}

export const FileBrowserEditorDialog: React.FC<
  FileBrowserEditorDialogProps
> = ({
  editingFileResource,
  editingPath,
  editorRef,
  isDirty,
  isEditingFileLoading,
  isSaving,
  onCloseEditor,
  onDirtyChange,
  onSaveFile,
  onSearchChange,
  onSwitchView,
  onToggleHiddenFiles,
  searchQuery,
  showHiddenFiles,
  showQuickSave,
  viewIcon,
  viewMode,
}) => {
  const theme = useAppTheme();

  return (
    <AppFullscreenDialog
      contentStyle={{ backgroundColor: theme.palette.background.default }}
      onClose={onCloseEditor}
      open={Boolean(editingPath)}
    >
      <FileBrowserHeader
        editingFileName={editingFileResource?.name}
        editingFilePath={editingPath || undefined}
        isDirty={isDirty}
        isSaving={isSaving}
        onCloseEditor={onCloseEditor}
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
              isSaving={isSaving}
              onDirtyChange={onDirtyChange}
              onSave={onSaveFile}
              ref={editorRef}
            />
          </Suspense>
        )}
      </div>
    </AppFullscreenDialog>
  );
};

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
  isStatPending: boolean;
  multiItemsStats: MultiItemsStats;
  onClose: () => void;
  onDownload: (path: string) => void;
  onEdit: (path: string) => void;
  shouldShowDetailLoader: boolean;
  statData?: ResourceStatData | null;
}

export const FileBrowserDetailsDialog: React.FC<
  FileBrowserDetailsDialogProps
> = ({
  detailError,
  detailResource,
  detailTarget,
  hasMultipleDetailTargets,
  hasSingleDetailTarget,
  isStatPending,
  multiItemsStats,
  onClose,
  onDownload,
  onEdit,
  shouldShowDetailLoader,
  statData,
}) => {
  const theme = useAppTheme();

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
        <AppIconButton onClick={onClose} size="small">
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
            isLoadingStat={isStatPending}
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
  fileInputRef: React.RefObject<HTMLInputElement | null>;
  folderInputRef: React.RefObject<HTMLInputElement | null>;
  isUploadProcessing: boolean;
  normalizedPath: string;
  onChangeUploadInput: (event: React.ChangeEvent<HTMLInputElement>) => void;
  onClearUploadSelection: () => void;
  onClose: () => void;
  onPickFiles: () => void;
  onPickFolder: () => void;
  onStartUpload: () => Promise<void> | void;
  open: boolean;
  uploadEntries: DroppedEntry[];
  uploadSummary: UploadSummary;
}

export const FileBrowserUploadDialog: React.FC<
  FileBrowserUploadDialogProps
> = ({
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
}) => {
  const theme = useAppTheme();

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

interface FileBrowserOverwriteDialogProps {
  normalizedPath: string;
  onCancel: () => void;
  onConfirm: () => Promise<void> | void;
  overwriteTargets: DroppedEntry[] | null;
}

export const FileBrowserOverwriteDialog: React.FC<
  FileBrowserOverwriteDialogProps
> = ({ normalizedPath, onCancel, onConfirm, overwriteTargets }) => {
  const theme = useAppTheme();

  return (
    <FileBrowserDialog
      fullWidth
      maxWidth="sm"
      onClose={onCancel}
      open={Boolean(overwriteTargets?.length)}
    >
      <AppDialogTitle>Overwrite existing items?</AppDialogTitle>
      <AppDialogContent
        style={{ borderTop: `1px solid ${theme.palette.divider}` }}
      >
        <AppTypography style={{ marginBottom: 4 }} variant="body2">
          These items already exist in {normalizedPath}. Do you want to
          overwrite them?
        </AppTypography>
        <ul style={{ margin: 0, paddingLeft: 20 }}>
          {overwriteTargets?.map(({ relativePath }) => (
            <li key={relativePath}>
              <AppTypography variant="body2">{relativePath}</AppTypography>
            </li>
          ))}
        </ul>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={onCancel}>Skip</AppButton>
        <AppButton color="warning" onClick={onConfirm} variant="contained">
          Overwrite
        </AppButton>
      </AppDialogActions>
    </FileBrowserDialog>
  );
};
