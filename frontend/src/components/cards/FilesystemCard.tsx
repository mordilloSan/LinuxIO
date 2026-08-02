import { useId } from "react";

import { type FilesystemInfo, type NFSMount } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import AppCollapse from "@/components/ui/AppCollapse";
import AppDivider from "@/components/ui/AppDivider";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import { formatFileSize } from "@/utils/formaters";

const SYSTEM_MOUNTPOINTS = new Set(["/", "/boot", "/boot/efi"]);

const canUnmountFilesystem = (filesystem: FilesystemInfo): boolean =>
  !SYSTEM_MOUNTPOINTS.has(filesystem.mountpoint);

const canCreateSubvolume = (filesystem: FilesystemInfo): boolean =>
  filesystem.fstype === "btrfs" && !filesystem.readOnly;

interface BackingDrive {
  model?: string;
  name: string;
}

interface FilesystemCardDetailsProps {
  backingDrive: BackingDrive | null;
  filesystem: FilesystemInfo;
  isCreatingSubvolume: boolean;
  isUnmounting: boolean;
  nfsMount: NFSMount | null;
  onBrowse: (mountpoint: string) => void;
  onCreateSubvolume: (mountpoint: string) => void;
  onInspectDrive: (driveName: string) => void;
  onSubvolumeNameChange: (mountpoint: string, value: string) => void;
  onUnmount: (mountpoint: string) => void;
  subvolumeName: string;
}

