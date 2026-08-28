import {
  useQuery,
  type QueryFunction,
  type QueryKey,
} from "@tanstack/react-query";
import { memo, type ReactNode } from "react";

import FrostedCard from "@/components/cards/FrostedCard";
import Chip from "@/components/ui/AppChip";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTypography from "@/components/ui/AppTypography";
import { getUsageColor } from "@/constants/statusColors";
import { CARD_PADDING_SM } from "@/theme/constants";
import { formatFileSize } from "@/utils/formaters";

/** The fields NFSMount and CIFSMount share; everything the mount UI renders. */
export interface MountBase {
  free: number;
  fsType: string;
  inFstab: boolean;
  mounted: boolean;
  mountpoint: string;
  options: string[];
  size: number;
  source: string;
  used: number;
  usedPct: number;
}

/** A generated `list_*_mounts` descriptor, spread into useQuery. */
export interface MountListQuery<T extends MountBase> {
  queryFn: QueryFunction<T[]>;
  queryKey: QueryKey;
}

export interface MountCardProps<T extends MountBase> {
  actions: (mount: T) => ReactNode;
  chips: (mount: T) => string[];
  listQueryOptions: MountListQuery<T>;
  mountpoint: string;
}

const selectMount =
  <T extends MountBase>(mountpoint: string) =>
  (mounts: T[]) =>
    mounts.find((mount) => mount.mountpoint === mountpoint);

const MountTitle = memo(function MountTitle({
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

const MountSource = memo(function MountSource({ source }: { source: string }) {
  return (
    <AppTypography
      color="text.secondary"
      noWrap
      style={{
        marginBottom: 4,
        fontFamily: "var(--app-font-mono)",
        lineHeight: 1.3,
      }}
      variant="body2"
    >
      {source}
    </AppTypography>
  );
});

// Cache-only observer: the grid's identity query owns the polling; each card
// re-renders only when its own mount changes.
const MountCardLiveContent = <T extends MountBase>({
  actions,
  chips,
  listQueryOptions,
  mountpoint,
}: MountCardProps<T>) => {
  const { data: mount } = useQuery({
    ...listQueryOptions,
    refetchOnMount: false,
    select: selectMount<T>(mountpoint),
  });

  if (!mount) return null;

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
        <MountTitle mountpoint={mount.mountpoint} />
        {actions(mount)}
      </div>

      <MountSource source={mount.source} />

      {mount.mounted ? (
        <div style={{ width: "100%", marginBottom: 4 }}>
          <AppLinearProgress
            color={getUsageColor(mount.usedPct)}
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
        {chips(mount).map((label) => (
          <Chip key={label} label={label} size="small" variant="soft" />
        ))}
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

const MountCard = <T extends MountBase>(props: MountCardProps<T>) => (
  <FrostedCard accent hoverLift style={{ padding: CARD_PADDING_SM }}>
    <MountCardLiveContent {...props} />
  </FrostedCard>
);

export default MountCard;
