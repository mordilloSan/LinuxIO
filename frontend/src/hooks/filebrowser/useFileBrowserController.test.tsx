import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useFileBrowserController } from "./useFileBrowserController";

import type { FileItem, FileResource } from "@/types/filebrowser";

const mocks = vi.hoisted(() => ({
  archiveActions: {
    canCompressSelection: true,
    canExtractSelection: false,
    compressFormatDialog: null,
    handleCloseCompressFormatDialog: vi.fn(),
    handleCompressConfirm: vi.fn(),
    handleCompressSelection: vi.fn(),
    handleExtractSelection: vi.fn(),
  },
  backgroundJobs: {
    startDownload: vi.fn(),
    startUpload: vi.fn(),
  },
  capability: {
    isEnabled: true,
    status: "available",
  },
  clearFileSubfoldersCache: vi.fn(),
  dialogs: {
    createFileDialog: false,
    createFolderDialog: true,
    deleteDialog: false,
    detailTarget: ["/srv/projects/readme.md"],
    pendingDeletePaths: ["/srv/projects/readme.md"],
    permissionsDialog: null,
    setCreateFileDialog: vi.fn(),
    setCreateFolderDialog: vi.fn(),
    setDeleteDialog: vi.fn(),
    setDetailTarget: vi.fn(),
    setPendingDeletePaths: vi.fn(),
    setPermissionsDialog: vi.fn(),
  },
  dragAndDrop: {
    handleCancelOverwrite: vi.fn(),
    handleConfirmOverwrite: vi.fn(),
    handleDragEnter: vi.fn(),
    handleDragLeave: vi.fn(),
    handleDragOver: vi.fn(),
    handleDrop: vi.fn(),
    isDragOver: false,
    overwriteTargets: null,
    setOverwriteTargets: vi.fn(),
  },
  editor: {
    closeEditorDialog: false,
    editingPath: null,
    editorRef: { current: null },
    isEditorDirty: false,
    isSavingFile: false,
    setCloseEditorDialog: vi.fn(),
    setEditingPath: vi.fn(),
    setIsEditorDirty: vi.fn(),
    setIsSavingFile: vi.fn(),
    showQuickSave: false,
  },
  editorActions: {
    handleCloseEditor: vi.fn(),
    handleDiscardAndExit: vi.fn(),
    handleKeepEditing: vi.fn(),
    handleSaveAndExit: vi.fn(),
    handleSaveFile: vi.fn(),
  },
  filteredResource: undefined as FileResource | undefined,
  itemActions: {
    canShowDetails: true,
    handleCancelInlineRename: vi.fn(),
    handleChangePermissions: vi.fn(),
    handleCloseCreateFileDialog: vi.fn(),
    handleCloseCreateFolderDialog: vi.fn(),
    handleCloseDeleteDialog: vi.fn(),
    handleCloseDetailDialog: vi.fn(),
    handleClosePermissionsDialog: vi.fn(),
    handleCloseUnsupportedEditDialog: vi.fn(),
    handleConfirmCreateFile: vi.fn(),
    handleConfirmCreateFolder: vi.fn(),
    handleConfirmDelete: vi.fn(),
    handleConfirmInlineRename: vi.fn(),
    handleConfirmPermissions: vi.fn(),
    handleConfirmUnsupportedEdit: vi.fn(),
    handleContextMenuRename: vi.fn(),
    handleCreateFile: vi.fn(),
    handleCreateFolder: vi.fn(),
    handleDelete: vi.fn(),
    handleDoubleClickFile: vi.fn(),
    handleDownloadCurrent: vi.fn(),
    handleDownloadDetail: vi.fn(),
    handleDownloadSelected: vi.fn(),
    handleEditFile: vi.fn(),
    handleOpenContainingFolder: vi.fn(),
    handleShowDetails: vi.fn(),
    handleStartInlineRename: vi.fn(),
    renamingPath: null,
    unsupportedEditPath: null,
  },
  mutations: {
    changePermissions: vi.fn(),
    compressItems: vi.fn(),
    copyItems: vi.fn(),
    createFile: vi.fn(),
    createFolder: vi.fn(),
    deleteItems: vi.fn(),
    extractArchive: vi.fn(),
    moveItems: vi.fn(),
    renameItem: vi.fn(),
  },
  navigation: {
    handleOpenDirectory: vi.fn(),
    normalizedPath: "/srv/projects",
  },
  queries: {
    detailError: null,
    detailResource: undefined as FileResource | undefined,
    editingFileResource: undefined as FileResource | undefined,
    errorMessage: null,
    isEditingFileLoading: false,
    isPending: false,
    isStatPending: false,
    multiItemsStats: null,
    resource: undefined as FileResource | undefined,
    shouldShowDetailLoader: false,
    statData: null,
  },
  searchAndSort: {
    handlePathChange: vi.fn(),
    handleSearchChange: vi.fn(),
    handleSortChange: vi.fn(),
    searchQuery: "readme",
    setSearchQuery: vi.fn(),
  },
  selection: {
    clipboard: {
      operation: "cut",
      paths: ["/srv/projects/readme.md"],
    },
    handleCopy: vi.fn(),
    handleCut: vi.fn(),
    handlePaste: vi.fn(),
    selectedItems: [] as FileItem[],
    selectedPaths: new Set(["/srv/projects/readme.md"]),
    setSelectedPaths: vi.fn(),
  },
  upload: {
    fileInputRef: { current: null },
    folderInputRef: { current: null },
    isUploadProcessing: false,
    setIsUploadProcessing: vi.fn(),
    setUploadDialogOpen: vi.fn(),
    setUploadEntries: vi.fn(),
    uploadDialogOpen: false,
    uploadEntries: [],
    uploadSummary: {
      directories: 0,
      files: 0,
      totalBytes: 0,
    },
  },
  uploadActions: {
    handleClearUploadSelection: vi.fn(),
    handleCloseUploadDialog: vi.fn(),
    handlePickFiles: vi.fn(),
    handlePickFolder: vi.fn(),
    handleStartUpload: vi.fn(),
    handleUpload: vi.fn(),
    handleUploadInputChange: vi.fn(),
  },
  useFileBrowserArchiveActions: vi.fn(),
  useFileBrowserClipboardShortcuts: vi.fn(),
  useFileBrowserEditorActions: vi.fn(),
  useFileBrowserFilteredResource: vi.fn(),
  useFileBrowserItemActions: vi.fn(),
  useFileBrowserNavigation: vi.fn(),
  useFileBrowserSearchAndSort: vi.fn(),
  useFileBrowserUploadActions: vi.fn(),
  useFileDialogs: vi.fn(),
  useFileDragAndDrop: vi.fn(),
  useFileEditor: vi.fn(),
  useFileMutations: vi.fn(),
  useFileQueries: vi.fn(),
  useFileSelection: vi.fn(),
  useFileUpload: vi.fn(),
  useFileViewState: vi.fn(),
  viewState: {
    contextMenuPosition: { left: 12, top: 24 },
    handleSwitchView: vi.fn(),
    handleToggleHiddenFiles: vi.fn(),
    setContextMenuPosition: vi.fn(),
    setSortField: vi.fn(),
    setSortOrder: vi.fn(),
    showHiddenFiles: false,
    sortField: "name",
    sortOrder: "asc",
    viewMode: "card",
  },
}));

