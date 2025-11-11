import DownloadIcon from "@mui/icons-material/Download";
import EditIcon from "@mui/icons-material/Edit";
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

import { FileResource, ResourceStatData } from "../../types/filebrowser";

import { formatDate, formatFileSize } from "@/utils/formaters";

interface FileDetailProps {
  resource?: FileResource;
  onDownload: (path: string) => void;
  onEdit?: (path: string) => void;
  directorySize?: number | null;
  fileCount?: number | null;
  folderCount?: number | null;
  isLoadingDirectorySize?: boolean;
  statData?: ResourceStatData | null;
  isLoadingStat?: boolean;
}

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
  onDownload,
  onEdit,
  directorySize,
  fileCount,
  folderCount,
  isLoadingDirectorySize,
  statData,
  isLoadingStat,
}) => {
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
  // Show edit button for all non-directory files (backend determines if truly editable)
  const isEditableFile = !isDirectory;

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

      {/* Permissions and Ownership Section */}
      {statData && (
        <>
          <Divider />
          <Typography variant="subtitle2" fontWeight={600}>
            Permissions & Ownership
          </Typography>
          <Stack spacing={1.5}>
            <DetailRow label="Mode" value={statData.mode} />
            <DetailRow label="Owner" value={statData.owner} />
            <DetailRow label="Group" value={statData.group} />
            <DetailRow label="Permissions" value={statData.permissions} />
          </Stack>
        </>
      )}
      {isLoadingStat && (
        <>
          <Divider />
          <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
            <CircularProgress size={16} />
            <Typography variant="body2">Loading permissions...</Typography>
          </Box>
        </>
      )}

      {/* Download and Edit buttons - only for files */}
      {!isDirectory && (
        <>
          <Divider />
          <Stack direction="row" spacing={1}>
            <Button
              variant="contained"
              startIcon={<DownloadIcon />}
              onClick={() => onDownload(resource.path)}
            >
              Download
            </Button>
            {isEditableFile && onEdit && (
              <Button
                variant="outlined"
                startIcon={<EditIcon />}
                onClick={() => onEdit(resource.path)}
              >
                Edit
              </Button>
            )}
          </Stack>
        </>
      )}
    </Paper>
  );
};

export default FileDetail;
