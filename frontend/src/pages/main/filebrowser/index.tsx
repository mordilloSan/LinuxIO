import CloseIcon from "@mui/icons-material/Close";
import GridViewIcon from "@mui/icons-material/GridView";
import ViewListIcon from "@mui/icons-material/ViewList";
import {
  Box,
  Dialog,
  DialogContent,
  DialogTitle,
  IconButton,
  Typography,
} from "@mui/material";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import React, {
  ReactNode,
  useCallback,
  useMemo,
  useRef,
  useState,
} from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { toast } from "sonner";

import BreadcrumbsNav from "@/components/filebrowser/Breadcrumbs";
import ConfirmDialog from "@/components/filebrowser/ConfirmDialog";
import ContextMenu from "@/components/filebrowser/ContextMenu";
import DirectoryListing from "@/components/filebrowser/DirectoryListing";
import ErrorState from "@/components/filebrowser/ErrorState";
import FileBrowserHeader from "@/components/filebrowser/FileBrowserHeader";
import FileDetail from "@/components/filebrowser/FileDetail";
import FileEditor from "@/components/filebrowser/FileEditor";
import { FileEditorHandle } from "@/components/filebrowser/FileEditor";
import InputDialog from "@/components/filebrowser/InputDialog";
import MultiFileDetail from "@/components/filebrowser/MultiFileDetail";
import SortBar, {
  SortField,
  SortOrder,
} from "@/components/filebrowser/SortBar";
import UnsavedChangesDialog from "@/components/filebrowser/UnsavedChangesDialog";
import {
  normalizeResource,
  buildDownloadUrl,
} from "@/components/filebrowser/utils";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import { useConfigValue } from "@/hooks/useConfig";
import { useMultipleDirectoryDetails } from "@/hooks/useMultipleDirectoryDetails";
import {
  ViewMode,
  ApiResource,
  FileResource,
  FileItem,
  ResourceStatData,
} from "@/types/filebrowser";
import axios from "@/utils/axios"; // Still used for mutations (create, delete)

const viewModes: ViewMode[] = ["card", "list"];

const viewIconMap: Record<ViewMode, ReactNode> = {
  card: <GridViewIcon fontSize="small" />,
  list: <ViewListIcon fontSize="small" />,
};

