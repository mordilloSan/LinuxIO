import React from "react";

import { type NFSMount } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import Chip from "@/components/ui/AppChip";
import AppButton from "@/components/ui/AppButton";
import AppCollapse from "@/components/ui/AppCollapse";
import AppDivider from "@/components/ui/AppDivider";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import { FilesystemInfo } from "@/types/fs";
import { formatFileSize } from "@/utils/formaters";

const SYSTEM_MOUNTPOINTS = new Set(["/", "/boot", "/boot/efi"]);

const canUnmountFilesystem = (filesystem: FilesystemInfo): boolean =>
  !SYSTEM_MOUNTPOINTS.has(filesystem.mountpoint);

const canCreateSubvolume = (filesystem: FilesystemInfo): boolean =>
  filesystem.fstype === "btrfs" && !filesystem.readOnly;

interface BackingDrive {
  name: string;
  model?: string;
}

interface FilesystemCardDetailsProps {
  filesystem: FilesystemInfo;
  backingDrive: BackingDrive | null;
  nfsMount: NFSMount | null;
  isUnmounting: boolean;
  isCreatingSubvolume: boolean;
  subvolumeName: string;
  onBrowse: (mountpoint: string) => void;
  onInspectDrive: (driveName: string) => void;
  onUnmount: (mountpoint: string) => void;
  onSubvolumeNameChange: (mountpoint: string, value: string) => void;
  onCreateSubvolume: (mountpoint: string) => void;
}

const FilesystemCardDetails: React.FC<FilesystemCardDetailsProps> = ({
  filesystem,
  backingDrive,
  nfsMount,
  isUnmounting,
  isCreatingSubvolume,
  subvolumeName,
  onBrowse,
  onInspectDrive,
  onUnmount,
  onSubvolumeNameChange,
  onCreateSubvolume,
}) => {
  const isSystemMount = SYSTEM_MOUNTPOINTS.has(filesystem.mountpoint);
  const isNfs = filesystem.fstype === "nfs" || filesystem.fstype === "nfs4";
  return (
    <AppCollapse in={true} timeout="auto" unmountOnExit>
      <div onClick={(e: React.MouseEvent) => e.stopPropagation()}>
        <AppDivider style={{ margin: "16px 0" }} />

        <div
          style={{
            display: "flex",
            flexWrap: "wrap",
            gap: 8,
            marginBottom: 16,
          }}
        >
          <Chip
            label={filesystem.fstype.toUpperCase()}
            size="small"
            variant="soft"
          />
          {filesystem.readOnly && (
            <Chip
              label="Read-only"
              size="small"
              color="warning"
              variant="soft"
            />
          )}
          {nfsMount && (
            <Chip label="NFS mount" size="small" color="info" variant="soft" />
          )}
          {isSystemMount && (
            <Chip
              label="System mount"
              size="small"
              color="default"
              variant="soft"
            />
          )}
        </div>

        <div
          style={{
            display: "grid",
            gap: 12,
            marginBottom: 16,
          }}
        >
          <div>
            <AppTypography variant="body2" color="text.secondary">
              Source
            </AppTypography>
            <AppTypography variant="body1">
              {nfsMount?.source || filesystem.device || "Unknown"}
            </AppTypography>
          </div>

          <div>
            <AppTypography variant="body2" color="text.secondary">
              Usage
            </AppTypography>
            <AppTypography variant="body1">
              {formatFileSize(filesystem.used)} used of{" "}
              {formatFileSize(filesystem.total)} (
              {filesystem.usedPercent.toFixed(1)}
              %)
            </AppTypography>
            <AppTypography variant="body2" color="text.secondary">
              {formatFileSize(filesystem.free)} free
            </AppTypography>
          </div>

          {typeof filesystem.inodesUsedPercent === "number" &&
            filesystem.inodesTotal &&
            filesystem.inodesTotal > 0 && (
              <div>
                <AppTypography variant="body2" color="text.secondary">
                  Inodes
                </AppTypography>
                <AppTypography variant="body1">
                  {(filesystem.inodesUsed ?? 0).toLocaleString()} used of{" "}
                  {filesystem.inodesTotal.toLocaleString()} (
                  {filesystem.inodesUsedPercent.toFixed(1)}%)
                </AppTypography>
              </div>
            )}

          {nfsMount && (
            <>
              <div>
                <AppTypography variant="body2" color="text.secondary">
                  Export
                </AppTypography>
                <AppTypography variant="body1">
                  {nfsMount.server}:{nfsMount.exportPath}
                </AppTypography>
              </div>

              <div>
                <AppTypography variant="body2" color="text.secondary">
                  Mount options
                </AppTypography>
                <AppTypography variant="body1">
                  {nfsMount.options.length > 0
                    ? nfsMount.options.join(", ")
                    : "Default options"}
                </AppTypography>
                <AppTypography variant="body2" color="text.secondary">
                  {nfsMount.inFstab
                    ? "Configured to mount at boot"
                    : "Not persisted in /etc/fstab"}
                </AppTypography>
              </div>
            </>
          )}

          <AppTypography variant="body2" color="text.secondary">
            {nfsMount
              ? "This filesystem supports direct unmount here because NFS management already exists in the backend."
              : isNfs
                ? "This looks like an NFS filesystem, but no matching NFS mount entry was loaded for direct actions."
                : canCreateSubvolume(filesystem)
                  ? "This btrfs filesystem can create subvolumes directly from the card."
                  : canUnmountFilesystem(filesystem)
                    ? "This filesystem can be unmounted directly from the card."
                    : "Protected system mounts stay visible here but do not expose unmount actions."}
          </AppTypography>

          {backingDrive && (
            <AppTypography variant="body2" color="text.secondary">
              Backing drive: /dev/{backingDrive.name}
              {backingDrive.model ? ` (${backingDrive.model})` : ""}
            </AppTypography>
          )}
        </div>

        <div
          style={{
            display: "flex",
            flexWrap: "wrap",
            gap: 8,
            alignItems: "center",
          }}
        >
          <AppButton
            variant="outlined"
            onClick={() => onBrowse(filesystem.mountpoint)}
          >
            Browse
          </AppButton>
          {backingDrive && (
            <AppButton
              variant="outlined"
              onClick={() => onInspectDrive(backingDrive.name)}
            >
              Inspect Drive
            </AppButton>
          )}
          {canUnmountFilesystem(filesystem) && (
            <AppButton
              color="error"
              variant="outlined"
              onClick={() => onUnmount(filesystem.mountpoint)}
              disabled={isUnmounting}
            >
              {isUnmounting ? "Unmounting..." : "Unmount"}
            </AppButton>
          )}
        </div>

        {canCreateSubvolume(filesystem) && (
          <div
            style={{
              display: "flex",
              flexWrap: "wrap",
              gap: 8,
              alignItems: "center",
              marginTop: 12,
            }}
          >
            <AppTextField
              size="small"
              label="Subvolume name"
              value={subvolumeName}
              onChange={(event) =>
                onSubvolumeNameChange(filesystem.mountpoint, event.target.value)
              }
              placeholder="@data"
              style={{ minWidth: 220, flex: "1 1 220px" }}
              onClick={(event) => event.stopPropagation()}
            />
            <AppButton
              variant="outlined"
              onClick={() => onCreateSubvolume(filesystem.mountpoint)}
              disabled={
                isCreatingSubvolume || subvolumeName.trim().length === 0
              }
            >
              {isCreatingSubvolume ? "Creating..." : "Create subvolume"}
            </AppButton>
          </div>
        )}
      </div>
    </AppCollapse>
  );
};

