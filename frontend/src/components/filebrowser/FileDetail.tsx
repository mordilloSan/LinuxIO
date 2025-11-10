import DownloadIcon from "@mui/icons-material/Download";
import FolderIcon from "@mui/icons-material/Folder";
import InsertDriveFileIcon from "@mui/icons-material/InsertDriveFile";
import LinkIcon from "@mui/icons-material/Link";
import VisibilityOffIcon from "@mui/icons-material/VisibilityOff";
import {
  Box,
  Button,
  Divider,
  Paper,
  Stack,
  Typography,
  alpha,
} from "@mui/material";
import React from "react";

import { FileResource } from "../../types/filebrowser";

interface FileDetailProps {
  resource: FileResource;
  onDownload: (path: string) => void;
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

const FileDetail: React.FC<FileDetailProps> = ({ resource, onDownload }) => {
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
        {!isDirectory && (
          <DetailRow label="Size" value={formatFileSize(resource.size)} />
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