const FileBrowser: React.FC = () => {
  const location = useLocation();
  const navigate = useNavigate();

  const [viewMode, setViewMode] = useState<ViewMode>("card");
  const [showHiddenFiles, setShowHiddenFilesConfig] =
    useConfigValue("showHiddenFiles");
  const [sortField, setSortField] = useState<SortField>("name");
  const [sortOrder, setSortOrder] = useState<SortOrder>("asc");
  const [contextMenuPosition, setContextMenuPosition] = useState<{
    top: number;
    left: number;
  } | null>(null);
  const [selectedPaths, setSelectedPaths] = useState<Set<string>>(new Set());
  const [createFileDialog, setCreateFileDialog] = useState(false);
  const [createFolderDialog, setCreateFolderDialog] = useState(false);
  const [deleteDialog, setDeleteDialog] = useState(false);
  const [pendingDeletePaths, setPendingDeletePaths] = useState<string[]>([]);
  const [detailTarget, setDetailTarget] = useState<string[] | null>(null);
  const [editingPath, setEditingPath] = useState<string | null>(null);
  const [isSavingFile, setIsSavingFile] = useState(false);
  const [isEditorDirty, setIsEditorDirty] = useState(false);
  const [closeEditorDialog, setCloseEditorDialog] = useState(false);
  const editorRef = useRef<FileEditorHandle>(null);

  const queryClient = useQueryClient();

  const showQuickSave = editingPath !== null;

  // Extract path from URL: /filebrowser/path/to/dir -> /path/to/dir
  const urlPath = location.pathname.replace(/^\/filebrowser\/?/, "");
  const normalizedPath = urlPath ? `/${urlPath}` : "/";

  // Mutations
  const { mutate: createFile } = useMutation({
    mutationFn: async (fileName: string) => {
      const path = `${normalizedPath}${normalizedPath.endsWith("/") ? "" : "/"}${fileName}`;
      await axios.post("/navigator/api/resources", null, {
        params: { path, source: "/" },
      });
    },
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ["fileResource", normalizedPath],
      });
      toast.success("File created successfully");
    },
    onError: (error: any) => {
      toast.error(error.response?.data?.error || "Failed to create file");
    },
  });

  const { mutate: createFolder } = useMutation({
    mutationFn: async (folderName: string) => {
      const path = `${normalizedPath}${normalizedPath.endsWith("/") ? "" : "/"}${folderName}/`;
      await axios.post("/navigator/api/resources", null, {
        params: { path, source: "/" },
      });
    },
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ["fileResource", normalizedPath],
      });
      toast.success("Folder created successfully");
    },
    onError: (error: any) => {
      toast.error(error.response?.data?.error || "Failed to create folder");
    },
  });

  const { mutate: deleteItems } = useMutation({
    mutationFn: async (paths: string[]) => {
      // DEBUG: Using Promise.all to test goroutine leak in backend
      await Promise.all(
        paths.map((path) =>
          axios.delete("/navigator/api/resources", {
            params: { path, source: "/" },
          }),
        ),
      );
    },
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ["fileResource", normalizedPath],
      });
      setSelectedPaths(new Set());
      toast.success("Items deleted successfully");
    },
    onError: (error: any) => {
      toast.error(error.response?.data?.error || "Failed to delete items");
    },
  });

  const {
    data: resource,
    isPending,
    isError,
    error,
  } = useQuery<FileResource>({
    queryKey: ["fileResource", normalizedPath],
    queryFn: async () => {
      const { data } = await axios.get<ApiResource>(
        "/navigator/api/resources",
        {
          params: {
            path: normalizedPath,
            source: "/",
          },
        },
      );
      return normalizeResource(data);
    },
  });

  const errorMessage = isError
    ? error instanceof Error
      ? (() => {
          // Check if it's an axios error with a status code
          const axiosError = error as any;
          if (axiosError.response?.status === 403) {
            return `Permission denied: You don't have access to "${normalizedPath}".`;
          }
          if (
            axiosError.response?.status === 404 ||
            axiosError.response?.status === 500
          ) {
            return `Path not found: "${normalizedPath}" does not exist.`;
          }
          return error.message;
        })()
      : "Failed to load file information."
    : null;

  const viewIcon = useMemo(() => viewIconMap[viewMode], [viewMode]);

  const handleSwitchView = useCallback(() => {
    setViewMode((current) => {
      const index = viewModes.indexOf(current);
      const next = (index + 1) % viewModes.length;
      return viewModes[next];
    });
  }, []);

  const canShowDetails = selectedPaths.size > 0;

  // For single item detail
  const detailTargetCount = detailTarget?.length ?? 0;
  const hasSingleDetailTarget = detailTargetCount === 1;
  const hasMultipleDetailTargets = detailTargetCount > 1;

  const {
    data: detailResource,
    isPending: isDetailPending,
    error: detailError,
  } = useQuery<FileResource>({
    queryKey: ["fileDetail", detailTarget],
    queryFn: async () => {
      const currentDetailTarget = detailTarget;
      if (!currentDetailTarget || currentDetailTarget.length !== 1) {
        throw new Error("Invalid selection");
      }
      const { data } = await axios.get<ApiResource>(
        "/navigator/api/resources",
        {
          params: { path: currentDetailTarget[0], content: "true" },
        },
      );
      return data as FileResource;
    },
    enabled: hasSingleDetailTarget,
  });

  // Directory details are now fetched by the FileDetail component using useDirectorySize hook

  // For stat data (permissions and ownership) on single item
  const { data: statData, isPending: isStatPending } =
    useQuery<ResourceStatData>({
      queryKey: ["fileStat", detailTarget],
      queryFn: async () => {
        const currentDetailTarget = detailTarget;
        if (!currentDetailTarget || currentDetailTarget.length !== 1) {
          throw new Error("Invalid selection");
        }
        const { data } = await axios.get<ResourceStatData>(
          "/navigator/api/resources/stat",
          {
            params: { path: currentDetailTarget[0] },
          },
        );
        return data;
      },
      enabled: hasSingleDetailTarget,
    });

  // For multiple file details
  // DEBUG: Using Promise.all to test goroutine leak in backend
  const { data: multipleFileResources, isPending: isMultipleFilesPending } =
    useQuery<Record<string, FileResource>>({
      queryKey: ["multipleFileDetails", detailTarget],
      queryFn: async () => {
        const currentDetailTarget = detailTarget;
        if (!currentDetailTarget || currentDetailTarget.length <= 1) {
          throw new Error("Invalid selection");
        }
        const results: Record<string, FileResource> = {};
        await Promise.all(
          currentDetailTarget.map(async (path) => {
            const { data } = await axios.get<ApiResource>(
              "/navigator/api/resources",
              {
                params: { path },
              },
            );
            results[path] = normalizeResource(data);
          }),
        );
        return results;
      },
      enabled: hasMultipleDetailTargets,
    });

  // Use the hook to fetch directory details for multiple items
  const fileResourceMap = useMemo(() => {
    if (!multipleFileResources) return {};
    return Object.entries(multipleFileResources).reduce(
      (acc, [path, resource]) => {
        acc[path] = {
          name: resource.name,
          type: resource.type,
          size: resource.size || 0,
        };
        return acc;
      },
      {} as Record<string, { name: string; type: string; size: number }>,
    );
  }, [multipleFileResources]);

  const multiItemsStats = useMultipleDirectoryDetails(
    detailTarget || [],
    fileResourceMap,
  );

  // For file content when editing
  const { data: editingFileResource, isPending: isEditingFileLoading } =
    useQuery<FileResource>({
      queryKey: ["fileEdit", editingPath],
      queryFn: async () => {
        if (!editingPath) throw new Error("No editing path");
        const { data } = await axios.get<ApiResource>(
          "/navigator/api/resources",
          {
            params: { path: editingPath, content: "true" },
          },
        );
        console.log("Loaded file resource:", data);
        return data as FileResource;
      },
      enabled: !!editingPath,
    });

  const handleCloseDetailDialog = useCallback(() => {
    setDetailTarget(null);
  }, []);

  const handleToggleHiddenFiles = useCallback(() => {
    setShowHiddenFilesConfig((prev) => !prev);
  }, [setShowHiddenFilesConfig]);

  const handleSortChange = useCallback((field: SortField) => {
    setSortField((currentField) => {
      if (currentField === field) {
        // Toggle sort order if clicking the same field
        setSortOrder((currentOrder) =>
          currentOrder === "asc" ? "desc" : "asc",
        );
        return field;
      } else {
        // Reset to ascending when changing fields
        setSortOrder("asc");
        return field;
      }
    });
  }, []);

  const handleOpenDirectory = useCallback(
    (path: string) => {
      if (path === "/") {
        navigate("/filebrowser");
        return;
      }
      // Remove leading slash for the URL path
      const urlPath = path.startsWith("/") ? path.slice(1) : path;
      navigate(`/filebrowser/${urlPath}`);
    },
    [navigate],
  );

  const handleDoubleClickFile = useCallback((item: FileItem) => {
    setDetailTarget([item.path]);
  }, []);

  const handleDownloadCurrent = useCallback((path: string) => {
    const url = buildDownloadUrl(path);
    window.open(url, "_blank", "noopener,noreferrer");
  }, []);

  const handleContextMenu = useCallback((event: React.MouseEvent) => {
    event.preventDefault();
    setContextMenuPosition({
      top: event.clientY,
      left: event.clientX,
    });
  }, []);

  const handleCloseContextMenu = useCallback(() => {
    setContextMenuPosition(null);
  }, []);

  const handleShowDetails = useCallback(() => {
    handleCloseContextMenu();
    if (selectedPaths.size === 0) return;
    setDetailTarget(Array.from(selectedPaths));
  }, [handleCloseContextMenu, selectedPaths]);

  const handleDownloadDetail = useCallback((path: string) => {
    const url = buildDownloadUrl(path);
    window.open(url, "_blank", "noopener,noreferrer");
  }, []);

  // Context menu action handlers
  const handleCreateFile = useCallback(() => {
    handleCloseContextMenu();
    setCreateFileDialog(true);
  }, [handleCloseContextMenu]);

  const handleCreateFolder = useCallback(() => {
    handleCloseContextMenu();
    setCreateFolderDialog(true);
  }, [handleCloseContextMenu]);

  const handleConfirmCreateFile = useCallback(
    (fileName: string) => {
      createFile(fileName);
    },
    [createFile],
  );

  const handleConfirmCreateFolder = useCallback(
    (folderName: string) => {
      createFolder(folderName);
    },
    [createFolder],
  );

  const handleChangePermissions = useCallback(() => {
    handleCloseContextMenu();
    // TODO: Implement permissions dialog
    console.log("Change permissions clicked for:", Array.from(selectedPaths));
  }, [handleCloseContextMenu, selectedPaths]);

  const handleCopy = useCallback(() => {
    handleCloseContextMenu();
    // TODO: Implement copy functionality
    console.log("Copy clicked for:", Array.from(selectedPaths));
  }, [handleCloseContextMenu, selectedPaths]);

  const handleMove = useCallback(() => {
    handleCloseContextMenu();
    // TODO: Implement move functionality
    console.log("Move clicked for:", Array.from(selectedPaths));
  }, [handleCloseContextMenu, selectedPaths]);

  const handleDelete = useCallback(() => {
    handleCloseContextMenu();
    const paths = Array.from(selectedPaths);
    console.log("Delete clicked, selectedPaths:", paths);
    if (paths.length > 0) {
      setPendingDeletePaths(paths);
      setDeleteDialog(true);
    } else {
      toast.error("No items selected");
    }
  }, [handleCloseContextMenu, selectedPaths]);

  const handleConfirmDelete = useCallback(() => {
    if (!pendingDeletePaths.length) {
      return;
    }
    console.log("Confirming delete for paths:", pendingDeletePaths);
    deleteItems(pendingDeletePaths);
    setPendingDeletePaths([]);
  }, [deleteItems, pendingDeletePaths]);

  const handleCloseDeleteDialog = useCallback(() => {
    setDeleteDialog(false);
    setPendingDeletePaths([]);
  }, []);

  const handleDownloadSelected = useCallback(() => {
    handleCloseContextMenu();
    const paths = Array.from(selectedPaths);
    if (paths.length === 0) return;

    // Build download URLs for each selected item
    const filesParam = paths
      .map((path) => `${encodeURIComponent("/")}::${encodeURIComponent(path)}`)
      .join("||");
    const url = `/navigator/api/raw?files=${filesParam}`;
    window.open(url, "_blank", "noopener,noreferrer");
  }, [handleCloseContextMenu, selectedPaths]);

  const handleUpload = useCallback(() => {
    handleCloseContextMenu();
    // TODO: Implement upload dialog
    console.log("Upload clicked");
  }, [handleCloseContextMenu]);

  const handleEditFile = useCallback((filePath: string) => {
    setEditingPath(filePath);
    setDetailTarget(null); // Close the detail dialog
  }, []);

  const handleSaveFile = useCallback(async () => {
    if (!editorRef.current || !editingPath) return;

    try {
      setIsSavingFile(true);
      const content = editorRef.current.getContent();

      console.log(
        "Saving file:",
        editingPath,
        "Content length:",
        content.length,
      );

      const response = await axios.put("/navigator/api/resources", content, {
        params: { path: editingPath },
        headers: { "Content-Type": "text/plain" },
      });

      console.log("Save response:", response);
      toast.success("File saved successfully!");
      setIsEditorDirty(false);

      // Invalidate the file cache so it reloads with new content
      queryClient.invalidateQueries({
        queryKey: ["fileEdit", editingPath],
      });
    } catch (error: any) {
      console.error("Save error:", error);
      toast.error(error.response?.data?.error || "Failed to save file");
    } finally {
      setIsSavingFile(false);
    }
  }, [editingPath, queryClient]);

  const handleCloseEditor = useCallback(() => {
    if (isEditorDirty) {
      setCloseEditorDialog(true);
    } else {
      setEditingPath(null);
      setIsEditorDirty(false);
    }
  }, [isEditorDirty]);

  const handleKeepEditing = useCallback(() => {
    setCloseEditorDialog(false);
  }, []);

  const handleDiscardAndExit = useCallback(() => {
    setEditingPath(null);
    setIsEditorDirty(false);
    setCloseEditorDialog(false);
  }, []);

  const handleSaveAndExit = useCallback(async () => {
    if (!editorRef.current || !editingPath) return;

    try {
      setIsSavingFile(true);
      const content = editorRef.current.getContent();

      await axios.put("/navigator/api/resources", content, {
        params: { path: editingPath },
        headers: { "Content-Type": "text/plain" },
      });

      toast.success("File saved successfully!");
      setIsEditorDirty(false);
      setEditingPath(null);
      setCloseEditorDialog(false);

      queryClient.invalidateQueries({
        queryKey: ["fileEdit", editingPath],
      });
    } catch (error: any) {
      toast.error(error.response?.data?.error || "Failed to save file");
    } finally {
      setIsSavingFile(false);
    }
  }, [editingPath, queryClient]);

  const shouldShowDetailLoader =
    (hasSingleDetailTarget && isDetailPending) ||
    (hasMultipleDetailTargets && isMultipleFilesPending);

  return (
    <>
      <Box
        data-allow-context-menu="true"
        sx={{
          height: "100%",
          width: "100%",
          display: "flex",
          flexDirection: "column",
          overflow: "hidden",
        }}
        onContextMenu={handleContextMenu}
      >
        <FileBrowserHeader
          viewMode={viewMode}
          showHiddenFiles={showHiddenFiles}
          showQuickSave={showQuickSave}
          onSwitchView={handleSwitchView}
          onToggleHiddenFiles={handleToggleHiddenFiles}
          onSaveFile={handleSaveFile}
          onCloseEditor={handleCloseEditor}
          isSaving={isSavingFile}
          viewIcon={viewIcon}
          editingFileName={editingFileResource?.name}
          editingFilePath={editingPath || undefined}
          isDirty={isEditorDirty}
        />
        <Box
          sx={{
            px: editingPath ? 0 : 2,
            flex: 1,
            minHeight: 0,
            display: "flex",
            flexDirection: "column",
          }}
        >
          {!editingPath && (
            <>
              <BreadcrumbsNav
                path={normalizedPath}
                onNavigate={handleOpenDirectory}
              />

              {!isPending &&
                !errorMessage &&
                resource &&
                resource.type === "directory" && (
                  <SortBar
                    sortOrder={sortOrder}
                    onSortChange={handleSortChange}
                  />
                )}
            </>
          )}
          <Box
            sx={{
              px: editingPath ? 0 : 2,
              flex: 1,
              minHeight: 0,
              display: "flex",
              flexDirection: "column",
            }}
          >
            {isPending && <ComponentLoader />}

            {!isPending && errorMessage && (
              <ErrorState
                message={errorMessage}
                onReset={() => handleOpenDirectory("/")}
              />
            )}

            {!editingPath &&
              !isPending &&
              !errorMessage &&
              resource &&
              resource.type === "directory" && (
                <DirectoryListing
                  key={normalizedPath}
                  resource={resource}
                  showHiddenFiles={showHiddenFiles}
                  viewMode={viewMode}
                  sortField={sortField}
                  sortOrder={sortOrder}
                  onOpenDirectory={handleOpenDirectory}
                  onDownloadFile={handleDoubleClickFile}
                  selectedPaths={selectedPaths}
                  onSelectedPathsChange={setSelectedPaths}
                  isContextMenuOpen={Boolean(contextMenuPosition)}
                  onDelete={handleDelete}
                />
              )}

            {editingPath && isEditingFileLoading && <ComponentLoader />}

            {!isPending &&
              !errorMessage &&
              editingPath &&
              !isEditingFileLoading &&
              editingFileResource && (
                <FileEditor
                  ref={editorRef}
                  filePath={editingPath}
                  fileName={editingFileResource.name}
                  initialContent={editingFileResource.content || ""}
                  onSave={handleSaveFile}
                  isSaving={isSavingFile}
                  onDirtyChange={setIsEditorDirty}
                />
              )}

            {!editingPath &&
              !isPending &&
              !errorMessage &&
              resource &&
              resource.type !== "directory" && (
                <FileDetail
                  resource={resource}
                  onDownload={handleDownloadCurrent}
                  onEdit={handleEditFile}
                />
              )}
          </Box>
        </Box>
      </Box>

      <ContextMenu
        anchorPosition={contextMenuPosition}
        hasSelection={selectedPaths.size > 0}
        onClose={handleCloseContextMenu}
        onCreateFile={handleCreateFile}
        onCreateFolder={handleCreateFolder}
        onChangePermissions={handleChangePermissions}
        onCopy={handleCopy}
        onMove={handleMove}
        onDelete={handleDelete}
        onDownload={handleDownloadSelected}
        onUpload={handleUpload}
        onShowDetails={handleShowDetails}
        canShowDetails={canShowDetails}
      />

      <Dialog
        open={Boolean(detailTarget)}
        onClose={handleCloseDetailDialog}
        maxWidth="md"
        fullWidth
      >
        <DialogTitle
          sx={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            pr: 2,
          }}
        >
          {detailTarget && detailTarget.length > 1
            ? "Multiple Items Details"
            : "File Details"}
          <IconButton onClick={handleCloseDetailDialog} size="small">
            <CloseIcon fontSize="small" />
          </IconButton>
        </DialogTitle>
        <DialogContent dividers sx={{ minHeight: 200 }}>
          {shouldShowDetailLoader && <ComponentLoader />}
          {!shouldShowDetailLoader && hasSingleDetailTarget && detailError && (
            <Typography color="error">
              {detailError instanceof Error
                ? detailError.message
                : "Failed to load details"}
            </Typography>
          )}
          {detailResource && (
            <FileDetail
              resource={detailResource}
              onDownload={handleDownloadDetail}
              onEdit={handleEditFile}
              statData={statData}
              isLoadingStat={isStatPending}
            />
          )}
          {hasMultipleDetailTargets && multiItemsStats.items.length > 0 && (
            <MultiFileDetail
              multiItems={multiItemsStats.items}
              totalSize={multiItemsStats.totalSize}
              onDownload={handleDownloadDetail}
              isLoadingDetails={multiItemsStats.isAnyLoading}
            />
          )}
        </DialogContent>
      </Dialog>

      <InputDialog
        open={createFileDialog}
        title="Create File"
        label="File Name"
        onClose={() => setCreateFileDialog(false)}
        onConfirm={handleConfirmCreateFile}
      />

      <InputDialog
        open={createFolderDialog}
        title="Create Folder"
        label="Folder Name"
        onClose={() => setCreateFolderDialog(false)}
        onConfirm={handleConfirmCreateFolder}
      />

      <ConfirmDialog
        open={deleteDialog}
        title="Delete Items"
        message={`Are you sure you want to delete ${pendingDeletePaths.length} item${pendingDeletePaths.length !== 1 ? "s" : ""}?`}
        confirmText="Delete"
        onClose={handleCloseDeleteDialog}
        onConfirm={handleConfirmDelete}
      />

      <UnsavedChangesDialog
        open={closeEditorDialog}
        onKeepEditing={handleKeepEditing}
        onDiscardAndExit={handleDiscardAndExit}
        onSaveAndExit={handleSaveAndExit}
        isSaving={isSavingFile}
      />
    </>
  );
};

export type { ViewMode };
export default FileBrowser;
