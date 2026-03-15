import { Icon } from "@iconify/react";
import {
  Button,
  CircularProgress,
  Divider,
  Paper,
  Typography,
  useTheme,
} from "@mui/material";
import React from "react";

import { FileResource, ResourceStatData } from "../../types/filebrowser";

import { isEditableFile } from "@/components/filebrowser/utils";
import { useFileSubfolders } from "@/hooks/useFileSubfolders";
import { formatDate, formatFileSize } from "@/utils/formaters";

interface FileDetailProps {
  resource?: FileResource;
  onDownload: (path: string) => void;
  onEdit?: (path: string) => void;
  statData?: ResourceStatData | null;
  isLoadingStat?: boolean;
}

const DetailRow: React.FC<{ label: string; value: React.ReactNode }> = ({
  label,
  value,
}) => {
  const theme = useTheme();

  return (
    <div
      style={{
        display: "flex",
        gap: theme.spacing(2),
      }}
    >
      <Typography
        variant="body2"
        fontWeight={600}
        color="text.secondary"
        sx={{ minWidth: 100 }}
      >
        {label}:
      </Typography>
      <Typography
        component="div"
        variant="body2"
        sx={{ flex: 1, wordBreak: "break-all" }}
      >
        {value}
      </Typography>
    </div>
  );
};

const FileDetail: React.FC<FileDetailProps> = ({
  resource,
  onDownload,
  onEdit,
  statData,
  isLoadingStat,
}) => {
  const theme = useTheme();
  // Fetch directory details only for directories
  const isDirectory = resource?.type === "directory";

  // Calculate parent path to fetch subfolders
  const parentPath = resource?.path
    ? resource.path.substring(0, resource.path.lastIndexOf("/")) || "/"
    : "/";

  // Fetch subfolders of the parent directory
  const { subfoldersMap, isLoading: isLoadingDirectoryDetails } =
    useFileSubfolders(parentPath, isDirectory && !!resource?.path);

  // Look up this directory's size from the parent's subfolders
  const size =
    isDirectory && resource?.path
      ? (subfoldersMap.get(resource.path)?.size ?? null)
      : null;

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

  const isSymlink = resource.symlink;
  // Show edit button only for text-based files that can be edited
  const canEdit = !isDirectory && isEditableFile(resource.name);

  const getTypeIcon = () => {
    if (isSymlink) return <Icon icon="mdi:link" width={28} height={28} />;
    if (isDirectory) return <Icon icon="mdi:folder" width={28} height={28} />;
    return <Icon icon="mdi:file" width={28} height={28} />;
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
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: theme.spacing(2),
        }}
      >
        <div style={{ color: theme.palette.primary.main, display: "flex" }}>
          {getTypeIcon()}
        </div>
        <div style={{ flex: 1 }}>
          <Typography variant="h6" fontWeight={600}>
            {resource.name}
          </Typography>
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: theme.spacing(1),
              marginTop: theme.spacing(0.5),
            }}
          >
            <Typography variant="body2" color="text.secondary">
              {getTypeLabel()}
            </Typography>
            {resource.hidden && (
              <>
                <Typography variant="body2" color="text.secondary">
                  •
                </Typography>
                <div
                  style={{
                    display: "flex",
                    alignItems: "center",
                    gap: theme.spacing(0.5),
                  }}
                >
                  <Icon icon="mdi:eye-off" width={16} height={16} />
                  <Typography variant="body2" color="text.secondary">
                    Hidden
                  </Typography>
                </div>
              </>
            )}
          </div>
        </div>
      </div>

      <Divider />

      {/* Details section */}
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          gap: theme.spacing(1.5),
        }}
      >
        <DetailRow label="Path" value={resource.path} />
        <DetailRow
          label="Size"
          value={
            !isDirectory ? (
              formatFileSize(resource.size)
            ) : isLoadingDirectoryDetails ? (
              <div
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: theme.spacing(1),
                }}
              >
                <CircularProgress size={16} />
                <Typography variant="body2">Calculating...</Typography>
              </div>
            ) : size !== undefined && size !== null && size !== 0 ? (
              formatFileSize(size)
            ) : (
              "—"
            )
          }
        />
        <DetailRow
          label="Modified"
          value={formatDate(resource.modified || resource.modTime)}
        />
      </div>

      {/* Permissions and Ownership Section */}
      {statData && (
        <>
          <Divider />
          <Typography variant="subtitle2" fontWeight={600}>
            Permissions & Ownership
          </Typography>
          <div
            style={{
              display: "flex",
              flexDirection: "column",
              gap: theme.spacing(1.5),
            }}
          >
            <DetailRow label="Mode" value={statData.mode} />
            <DetailRow label="Owner" value={statData.owner} />
            <DetailRow label="Group" value={statData.group} />
            <DetailRow label="Permissions" value={statData.permissions} />
          </div>
        </>
      )}
      {isLoadingStat && (
        <>
          <Divider />
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: theme.spacing(1),
            }}
          >
            <CircularProgress size={16} />
            <Typography variant="body2">Loading permissions...</Typography>
          </div>
        </>
      )}

      {/* Download and Edit buttons - only for files */}
      {!isDirectory && (
        <>
          <Divider />
          <div
            style={{
              display: "flex",
              gap: theme.spacing(1),
            }}
          >
            <Button
              variant="contained"
              startIcon={<Icon icon="mdi:download" width={20} height={20} />}
              onClick={() => onDownload(resource.path)}
            >
              Download
            </Button>
            {canEdit && onEdit && (
              <Button
                variant="outlined"
                startIcon={<Icon icon="mdi:pencil" width={20} height={20} />}
                onClick={() => onEdit(resource.path)}
              >
                Edit
              </Button>
            )}
          </div>
        </>
      )}
    </Paper>
  );
};

export default FileDetail;
