import { Icon } from "@iconify/react";
import React from "react";

import AppButton from "@/components/ui/AppButton";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppDivider from "@/components/ui/AppDivider";
import AppPaper from "@/components/ui/AppPaper";
import AppTypography from "@/components/ui/AppTypography";
import { useFileSubfolders } from "@/hooks/filebrowser/useFileSubfolders";
import { useAppTheme } from "@/theme";
import { formatDate, formatFileSize } from "@/utils/formaters";

import { FileResource, ResourceStatData } from "../../types/filebrowser";
interface FileDetailProps {
  isLoadingStat?: boolean;
  onDownload: (path: string) => void;
  onEdit?: (path: string) => void;
  resource?: FileResource;
  statData?: ResourceStatData | null;
}
const DetailRow: React.FC<{
  label: string;
  value: React.ReactNode;
}> = ({ label, value }) => {
  const theme = useAppTheme();
  return (
    <div
      style={{
        display: "flex",
        gap: theme.spacing(2),
      }}
    >
      <AppTypography
        color="text.secondary"
        fontWeight={600}
        style={{
          minWidth: 100,
        }}
        variant="body2"
      >
        {label}:
      </AppTypography>
      <AppTypography
        component="div"
        style={{
          flex: 1,
          wordBreak: "break-all",
        }}
        variant="body2"
      >
        {value}
      </AppTypography>
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
  const theme = useAppTheme();
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
      <AppPaper
        style={{
          borderRadius: 8,
          padding: 12,
        }}
        variant="outlined"
      >
        <AppTypography color="text.secondary" variant="body2">
          Select an item to view its details.
        </AppTypography>
      </AppPaper>
    );
  }
  const isSymlink = resource.symlink;
  // Show edit button for any non-directory; the parent handler asks for
  // confirmation when the file isn't in the editable allowlist.
  const canEdit = !isDirectory;
  const getTypeIcon = () => {
    if (isSymlink) return <Icon height={28} icon="mdi:link" width={28} />;
    if (isDirectory) return <Icon height={28} icon="mdi:folder" width={28} />;
    return <Icon height={28} icon="mdi:file" width={28} />;
  };
  const getTypeLabel = () => {
    if (isSymlink) return "Symbolic Link";
    if (isDirectory) return "Directory";
    return "File";
  };
  return (
    <AppPaper
      style={{
        borderRadius: 8,
        display: "flex",
        flexDirection: "column",
        padding: 12,
        gap: 8,
      }}
      variant="outlined"
    >
      {/* Header with icon and name */}
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: theme.spacing(2),
        }}
      >
        <div
          style={{
            color: theme.palette.primary.main,
            display: "flex",
          }}
        >
          {getTypeIcon()}
        </div>
        <div
          style={{
            flex: 1,
          }}
        >
          <AppTypography fontWeight={600} variant="h6">
            {resource.name}
          </AppTypography>
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: theme.spacing(1),
              marginTop: theme.spacing(0.5),
            }}
          >
            <AppTypography color="text.secondary" variant="body2">
              {getTypeLabel()}
            </AppTypography>
            {resource.hidden && (
              <>
                <AppTypography color="text.secondary" variant="body2">
                  •
                </AppTypography>
                <div
                  style={{
                    display: "flex",
                    alignItems: "center",
                    gap: theme.spacing(0.5),
                  }}
                >
                  <Icon height={16} icon="mdi:eye-off" width={16} />
                  <AppTypography color="text.secondary" variant="body2">
                    Hidden
                  </AppTypography>
                </div>
              </>
            )}
          </div>
        </div>
      </div>

      <AppDivider />

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
                <AppCircularProgress size={16} />
                <AppTypography variant="body2">Calculating...</AppTypography>
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
          <AppDivider />
          <AppTypography fontWeight={600} variant="subtitle2">
            Permissions & Ownership
          </AppTypography>
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
          <AppDivider />
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: theme.spacing(1),
            }}
          >
            <AppCircularProgress size={16} />
            <AppTypography variant="body2">
              Loading permissions...
            </AppTypography>
          </div>
        </>
      )}

      {/* Download and Edit buttons - only for files */}
      {!isDirectory && (
        <>
          <AppDivider />
          <div
            style={{
              display: "flex",
              gap: theme.spacing(1),
            }}
          >
            <AppButton
              onClick={() => onDownload(resource.path)}
              startIcon={<Icon height={20} icon="mdi:download" width={20} />}
              variant="contained"
            >
              Download
            </AppButton>
            {canEdit && onEdit && (
              <AppButton
                onClick={() => onEdit(resource.path)}
                startIcon={<Icon height={20} icon="mdi:pencil" width={20} />}
                variant="outlined"
              >
                Edit
              </AppButton>
            )}
          </div>
        </>
      )}
    </AppPaper>
  );
};
export default FileDetail;
