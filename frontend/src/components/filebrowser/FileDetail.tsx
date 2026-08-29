import { Icon } from "@iconify/react";
import type { ReactNode } from "react";

import AppButton from "@/components/ui/AppButton";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppDivider from "@/components/ui/AppDivider";
import AppPaper from "@/components/ui/AppPaper";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { useFileDirectorySize } from "@/hooks/filebrowser/useFileDirectorySize";
import { formatDate, formatFileSize } from "@/utils/formaters";

import { getTextEditBlockedReason } from "./utils";
import type { FileResource, ResourceStatData } from "../../types/filebrowser";

interface FileDetailProps {
  isLoadingStat?: boolean;
  onDownload: (path: string) => void;
  onEdit?: (path: string) => void;
  resource?: FileResource;
  statData?: ResourceStatData | null;
}
const DetailRow = ({ label, value }: { label: string; value: ReactNode }) => {
  return (
    <div
      style={{
        display: "flex",
        gap: "var(--app-space-8)",
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
const FileDetail = ({
  resource,
  onDownload,
  onEdit,
  statData,
  isLoadingStat,
}: FileDetailProps) => {
  // Fetch directory details only for directories
  const isDirectory = resource?.type === "directory";

  const { size, isLoading: isLoadingDirectoryDetails } = useFileDirectorySize(
    resource?.path ?? "",
    isDirectory && !!resource?.path,
  );
  if (!resource) {
    return (
      <AppPaper
        style={{
          borderRadius: "var(--app-radius-md)",
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
  const editBlockedReason = getTextEditBlockedReason(resource);
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
        borderRadius: "var(--app-radius-md)",
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
          gap: "var(--app-space-8)",
        }}
      >
        <div
          style={{
            color: "var(--app-palette-primary-main)",
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
              gap: "var(--app-space-4)",
              marginTop: "var(--app-space-2)",
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
                    gap: "var(--app-space-2)",
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
          gap: "var(--app-space-6)",
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
                  gap: "var(--app-space-4)",
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
              gap: "var(--app-space-6)",
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
              gap: "var(--app-space-4)",
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
              gap: "var(--app-space-4)",
            }}
          >
            <AppButton
              onClick={() => onDownload(resource.path)}
              startIcon={<Icon height={20} icon="mdi:download" width={20} />}
              variant="contained"
            >
              Download
            </AppButton>
            {onEdit && (
              <AppTooltip title={editBlockedReason ?? ""}>
                <AppButton
                  disabled={editBlockedReason !== null}
                  onClick={() => onEdit(resource.path)}
                  startIcon={<Icon height={20} icon="mdi:pencil" width={20} />}
                  variant="outlined"
                >
                  Open text file
                </AppButton>
              </AppTooltip>
            )}
          </div>
        </>
      )}
    </AppPaper>
  );
};
export default FileDetail;