vi.mock("@/api", () => ({
  linuxio: {
    filebrowser: {
      resource_get: {
        queryKey: ({ path }: { path: string }) => [
          "filebrowser",
          "resource",
          path,
        ],
      },
    },
  },
}));

vi.mock("@/hooks/backgroundJobs/useBackgroundJobActions", () => ({
  useBackgroundJobActions: () => mocks.backgroundJobs,
}));

vi.mock("@/hooks/filebrowser/useFileBrowserArchiveActions", () => ({
  useFileBrowserArchiveActions: mocks.useFileBrowserArchiveActions,
}));

vi.mock("@/hooks/filebrowser/useFileBrowserClipboardShortcuts", () => ({
  useFileBrowserClipboardShortcuts: mocks.useFileBrowserClipboardShortcuts,
}));

vi.mock("@/hooks/filebrowser/useFileBrowserEditorActions", () => ({
  useFileBrowserEditorActions: mocks.useFileBrowserEditorActions,
}));

vi.mock("@/hooks/filebrowser/useFileBrowserFilteredResource", () => ({
  useFileBrowserFilteredResource: mocks.useFileBrowserFilteredResource,
}));

vi.mock("@/hooks/filebrowser/useFileBrowserItemActions", () => ({
  useFileBrowserItemActions: mocks.useFileBrowserItemActions,
}));

