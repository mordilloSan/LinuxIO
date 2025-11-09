import {
  GridView as GridViewIcon,
  ViewList as ViewListIcon,
  ViewModule as ViewModuleIcon,
  ViewStream as ViewStreamIcon,
} from "@mui/icons-material";
import { Box } from "@mui/material";
import {
  useQuery,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import React, { ReactNode, useCallback, useMemo, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { toast } from "sonner";

import BreadcrumbsNav from "@/components/filebrowser/Breadcrumbs";
import ConfirmDialog from "@/components/filebrowser/ConfirmDialog";
import ContextMenu from "@/components/filebrowser/ContextMenu";
import DirectoryListing from "@/components/filebrowser/DirectoryListing";
import ErrorState from "@/components/filebrowser/ErrorState";
import FileBrowserHeader from "@/components/filebrowser/FileBrowserHeader";
import FileDetail from "@/components/filebrowser/FileDetail";
import InputDialog from "@/components/filebrowser/InputDialog";
import SortBar, {
  SortField,
  SortOrder,
} from "@/components/filebrowser/SortBar";
import {
  normalizeResource,
  buildDownloadUrl,
} from "@/components/filebrowser/utils";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import { useConfigValue } from "@/hooks/useConfig";
import {
  ViewMode,
  ApiResource,
  FileResource,
  FileItem,
} from "@/types/filebrowser";
import axios from "@/utils/axios"; // Still used for mutations (create, delete)

const viewModes: ViewMode[] = ["list", "compact", "normal", "gallery"];

const viewIconMap: Record<ViewMode, ReactNode> = {
  list: <ViewListIcon fontSize="small" />,
  compact: <ViewStreamIcon fontSize="small" />,
  normal: <ViewModuleIcon fontSize="small" />,
  gallery: <GridViewIcon fontSize="small" />,
};

const FileBrowser: React.FC = () => {
  const location = useLocation();
  const navigate = useNavigate();

  const [viewMode, setViewMode] = useState<ViewMode>("list");
  const [showHiddenFiles, setShowHiddenFilesConfig] =
    useConfigValue("showHiddenFiles");
  const [gallerySize, setGallerySize] = useState<number>(4);
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
  const showQuickSave = false;

  const queryClient = useQueryClient();

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

  const handleDownloadFile = useCallback((item: FileItem) => {
    const url = buildDownloadUrl(item.source, item.path);
    window.open(url, "_blank", "noopener,noreferrer");
  }, []);

  const handleDownloadCurrent = useCallback(
    (path: string) => {
      const url = buildDownloadUrl(resource?.source ?? "/", path);
      window.open(url, "_blank", "noopener,noreferrer");
    },
    [resource?.source],
  );

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

  return (
    <>
      <Box
        data-allow-context-menu="true"
        sx={{
          height: "100%",
        }}
        onContextMenu={handleContextMenu}
      >
        <FileBrowserHeader
          viewMode={viewMode}
          showHiddenFiles={showHiddenFiles}
          showQuickSave={showQuickSave}
          onSwitchView={handleSwitchView}
          onToggleHiddenFiles={handleToggleHiddenFiles}
          viewIcon={viewIcon}
        />
        <Box sx={{ px: 2 }}>
          <BreadcrumbsNav
            path={normalizedPath}
            onNavigate={handleOpenDirectory}
            showGallerySize={
              viewMode === "gallery" && (resource?.items?.length ?? 0) > 0
            }
            gallerySize={gallerySize}
            onGallerySizeChange={setGallerySize}
          />

          {!isPending &&
            !errorMessage &&
            resource &&
            resource.type === "directory" && (
              <SortBar sortOrder={sortOrder} onSortChange={handleSortChange} />
            )}
          <Box sx={{ px: 2 }}>
            {isPending && <ComponentLoader />}

            {!isPending && errorMessage && (
              <ErrorState
                message={errorMessage}
                onReset={() => handleOpenDirectory("/")}
              />
            )}

            {!isPending &&
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
                  onDownloadFile={handleDownloadFile}
                  selectedPaths={selectedPaths}
                  onSelectedPathsChange={setSelectedPaths}
                  isContextMenuOpen={Boolean(contextMenuPosition)}
                />
              )}

            {!isPending &&
              !errorMessage &&
              resource &&
              resource.type !== "directory" && (
                <FileDetail
                  resource={resource}
                  onDownload={handleDownloadCurrent}
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
      />

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
    </>
  );
};

export type { ViewMode };
export default FileBrowser;
