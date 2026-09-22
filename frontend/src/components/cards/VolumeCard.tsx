import type { ReactNode } from "react";

import { type DockerVolume } from "@/api";
import DockerResourceCard from "@/components/cards/DockerResourceCard";
import AppTypography from "@/components/ui/AppTypography";
import { GAP_MD, GAP_SM } from "@/theme/constants";
import { formatFileSize } from "@/utils/formaters";

export interface VolumeCardProps {
  actions?: ReactNode;
  onOpen?: () => void;
  selected?: boolean;
  volume: DockerVolume;
}

const formatVolumeSize = (size?: number) => {
  if (size === undefined || size < 0) return "Unavailable";
  return formatFileSize(size);
};

const formatReferenceCount = (count?: number) => {
  if (count === undefined || count < 0) return "Unavailable";
  return count.toLocaleString();
};

const VolumeCard = ({ actions, volume, selected, onOpen }: VolumeCardProps) => (
  <DockerResourceCard
    icon="mdi:database-outline"
    headingVariant="section"
    actions={actions}
    label={`volume ${volume.Name}`}
    onOpen={onOpen}
    selected={selected}
    subtitle={`Driver: ${volume.Driver} · Scope: ${volume.Scope || "local"}`}
    title={volume.Name}
  >
    <dl
      style={{
        display: "grid",
        gridTemplateColumns: "1fr 1fr",
        gap: GAP_MD,
        margin: 0,
      }}
    >
      <div>
        <AppTypography component="dt" color="text.secondary" variant="body2">
          Size
        </AppTypography>
        <AppTypography
          component="dd"
          fontWeight={500}
          style={{ fontVariantNumeric: "tabular-nums" }}
          variant="body1"
        >
          {formatVolumeSize(volume.UsageData?.Size)}
        </AppTypography>
      </div>
      <div>
        <AppTypography component="dt" color="text.secondary" variant="body2">
          References
        </AppTypography>
        <AppTypography
          component="dd"
          fontWeight={500}
          style={{ fontVariantNumeric: "tabular-nums" }}
          variant="body1"
        >
          {formatReferenceCount(volume.UsageData?.RefCount)}
        </AppTypography>
      </div>
      <div style={{ gridColumn: "1 / -1", minWidth: 0 }}>
        <AppTypography component="dt" color="text.secondary" variant="body2">
          Local path
        </AppTypography>
        <AppTypography
          component="dd"
          style={{ overflowWrap: "anywhere" }}
          variant="body2"
        >
          {volume.Mountpoint || "Unavailable"}
        </AppTypography>
      </div>
    </dl>
    {volume.CreatedAt && (
      <AppTypography
        color="text.secondary"
        style={{ marginTop: GAP_SM }}
        variant="body2"
      >
        Created {new Date(volume.CreatedAt).toLocaleDateString()}
      </AppTypography>
    )}
  </DockerResourceCard>
);

export default VolumeCard;
