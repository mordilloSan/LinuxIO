import {
  GridView as GridViewIcon,
  ViewList as ViewListIcon,
  ViewModule as ViewModuleIcon,
  ViewStream as ViewStreamIcon,
} from "@mui/icons-material";
import { Box } from "@mui/material";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import React, { ReactNode, useCallback, useMemo, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";

import { useConfigValue } from "@/hooks/useConfig";
import axios from "@/utils/axios";
import BreadcrumbsNav from "@/components/filebrowser/Breadcrumbs";
import SortBar, { SortField, SortOrder } from "@/components/filebrowser/SortBar";
import FileBrowserHeader from "@/components/filebrowser/FileBrowserHeader";
import DirectoryListing from "@/components/filebrowser/DirectoryListing";
import FileDetail from "@/components/filebrowser/FileDetail";
import ErrorState from "@/components/filebrowser/ErrorState";
import { normalizeResource, buildDownloadUrl } from "@/components/filebrowser/utils";
import { ViewMode, ApiResource, FileResource, FileItem } from "@/types/filebrowser";
import PageLoader from "@/components/loaders/PageLoader";

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
  const [showHiddenFiles, setShowHiddenFilesConfig] = useConfigValue("showHiddenFiles");
  const [gallerySize, setGallerySize] = useState<number>(4);
  const [sortField, setSortField] = useState<SortField>("name");
  const [sortOrder, setSortOrder] = useState<SortOrder>("asc");
  const showQuickSave = false;

  // Extract path from URL: /filebrowser/path/to/dir -> /path/to/dir
  const urlPath = location.pathname.replace(/^\/filebrowser\/?/, "");
  const normalizedPath = urlPath ? `/${urlPath}` : "/";

  const {
    data: resource,
    isPending,
    isError,
    error,
  } = useQuery<FileResource>({
    queryKey: ["fileResource", normalizedPath],
    placeholderData: keepPreviousData,
    queryFn: async () => {
      const { data } = await axios.get<ApiResource>("/navigator/api/resources", {
        params: {
          path: normalizedPath,
          source: "/",
        },
      });
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
        if (axiosError.response?.status === 404 || axiosError.response?.status === 500) {
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
        setSortOrder((currentOrder) => (currentOrder === "asc" ? "desc" : "asc"));
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

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
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

        {!isPending && !errorMessage && resource && resource.type === "directory" && (
          <SortBar
            sortField={sortField}
            sortOrder={sortOrder}
            onSortChange={handleSortChange}
          />
        )}
        <Box sx={{ px: 2 }}>
          {isPending && <PageLoader />}

          {!isPending && errorMessage && (
            <ErrorState message={errorMessage} onReset={() => handleOpenDirectory("/")} />
          )}

          {!isPending && !errorMessage && resource && resource.type === "directory" && (
            <DirectoryListing
              key={normalizedPath}
              resource={resource}
              showHiddenFiles={showHiddenFiles}
              viewMode={viewMode}
              sortField={sortField}
              sortOrder={sortOrder}
              onOpenDirectory={handleOpenDirectory}
              onDownloadFile={handleDownloadFile}
            />
          )}

          {!isPending && !errorMessage && resource && resource.type !== "directory" && (
            <FileDetail resource={resource} onDownload={handleDownloadCurrent} />
          )}
        </Box>
      </Box>
    </Box>
  );
};

export type { ViewMode };
export default FileBrowser;
