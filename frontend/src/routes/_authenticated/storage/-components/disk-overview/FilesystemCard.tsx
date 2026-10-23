import { useId } from "react";

import { type FilesystemInfo, type NFSMount } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import AppCollapse from "@/components/ui/AppCollapse";
import AppDivider from "@/components/ui/AppDivider";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppRouterLinkButton from "@/components/ui/AppRouterLinkButton";
import AppTypography from "@/components/ui/AppTypography";
import { getUsageColor } from "@/constants/statusColors";
import { CARD_PADDING_SM, GAP_MD } from "@/theme/constants";
import { formatFileSize } from "@/utils/formatters";

const SYSTEM_MOUNTPOINTS = new Set(["/", "/boot", "/boot/efi"]);

const canUnmountFilesystem = (filesystem: FilesystemInfo): boolean =>
  !SYSTEM_MOUNTPOINTS.has(filesystem.mountpoint);

interface BackingDrive {
  model?: string;
  name: string;
}

interface FilesystemCardDetailsProps {
  backingDrive: BackingDrive | null;
  filesystem: FilesystemInfo;
  isUnmounting: boolean;
  nfsMount: NFSMount | null;
  onBrowse: (mountpoint: string) => void;
  onInspectDrive: (driveName: string) => void;
  onUnmount: (mountpoint: string) => void;
}

const FilesystemCardDetails = ({
  filesystem,
  backingDrive,
  nfsMount,
  isUnmounting,
  onBrowse,
  onInspectDrive,
  onUnmount,
}: FilesystemCardDetailsProps) => {
  const isSystemMount = SYSTEM_MOUNTPOINTS.has(filesystem.mountpoint);
  const isNfs = filesystem.fstype === "nfs" || filesystem.fstype === "nfs4";
  const isBtrfs = filesystem.fstype === "btrfs";
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
            gap: GAP_MD,
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
                : isBtrfs
                  ? "Snapshots and subvolumes of this btrfs filesystem are managed on the Snapshots tab; its disks, health and settings in its pool's details."
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
          {isBtrfs && (
            <AppRouterLinkButton to="/storage/snapshots" variant="outlined">
              Manage snapshots
            </AppRouterLinkButton>
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
      </div>
    </AppCollapse>
  );
};

interface FilesystemCardProps {
  backingDrive: BackingDrive | null;
  filesystem: FilesystemInfo;
  isUnmounting: boolean;
  nfsMount: NFSMount | null;
  onBrowse: (mountpoint: string) => void;
  onClick: () => void;
  onInspectDrive: (driveName: string) => void;
  onUnmount: (mountpoint: string) => void;
  selected: boolean;
}

const FilesystemCard = ({
  filesystem,
  selected,
  backingDrive,
  nfsMount,
  isUnmounting,
  onClick,
  onBrowse,
  onInspectDrive,
  onUnmount,
}: FilesystemCardProps) => {
  const detailsId = useId();

  return (
    <FrostedCard
      hoverLift={!selected}
      style={{
        padding: CARD_PADDING_SM,
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
          color={getUsageColor(filesystem.usedPercent)}
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
            isUnmounting={isUnmounting}
            nfsMount={nfsMount}
            onBrowse={onBrowse}
            onInspectDrive={onInspectDrive}
            onUnmount={onUnmount}
          />
        )}
      </div>
    </FrostedCard>
  );
};

export default FilesystemCard;
