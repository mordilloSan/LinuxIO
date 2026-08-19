import type { ReactNode } from "react";

import { type DockerVolume } from "@/api";
import DockerResourceCard from "@/components/cards/DockerResourceCard";
import Chip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";
import { GAP_SM } from "@/theme/constants";
import { longTextStyles } from "@/theme/tableStyles";
import { formatFileSize } from "@/utils/formaters";

export interface VolumeCardProps {
  actions?: ReactNode;
  onOpen?: () => void;
  selected?: boolean;
  volume: DockerVolume;
}

const formatVolumeSize = (size?: number) => {
  if (size === undefined || size < 0) return "Size unavailable";
  return `Size: ${formatFileSize(size)}`;
};

const formatReferenceCount = (count?: number) => {
  if (count === undefined || count < 0) return "References unavailable";
  return `References: ${count}`;
};

const VolumeCard = ({ actions, volume, selected, onOpen }: VolumeCardProps) => (
  <DockerResourceCard
    icon="mdi:database-outline"
    actions={actions}
    label={`volume ${volume.Name}`}
    onOpen={onOpen}
    selected={selected}
    subtitle={`${volume.Driver} · ${volume.Scope || "local"}`}
    title={volume.Name}
  >
    <AppTypography
      color="text.secondary"
      style={{
        marginBottom: GAP_SM,
        fontFamily: "var(--app-font-mono)",
        ...longTextStyles,
      }}
      variant="body2"
    >
      {volume.Mountpoint || "-"}
    </AppTypography>

    <div style={{ display: "flex", flexWrap: "wrap", gap: GAP_SM }}>
      {volume.CreatedAt && (
        <Chip
          label={new Date(volume.CreatedAt).toLocaleDateString()}
          size="small"
          variant="soft"
        />
      )}
      {volume.UsageData && (
        <>
          <Chip
            label={formatVolumeSize(volume.UsageData.Size)}
            size="small"
            variant="soft"
          />
          <Chip
            label={formatReferenceCount(volume.UsageData.RefCount)}
            size="small"
            variant="soft"
          />
        </>
      )}
    </div>
  </DockerResourceCard>
);

export default VolumeCard;
