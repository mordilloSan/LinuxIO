import DownloadIcon from "@mui/icons-material/Download";
import FolderIcon from "@mui/icons-material/Folder";
import InsertDriveFileIcon from "@mui/icons-material/InsertDriveFile";
import LinkIcon from "@mui/icons-material/Link";
import VisibilityOffIcon from "@mui/icons-material/VisibilityOff";
import {
  Box,
  Button,
  CircularProgress,
  Divider,
  Paper,
  Stack,
  Typography,
  alpha,
} from "@mui/material";
import React from "react";

import { FileResource, MultiStatsItem } from "../../types/filebrowser";

interface FileDetailProps {
  resource?: FileResource;
  multiItems?: MultiStatsItem[];
  onDownload: (path: string) => void;
  directorySize?: number | null;
  fileCount?: number | null;
  folderCount?: number | null;
  isLoadingDirectorySize?: boolean;
  totalSize?: number | null;
  totalFiles?: number | null;
  totalFolders?: number | null;
}

const formatFileSize = (bytes?: number): string => {
  if (bytes === undefined) return "Unknown";
  if (bytes === 0) return "0 Bytes";

  const k = 1024;
  const sizes = ["Bytes", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(k));

  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(2))} ${sizes[i]}`;
};

const formatDate = (dateString?: string): string => {
  if (!dateString) return "Unknown";
  try {
    const date = new Date(dateString);
    return date.toLocaleString();
  } catch {
    return dateString;
  }
};

const DetailRow: React.FC<{ label: string; value: React.ReactNode }> = ({
  label,
  value,
}) => (
  <Box sx={{ display: "flex", gap: 2 }}>
    <Typography
      variant="body2"
      fontWeight={600}
      color="text.secondary"
      sx={{ minWidth: 100 }}
    >
      {label}:
    </Typography>
    <Typography variant="body2" sx={{ flex: 1, wordBreak: "break-all" }}>
      {value}
    </Typography>
  </Box>
);

const FileDetail: React.FC<FileDetailProps> = ({
  resource,
  multiItems,
  onDownload,
  directorySize,
  fileCount,
  folderCount,
  isLoadingDirectorySize,
  totalSize,
  totalFiles,
  totalFolders,
}) => {
  const isMultiSelection = Boolean(multiItems?.length);

  if (isMultiSelection && multiItems) {
    return (
      <Paper
        variant="outlined"
        sx={{
          borderRadius: 2,
          display: "flex",
          flexDirection: "column",
          p: 3,
          gap: 2,
        }}
      >
        <Box
          sx={{
            display: "flex",
            justifyContent: "space-between",
            alignItems: "center",
            gap: 2,
          }}
        >
          <Box>
            <Typography variant="h6" fontWeight={600}>
              {multiItems.length} Selected Item
              {multiItems.length === 1 ? "" : "s"}
            </Typography>
            <Typography variant="body2" color="text.secondary">
              Combined statistics for the selected files and folders
            </Typography>
          </Box>
        </Box>

        <Divider />

        <Stack spacing={1.5}>
          <DetailRow
            label="Selected Items"
            value={multiItems.length.toLocaleString()}
          />
          <DetailRow
            label="Aggregate Size"
            value={
              totalSize !== undefined && totalSize !== null
                ? formatFileSize(totalSize)
                : "Unknown"
            }
          />
          <DetailRow
            label="Files (including nested)"
            value={
              totalFiles !== undefined && totalFiles !== null
                ? totalFiles.toLocaleString()
                : "Unknown"
            }
          />
          <DetailRow
            label="Folders (including nested)"
            value={
              totalFolders !== undefined && totalFolders !== null
                ? totalFolders.toLocaleString()
                : "Unknown"
            }
          />
        </Stack>

        <Divider />

        <Stack spacing={1}>
          {multiItems.map((item) => {
            const isDir = item.type === "directory";
            return (
              <Box
                key={item.path}
                sx={{
                  border: "1px solid",
                  borderColor: "divider",
                  borderRadius: 1.5,
                  p: 1.5,
                  display: "flex",
                  flexDirection: "column",
                  gap: 0.5,
                }}
              >
                <Box
                  sx={{
                    display: "flex",
                    justifyContent: "space-between",
                    alignItems: "center",
                    gap: 2,
                  }}
                >
                  <Box>
                    <Typography variant="subtitle1" fontWeight={600}>
                      {item.name}
                    </Typography>
                    <Typography variant="body2" color="text.secondary">
                      {isDir ? "Directory" : "File"}
                    </Typography>
                  </Box>
                  {!isDir && (
                    <Button
                      size="small"
                      startIcon={<DownloadIcon fontSize="small" />}
                      onClick={() => onDownload(item.path)}
                    >
                      Download
                    </Button>
                  )}
                </Box>
                <Typography variant="body2" color="text.secondary">
                  Size: {formatFileSize(item.size)}
                </Typography>
                {isDir && (
                  <Typography variant="body2" color="text.secondary">
                    {`${item.fileCount ?? 0} file${item.fileCount === 1 ? "" : "s"}, ${item.folderCount ?? 0} folder${item.folderCount === 1 ? "" : "s"}`}
                  </Typography>
                )}
              </Box>
            );
          })}
        </Stack>
      </Paper>
    );
  }

  if (!resource) {
    return (
      <Paper
        variant="outlined"
        sx={{
          borderRadius: 2,
          p: 3,
        }}
      >
        <Typography variant="body2" color="text.secondary">
          Select an item to view its details.
        </Typography>
      </Paper>
    );
  }

  const isDirectory = resource.type === "directory";
  const isSymlink = resource.symlink;

  const getTypeIcon = () => {
    if (isSymlink) return <LinkIcon fontSize="large" />;
    if (isDirectory) return <FolderIcon fontSize="large" />;
    return <InsertDriveFileIcon fontSize="large" />;
  };

  const getTypeLabel = () => {
    if (isSymlink) return "Symbolic Link";
    if (isDirectory) return "Directory";
    return "File";
  };

  return (
    <Paper
      variant="outlined"
      sx={{
        borderRadius: 2,
        display: "flex",
        flexDirection: "column",
        p: 3,
        gap: 2,
      }}
    >
      {/* Header with icon and name */}
      <Box sx={{ display: "flex", alignItems: "center", gap: 2 }}>
        <Box sx={{ color: "primary.main" }}>{getTypeIcon()}</Box>
        <Box sx={{ flex: 1 }}>
          <Typography variant="h6" fontWeight={600}>
            {resource.name}
          </Typography>
          <Box sx={{ display: "flex", alignItems: "center", gap: 1, mt: 0.5 }}>
            <Typography variant="body2" color="text.secondary">
              {getTypeLabel()}
            </Typography>
            {resource.hidden && (
              <>
                <Typography variant="body2" color="text.secondary">
                  •
                </Typography>
                <Box sx={{ display: "flex", alignItems: "center", gap: 0.5 }}>
                  <VisibilityOffIcon sx={{ fontSize: 16 }} />
                  <Typography variant="body2" color="text.secondary">
                    Hidden
                  </Typography>
                </Box>
              </>
            )}
          </Box>
        </Box>
      </Box>

      <Divider />

      {/* Details section */}
      <Stack spacing={1.5}>
        <DetailRow label="Path" value={resource.path} />
        {!isDirectory ? (
          <DetailRow label="Size" value={formatFileSize(resource.size)} />
        ) : (
          <>
            <DetailRow
              label="Size"
              value={
                isLoadingDirectorySize ? (
                  <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
                    <CircularProgress size={16} />
                    <Typography variant="body2">Calculating...</Typography>
                  </Box>
                ) : directorySize !== undefined && directorySize !== null ? (
                  formatFileSize(directorySize)
                ) : (
                  "Unknown"
                )
              }
            />
            <DetailRow
              label="Contents"
              value={
                isLoadingDirectorySize ? (
                  <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
                    <CircularProgress size={16} />
                    <Typography variant="body2">Counting...</Typography>
                  </Box>
                ) : fileCount !== undefined && folderCount !== undefined ? (
                  `${fileCount} file${fileCount !== 1 ? "s" : ""}, ${folderCount} folder${folderCount !== 1 ? "s" : ""}`
                ) : (
                  "Unknown"
                )
              }
            />
          </>
        )}
        <DetailRow
          label="Modified"
          value={formatDate(resource.modified || resource.modTime)}
        />
      </Stack>

      {/* Content preview for text files */}
      {resource.content && (
        <>
          <Divider />
          <Typography variant="subtitle2" fontWeight={600}>
            Preview
          </Typography>
          <Box
            sx={{
              maxHeight: 320,
              overflowY: "auto",
              p: 2,
              borderRadius: 1,
              backgroundColor: (theme) =>
                alpha(theme.palette.text.primary, 0.04),
              fontFamily: "monospace",
              fontSize: "0.875rem",
            }}
          >
            <pre
              style={{
                margin: 0,
                whiteSpace: "pre-wrap",
                wordBreak: "break-word",
              }}
            >
              {resource.content}
            </pre>
          </Box>
        </>
      )}

      {/* Download button - only for files */}
      {!isDirectory && (
        <>
          <Divider />
          <Button
            variant="contained"
            startIcon={<DownloadIcon />}
            onClick={() => onDownload(resource.path)}
            sx={{ alignSelf: "flex-start" }}
          >
            Download
          </Button>
        </>
      )}
    </Paper>
  );
};

export default FileDetail;