vi.mock("@/hooks/filebrowser/useFileBrowserNavigation", () => ({
  useFileBrowserNavigation: mocks.useFileBrowserNavigation,
}));

vi.mock("@/hooks/filebrowser/useFileBrowserSearchAndSort", () => ({
  useFileBrowserSearchAndSort: mocks.useFileBrowserSearchAndSort,
}));

vi.mock("@/hooks/filebrowser/useFileBrowserUploadActions", () => ({
  useFileBrowserUploadActions: mocks.useFileBrowserUploadActions,
}));

vi.mock("@/hooks/filebrowser/useFileDialogs", () => ({
  useFileDialogs: mocks.useFileDialogs,
}));

vi.mock("@/hooks/filebrowser/useFileDragAndDrop", () => ({
  useFileDragAndDrop: mocks.useFileDragAndDrop,
}));

vi.mock("@/hooks/filebrowser/useFileEditor", () => ({
  useFileEditor: mocks.useFileEditor,
}));

vi.mock("@/hooks/filebrowser/useFileMutations", () => ({
  useFileMutations: mocks.useFileMutations,
}));

vi.mock("@/hooks/filebrowser/useFileQueries", () => ({
  useFileQueries: mocks.useFileQueries,
}));

vi.mock("@/hooks/filebrowser/useFileSelection", () => ({
  useFileSelection: mocks.useFileSelection,
}));

vi.mock("@/hooks/filebrowser/useFileSubfolders", () => ({
  clearFileSubfoldersCache: mocks.clearFileSubfoldersCache,
}));

vi.mock("@/hooks/filebrowser/useFileUpload", () => ({
  useFileUpload: mocks.useFileUpload,
}));

vi.mock("@/hooks/filebrowser/useFileViewState", () => ({
  useFileViewState: mocks.useFileViewState,
}));

vi.mock("@/hooks/useCapabilities", () => ({
  useCapability: () => mocks.capability,
}));

const resource = {
  path: "/srv/projects",
  type: "directory",
  items: [{ name: "readme.md", path: "/srv/projects/readme.md", type: "file" }],
} as FileResource;

function applyDefaultHookReturns() {
  mocks.filteredResource = resource;
  mocks.queries.resource = resource;
  mocks.selection.selectedItems = [
    { name: "readme.md", path: "/srv/projects/readme.md", type: "file" },
  ];
  mocks.selection.selectedPaths = new Set(["/srv/projects/readme.md"]);
  mocks.viewState.contextMenuPosition = { left: 12, top: 24 };

  mocks.useFileBrowserArchiveActions.mockReturnValue(mocks.archiveActions);
  mocks.useFileBrowserEditorActions.mockReturnValue(mocks.editorActions);
  mocks.useFileBrowserFilteredResource.mockReturnValue(mocks.filteredResource);
  mocks.useFileBrowserItemActions.mockReturnValue(mocks.itemActions);
  mocks.useFileBrowserNavigation.mockReturnValue(mocks.navigation);
  mocks.useFileBrowserSearchAndSort.mockReturnValue(mocks.searchAndSort);
  mocks.useFileBrowserUploadActions.mockReturnValue(mocks.uploadActions);
  mocks.useFileDialogs.mockReturnValue(mocks.dialogs);
  mocks.useFileDragAndDrop.mockReturnValue(mocks.dragAndDrop);
  mocks.useFileEditor.mockReturnValue(mocks.editor);
  mocks.useFileMutations.mockReturnValue(mocks.mutations);
  mocks.useFileQueries.mockReturnValue(mocks.queries);
  mocks.useFileSelection.mockReturnValue(mocks.selection);
  mocks.useFileUpload.mockReturnValue(mocks.upload);
  mocks.useFileViewState.mockReturnValue(mocks.viewState);
}

function setup() {
  const queryClient = new QueryClient({
    defaultOptions: {
      mutations: { retry: false },
      queries: { retry: false },
    },
  });

  function wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
  }

  const hook = renderHook(() => useFileBrowserController(), { wrapper });
  return { ...hook, queryClient };
}

