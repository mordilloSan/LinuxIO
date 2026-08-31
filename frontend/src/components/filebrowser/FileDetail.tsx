import { Icon } from "@iconify/react";
import type { ReactNode } from "react";

import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import AppButton from "@/components/ui/AppButton";
import AppChip from "@/components/ui/AppChip";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppPaper from "@/components/ui/AppPaper";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { useFileDirectorySize } from "@/hooks/filebrowser/useFileDirectorySize";
import { GAP_LG, GAP_MD } from "@/theme/constants";
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

/** Headline number tile — the settings/VM stat card, for the few facts worth reading first. */
const MetricTile = ({
  detail,
  icon,
  label,
  value,
}: {
  detail?: ReactNode;
  icon: string;
  label: string;
  value: ReactNode;
}) => (
  <FrostedCard
    hoverLift
    style={{
      display: "flex",
      flexDirection: "column",
      gap: "var(--app-space-4)",
      minWidth: 0,
      padding: 12,
    }}
  >
    <div
      style={{
        alignItems: "center",
        color: "var(--app-palette-primary-main)",
        display: "flex",
        gap: "var(--app-space-4)",
      }}
    >
      <Icon height={18} icon={icon} width={18} />
      <AppTypography color="text.secondary" variant="caption">
        {label}
      </AppTypography>
    </div>
    <AppTypography
      component="div"
      fontWeight={700}
      style={{ lineHeight: 1.2, overflowWrap: "anywhere" }}
      variant="h4"
    >
      {value}
    </AppTypography>
    {detail ? (
      <AppTypography color="text.secondary" variant="caption">
        {detail}
      </AppTypography>
    ) : null}
  </FrostedCard>
);

/** Settings-style card: tinted icon, heading, subtitle, then its rows. */
const DetailSection = ({
  children,
  icon,
  subtitle,
  title,
}: {
  children: ReactNode;
  icon: string;
  subtitle: string;
  title: string;
}) => (
  <section aria-label={title}>
    <FrostedCard style={{ padding: 12 }}>
      <CardIconHeader
        headingVariant="section"
        icon={<Icon height={20} icon={icon} width={20} />}
        iconTint
        subtitle={subtitle}
        title={title}
      />
      <div style={{ marginTop: GAP_MD }}>{children}</div>
    </FrostedCard>
  </section>
);

const DetailRow = ({
  label,
  mono = false,
  value,
  wide = false,
}: {
  label: string;
  mono?: boolean;
  value: ReactNode;
  wide?: boolean;
}) => (
  <div
    style={{
      display: "flex",
      flexDirection: "column",
      gap: "var(--app-space-4)",
      gridColumn: wide ? "1 / -1" : undefined,
      minWidth: 0,
    }}
  >
    <AppTypography color="text.secondary" variant="caption">
      {label}
    </AppTypography>
    <AppTypography
      component="div"
      fontWeight={500}
      style={{
        ...(mono ? { fontFamily: "var(--app-font-mono)" } : {}),
        minWidth: 0,
        overflowWrap: "anywhere",
      }}
      variant="body2"
    >
      {value}
    </AppTypography>
  </div>
);

const FileDetail = ({
  resource,
  onDownload,
  onEdit,
  statData,
  isLoadingStat,
}: FileDetailProps) => {
  // Fetch directory details only for directories
  const isDirectory = resource?.type === "directory";

  const {
    fileCount,
    folderCount,
    size,
    error: directorySizeError,
    isUnavailable: isDirectorySizeUnavailable,
    isLoading: isLoadingDirectoryDetails,
  } = useFileDirectorySize(
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
  // toLocaleString puts the clock time after the date, so the tile reads as a
  // date with the time under it; a locale without that split keeps one line.
  const [modifiedDate, ...modifiedTime] = formatDate(
    resource.modified || resource.modTime,
  ).split(", ");
  const spinner = <AppCircularProgress size={20} />;
  const directoryCount = (count?: number | null) =>
    isLoadingDirectoryDetails ? spinner : (count?.toLocaleString() ?? "—");
  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: GAP_LG,
      }}
    >
      <CardIconHeader
        icon={getTypeIcon()}
        iconTint
        right={
          <div
            style={{
              display: "flex",
              flexWrap: "wrap",
              gap: "var(--app-space-6)",
              justifyContent: "flex-end",
            }}
          >
            <AppChip label={getTypeLabel()} size="small" variant="soft" />
            {resource.hidden && (
              <AppChip label="Hidden" size="small" variant="soft" />
            )}
          </div>
        }
        subtitle={resource.path}
        title={resource.name}
      />

      <div
        style={{
          display: "grid",
          gap: GAP_MD,
          gridTemplateColumns: "repeat(auto-fit, minmax(150px, 1fr))",
        }}
      >
        <MetricTile
          detail={
            isDirectory && isLoadingDirectoryDetails
              ? "Indexing this directory"
              : isDirectory && isDirectorySizeUnavailable
                ? (directorySizeError?.message ??
                  "Cached size is unavailable while the indexer is offline")
                : undefined
          }
          icon="mdi:harddisk"
          label={isDirectory ? "Total size" : "Size"}
          value={
            !isDirectory
              ? formatFileSize(resource.size)
              : isLoadingDirectoryDetails
                ? spinner
                : isDirectorySizeUnavailable
                  ? "Unavailable"
                  : formatFileSize(size, 2, "—")
          }
        />
        {isDirectory && (
          <>
            <MetricTile
              icon="mdi:file-outline"
              label="Files"
              value={directoryCount(fileCount)}
            />
            <MetricTile
              icon="mdi:folder-outline"
              label="Folders"
              value={directoryCount(folderCount)}
            />
          </>
        )}
        <MetricTile
          detail={modifiedTime.join(", ") || undefined}
          icon="mdi:clock-outline"
          label="Modified"
          value={modifiedDate}
        />
      </div>

      {(statData || isLoadingStat) && (
        <DetailSection
          icon="mdi:shield-lock-outline"
          subtitle="Who may read, write and run this item."
          title="Permissions & ownership"
        >
          {statData ? (
            <div
              style={{
                display: "grid",
                gap: GAP_MD,
                gridTemplateColumns: "repeat(auto-fit, minmax(140px, 1fr))",
              }}
            >
              <DetailRow label="Mode" mono value={statData.mode} />
              <DetailRow label="Owner" value={statData.owner} />
              <DetailRow label="Group" value={statData.group} />
              <DetailRow
                label="Permissions"
                value={statData.permissions}
                wide
              />
            </div>
          ) : (
            <div
              style={{
                alignItems: "center",
                display: "flex",
                gap: "var(--app-space-4)",
              }}
            >
              {spinner}
              <AppTypography color="text.secondary" variant="body2">
                Loading permissions...
              </AppTypography>
            </div>
          )}
        </DetailSection>
      )}

      {/* Download and Edit buttons - only for files */}
      {!isDirectory && (
        <div
          style={{
            display: "flex",
            gap: "var(--app-space-4)",
            flexWrap: "wrap",
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
      )}
    </div>
  );
};
export default FileDetail;