export interface FilesystemCardProps {
  filesystem: FilesystemInfo;
  selected: boolean;
  backingDrive: BackingDrive | null;
  nfsMount: NFSMount | null;
  isUnmounting: boolean;
  isCreatingSubvolume: boolean;
  subvolumeName: string;
  onClick: () => void;
  onBrowse: (mountpoint: string) => void;
  onInspectDrive: (driveName: string) => void;
  onUnmount: (mountpoint: string) => void;
  onSubvolumeNameChange: (mountpoint: string, value: string) => void;
  onCreateSubvolume: (mountpoint: string) => void;
}

const FilesystemCard: React.FC<FilesystemCardProps> = ({
  filesystem,
  selected,
  backingDrive,
  nfsMount,
  isUnmounting,
  isCreatingSubvolume,
  subvolumeName,
  onClick,
  onBrowse,
  onInspectDrive,
  onUnmount,
  onSubvolumeNameChange,
  onCreateSubvolume,
}) => (
  <FrostedCard
    hoverLift={!selected}
    style={{
      padding: 8,
      cursor: "pointer",
    }}
    onClick={onClick}
    onKeyDown={(event) => {
      if (event.key === "Enter" || event.key === " ") {
        event.preventDefault();
        onClick();
      }
    }}
    role="button"
    tabIndex={0}
    aria-label={`Toggle details for ${filesystem.mountpoint}`}
  >
    <AppTypography
      variant="subtitle2"
      fontWeight={600}
      noWrap
      title={filesystem.mountpoint}
    >
      {filesystem.mountpoint}
    </AppTypography>
    <AppTypography
      variant="body2"
      color="text.secondary"
      noWrap
      title={`${filesystem.device} (${filesystem.fstype})`}
      style={{ marginBottom: 6 }}
    >
      {filesystem.device} ({filesystem.fstype})
    </AppTypography>
    <AppLinearProgress
      variant="determinate"
      value={filesystem.usedPercent}
      style={{
        height: 8,
        borderRadius: 4,
        marginBottom: 8,
      }}
      color={
        filesystem.usedPercent > 90
          ? "error"
          : filesystem.usedPercent > 70
            ? "warning"
            : "primary"
      }
    />
    <AppTypography variant="body2" color="text.secondary">
      {formatFileSize(filesystem.used)} / {formatFileSize(filesystem.total)}{" "}
      ({filesystem.usedPercent.toFixed(1)}%)
    </AppTypography>
    {selected && (
      <FilesystemCardDetails
        filesystem={filesystem}
        backingDrive={backingDrive}
        nfsMount={nfsMount}
        isUnmounting={isUnmounting}
        isCreatingSubvolume={isCreatingSubvolume}
        subvolumeName={subvolumeName}
        onBrowse={onBrowse}
        onInspectDrive={onInspectDrive}
        onUnmount={onUnmount}
        onSubvolumeNameChange={onSubvolumeNameChange}
        onCreateSubvolume={onCreateSubvolume}
      />
    )}
  </FrostedCard>
);

export default FilesystemCard;
