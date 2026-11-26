import type { BtrfsFilesystem } from "@/api";
import { STATUS_COLOR, StatusLine } from "@/components/cards/EntityCardKit";
import FrostedCard from "@/components/cards/FrostedCard";
import SelectableCard from "@/components/cards/SelectableCard";
import AppChip from "@/components/ui/AppChip";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTypography from "@/components/ui/AppTypography";
import { formatFileSize } from "@/utils/formatters";

import { poolHealth } from "../btrfs/BtrfsHealthBanner";
import { keptCopies } from "../btrfs/btrfsShared";
import { describePool } from "../disk-overview/storageGraph";

type Tone = keyof typeof STATUS_COLOR;

const toneOf = (color: string): Tone =>
  (Object.keys(STATUS_COLOR) as Tone[]).find(
    (tone) => STATUS_COLOR[tone] === color,
  ) ?? "muted";

/** The profile data chunks get, "" when the space could not be read. */
export const dataProfile = (fs: BtrfsFilesystem) =>
  fs.allocation.find((a) => a.kind.split("+").includes("data"))?.profile ?? "";

interface PoolSummaryCardProps {
  fs: BtrfsFilesystem;
  name: string;
  /** Opens the pool's page; absent on the page itself. */
  onOpen?: () => void;
  selected?: boolean;
}

/**
 * A pool at a glance: its health, how many disks keep its data and how, and
 * its space. The Pools tab lists these, and the pool's page keeps its own
 * beside the details.
 */
const PoolSummaryCard = ({
  fs,
  name,
  onOpen,
  selected,
}: PoolSummaryCardProps) => {
  const health = poolHealth(fs, name);
  const { space, disks } = describePool(fs, 0);
  const profile = dataProfile(fs);
  const count =
    fs.devices.length === 1 ? "1 disk" : `${fs.devices.length} disks`;
  const size = space ? space.storedBytes + space.freeBytes : 0;

  return (
    <SelectableCard label={name} onOpen={onOpen}>
      <FrostedCard
        style={{
          display: "flex",
          flexDirection: "column",
          gap: "var(--app-space-8)",
          height: "100%",
          outline: selected
            ? "2px solid var(--app-palette-primary-main)"
            : undefined,
          padding: "var(--app-space-16)",
        }}
      >
        <div style={{ minWidth: 0 }}>
          <AppTypography component="h2" fontWeight={700} noWrap variant="body1">
            {name}
          </AppTypography>
          <StatusLine text={health.title} tone={toneOf(health.color)} />
        </div>
        <AppTypography color="text.secondary" variant="body2">
          {profile ? `${count} · ${keptCopies(profile)}` : count}
        </AppTypography>
        {space ? (
          <>
            <AppLinearProgress
              aria-label={`${name} space used`}
              value={size > 0 ? (space.storedBytes / size) * 100 : 0}
              variant="determinate"
            />
            <AppTypography variant="caption">
              {`${formatFileSize(space.storedBytes)} stored · about ${formatFileSize(space.freeBytes)} free`}
            </AppTypography>
          </>
        ) : (
          <AppTypography color="text.secondary" variant="caption">
            Space unknown
          </AppTypography>
        )}
        <div
          style={{
            display: "flex",
            flexWrap: "wrap",
            gap: "var(--app-space-4)",
          }}
        >
          {disks.map((disk) => (
            <AppChip
              key={disk.key}
              label={`${disk.name} · ${formatFileSize(disk.sizeBytes)}`}
              size="small"
              variant="soft"
            />
          ))}
        </div>
      </FrostedCard>
    </SelectableCard>
  );
};

export default PoolSummaryCard;
