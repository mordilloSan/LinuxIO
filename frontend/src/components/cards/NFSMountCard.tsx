import { useQuery } from "@tanstack/react-query";
import { memo, type ReactNode } from "react";

import { linuxio, type NFSMount } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import Chip from "@/components/ui/AppChip";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTypography from "@/components/ui/AppTypography";
import { formatFileSize } from "@/utils/formaters";

export interface NFSMountCardProps {
  actions: (mount: NFSMount) => ReactNode;
  mountpoint: string;
}

const selectNFSMount = (mountpoint: string) => (mounts: NFSMount[]) =>
  mounts.find((mount) => mount.mountpoint === mountpoint);

const NFSMountTitle = memo(function NFSMountTitle({
  mountpoint,
}: {
  mountpoint: string;
}) {
  return (
    <AppTypography
      fontWeight={700}
      noWrap
      style={{
        fontFamily: "var(--app-font-mono)",
        flex: 1,
        minWidth: 0,
        lineHeight: 1.25,
      }}
      variant="body1"
    >
      {mountpoint}
    </AppTypography>
  );
});

const NFSMountSource = memo(function NFSMountSource({
  source,
}: {
  source: string;
}) {
  return (
    <AppTypography
      color="text.secondary"
      noWrap
      style={{
        marginBottom: 4,
        fontFamily: "var(--app-font-mono)",
        fontSize: "0.8rem",
        lineHeight: 1.3,
      }}
      variant="body2"
    >
      {source}
    </AppTypography>
  );
});

const NFSMountCardLiveContent = ({
  mountpoint,
  actions,
}: NFSMountCardProps) => {
  const { data: mount } = useQuery(
    linuxio.storage.list_nfs_mounts.queryOptions({
      refetchOnMount: false,
      select: selectNFSMount(mountpoint),
    }),
  );

  if (!mount) return null;

  const statusLabel = mount.mounted ? "Mounted" : "Configured";
  const persistenceLabel = mount.inFstab ? "Persistent" : "Temporary";

  return (
    <>
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
          gap: 8,
        }}
      >
        <NFSMountTitle mountpoint={mount.mountpoint} />
        {actions(mount)}
      </div>

      <NFSMountSource source={mount.source} />

      {mount.mounted ? (
        <div style={{ width: "100%", marginBottom: 4 }}>
          <AppLinearProgress
            color={
              mount.usedPct > 90
                ? "error"
                : mount.usedPct > 70
                  ? "warning"
                  : "primary"
            }
            style={{ height: 6, borderRadius: 3, marginBottom: 2 }}
            value={mount.usedPct}
            variant="determinate"
          />
          <AppTypography color="text.secondary" variant="caption">
            {formatFileSize(mount.used)} / {formatFileSize(mount.size)}
          </AppTypography>
        </div>
      ) : (
        <AppTypography
          color="text.secondary"
          style={{ display: "block", marginBottom: 4 }}
          variant="caption"
        >
          Not currently mounted
        </AppTypography>
      )}

      <div
        style={{ display: "flex", flexWrap: "wrap", gap: 3, marginBottom: 4 }}
      >
        <Chip label={mount.fsType} size="small" variant="soft" />
        <Chip label={statusLabel} size="small" variant="soft" />
        <Chip label={persistenceLabel} size="small" variant="soft" />
        {mount.options?.slice(0, 2).map((option, index) => (
          <Chip
            key={`${mount.mountpoint}-${index}`}
            label={option}
            size="small"
            variant="soft"
          />
        ))}
      </div>
    </>
  );
};

const NFSMountCard = (props: NFSMountCardProps) => (
  <FrostedCard style={{ padding: 8 }}>
    <NFSMountCardLiveContent {...props} />
  </FrostedCard>
);

export default NFSMountCard;