const FilesystemCardDetails = ({
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
}: FilesystemCardDetailsProps) => {
  const isSystemMount = SYSTEM_MOUNTPOINTS.has(filesystem.mountpoint);
  const isNfs = filesystem.fstype === "nfs" || filesystem.fstype === "nfs4";
  return (
    <AppCollapse in={true} unmountOnExit>
      <div>
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
              color="warning"
              label="Read-only"
              size="small"
              variant="soft"
            />
          )}
          {nfsMount && (
            <Chip color="info" label="NFS mount" size="small" variant="soft" />
          )}
          {isSystemMount && (
            <Chip
              color="default"
              label="System mount"
              size="small"
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
            <AppTypography color="text.secondary" variant="body2">
              Source
            </AppTypography>
            <AppTypography variant="body1">
              {nfsMount?.source || filesystem.device || "Unknown"}
            </AppTypography>
          </div>

          <div>
            <AppTypography color="text.secondary" variant="body2">
              Usage
            </AppTypography>
            <AppTypography variant="body1">
              {formatFileSize(filesystem.used)} used of{" "}
              {formatFileSize(filesystem.total)} (
              {filesystem.usedPercent.toFixed(1)}
              %)
            </AppTypography>
            <AppTypography color="text.secondary" variant="body2">
              {formatFileSize(filesystem.free)} free
            </AppTypography>
          </div>

          {typeof filesystem.inodesUsedPercent === "number" &&
            filesystem.inodesTotal &&
            filesystem.inodesTotal > 0 && (
              <div>
                <AppTypography color="text.secondary" variant="body2">
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
                <AppTypography color="text.secondary" variant="body2">
                  Export
                </AppTypography>
                <AppTypography variant="body1">
                  {nfsMount.server}:{nfsMount.exportPath}
                </AppTypography>
              </div>

              <div>
                <AppTypography color="text.secondary" variant="body2">
                  Mount options
                </AppTypography>
                <AppTypography variant="body1">
                  {nfsMount.options.length > 0
                    ? nfsMount.options.join(", ")
                    : "Default options"}
                </AppTypography>
                <AppTypography color="text.secondary" variant="body2">
                  {nfsMount.inFstab
                    ? "Configured to mount at boot"
                    : "Not persisted in /etc/fstab"}
                </AppTypography>
              </div>
            </>
          )}

          <AppTypography color="text.secondary" variant="body2">
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
            <AppTypography color="text.secondary" variant="body2">
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
            onClick={() => onBrowse(filesystem.mountpoint)}
            variant="outlined"
          >
            Browse
          </AppButton>
          {backingDrive && (
            <AppButton
              onClick={() => onInspectDrive(backingDrive.name)}
              variant="outlined"
            >
              Inspect Drive
            </AppButton>
          )}
          {canUnmountFilesystem(filesystem) && (
            <AppButton
              color="error"
              disabled={isUnmounting}
              onClick={() => onUnmount(filesystem.mountpoint)}
              variant="outlined"
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
              label="Subvolume name"
              onChange={(event) =>
                onSubvolumeNameChange(filesystem.mountpoint, event.target.value)
              }
              placeholder="@data"
              size="small"
              style={{ minWidth: 220, flex: "1 1 220px" }}
              value={subvolumeName}
            />
            <AppButton
              disabled={
                isCreatingSubvolume || subvolumeName.trim().length === 0
              }
              onClick={() => onCreateSubvolume(filesystem.mountpoint)}
              variant="outlined"
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
  backingDrive: BackingDrive | null;
  filesystem: FilesystemInfo;
  isCreatingSubvolume: boolean;
  isUnmounting: boolean;
  nfsMount: NFSMount | null;
  onBrowse: (mountpoint: string) => void;
  onClick: () => void;
  onCreateSubvolume: (mountpoint: string) => void;
  onInspectDrive: (driveName: string) => void;
  onSubvolumeNameChange: (mountpoint: string, value: string) => void;
  onUnmount: (mountpoint: string) => void;
  selected: boolean;
  subvolumeName: string;
}

const FilesystemCard = ({
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
}: FilesystemCardProps) => {
  const detailsId = useId();

  return (
    <FrostedCard
      hoverLift={!selected}
      style={{
        padding: 8,
      }}
    >
      <AppButton
        aria-controls={detailsId}
        aria-expanded={selected}
        aria-label={`Toggle details for ${filesystem.mountpoint}`}
        color="inherit"
        fullWidth
        onClick={onClick}
        style={{
          alignItems: "stretch",
          background: "transparent",
          border: 0,
          display: "flex",
          flexDirection: "column",
          minWidth: 0,
          padding: 0,
          textAlign: "left",
        }}
      >
        <AppTypography
          fontWeight={600}
          noWrap
          title={filesystem.mountpoint}
          variant="subtitle2"
        >
          {filesystem.mountpoint}
        </AppTypography>
        <AppTypography
          color="text.secondary"
          noWrap
          style={{ marginBottom: 6 }}
          title={`${filesystem.device} (${filesystem.fstype})`}
          variant="body2"
        >
          {filesystem.device} ({filesystem.fstype})
        </AppTypography>
        <AppLinearProgress
          color={
            filesystem.usedPercent > 90
              ? "error"
              : filesystem.usedPercent > 70
                ? "warning"
                : "primary"
          }
          style={{
            height: 8,
            borderRadius: 4,
            marginBottom: 8,
          }}
          value={filesystem.usedPercent}
          variant="determinate"
        />
        <AppTypography color="text.secondary" variant="body2">
          {formatFileSize(filesystem.used)} / {formatFileSize(filesystem.total)}{" "}
          ({filesystem.usedPercent.toFixed(1)}%)
        </AppTypography>
      </AppButton>
      <div id={detailsId}>
        {selected && (
          <FilesystemCardDetails
            backingDrive={backingDrive}
            filesystem={filesystem}
            isCreatingSubvolume={isCreatingSubvolume}
            isUnmounting={isUnmounting}
            nfsMount={nfsMount}
            onBrowse={onBrowse}
            onCreateSubvolume={onCreateSubvolume}
            onInspectDrive={onInspectDrive}
            onSubvolumeNameChange={onSubvolumeNameChange}
            onUnmount={onUnmount}
            subvolumeName={subvolumeName}
          />
        )}
      </div>
    </FrostedCard>
  );
};

export default FilesystemCard;