describe("useFileBrowserController", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    applyDefaultHookReturns();
  });

  it("returns the content and dialog prop groups used by the route", () => {
    const { result } = setup();

    expect(result.current.contentProps).toMatchObject({
      chrome: {
        indexerEnabled: true,
        indexerStatus: "available",
        normalizedPath: "/srv/projects",
        searchQuery: "readme",
        showHiddenFiles: false,
        sortOrder: "asc",
        viewMode: "card",
      },
      data: {
        errorMessage: null,
        filteredResource: resource,
        isPending: false,
        resource,
      },
      listing: {
        contextMenuOpen: true,
        onDelete: mocks.itemActions.handleDelete,
        onDownloadFile: mocks.itemActions.handleDoubleClickFile,
        onOpenDirectory: mocks.navigation.handleOpenDirectory,
        selectedPaths: mocks.selection.selectedPaths,
        sortField: "name",
        sortOrder: "asc",
        viewMode: "card",
      },
    });

    expect([...result.current.contentProps.listing.cutPaths]).toEqual([
      "/srv/projects/readme.md",
    ]);
    expect(result.current.dialogsProps).toMatchObject({
      contextMenu: {
        anchorPosition: { left: 12, top: 24 },
        canCompress: true,
        canExtract: false,
        canOpenContainingFolder: true,
        canRename: true,
        canShowDetails: true,
        hasClipboard: true,
        hasSelection: true,
      },
      create: {
        fileOpen: false,
        folderOpen: true,
      },
      deleteDialog: {
        open: false,
        pendingDeletePaths: ["/srv/projects/readme.md"],
      },
      details: {
        detailTarget: ["/srv/projects/readme.md"],
        hasMultipleTargets: false,
        hasSingleTarget: true,
      },
      editor: {
        closeEditorDialog: false,
        isDirty: false,
        isSaving: false,
        onSave: mocks.editorActions.handleSaveFile,
      },
      upload: {
        normalizedPath: "/srv/projects",
        open: false,
      },
    });
  });

  it("owns the surface context-menu behavior", () => {
    const { result } = setup();
    const preventDefault = vi.fn();

    act(() => {
      result.current.contentProps.surface.onContextMenu({
        clientX: 42,
        clientY: 84,
        preventDefault,
      } as unknown as React.MouseEvent<HTMLDivElement>);
    });

    expect(preventDefault).toHaveBeenCalledTimes(1);
    expect(mocks.viewState.setContextMenuPosition).toHaveBeenCalledWith({
      left: 42,
      top: 84,
    });

    act(() => result.current.dialogsProps.contextMenu.onClose());

    expect(mocks.viewState.setContextMenuPosition).toHaveBeenLastCalledWith(
      null,
    );
  });

  it("wires cross-domain callbacks for selection clearing and listing invalidation", () => {
    const { queryClient } = setup();
    const invalidateQueries = vi.spyOn(queryClient, "invalidateQueries");
    const mutationArgs = mocks.useFileMutations.mock.calls[0]?.[0];
    const dragArgs = mocks.useFileDragAndDrop.mock.calls[0]?.[0];
    const uploadArgs = mocks.useFileBrowserUploadActions.mock.calls[0]?.[0];

    expect(mutationArgs).toMatchObject({
      normalizedPath: "/srv/projects",
      queryClient,
    });
    expect(dragArgs).toMatchObject({
      normalizedPath: "/srv/projects",
      resource,
    });
    expect(uploadArgs).toMatchObject({
      isUploadProcessing: false,
      normalizedPath: "/srv/projects",
      setOverwriteTargets: mocks.dragAndDrop.setOverwriteTargets,
    });

    act(() => mutationArgs.onDeleteSuccess());

    const clearedSelection =
      mocks.selection.setSelectedPaths.mock.calls.at(-1)?.[0];
    expect(clearedSelection).toBeInstanceOf(Set);
    expect([...(clearedSelection as Set<string>)]).toEqual([]);

    act(() => dragArgs.onUploadComplete());
    act(() => uploadArgs.invalidateListing());

    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: ["filebrowser", "resource", "/srv/projects"],
    });
    expect(mocks.clearFileSubfoldersCache).toHaveBeenCalledWith(queryClient);
  });
});
