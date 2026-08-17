import { type DockerVolume } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppCheckbox from "@/components/ui/AppCheckbox";
import Chip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";
import { CARD_PADDING_SM } from "@/theme/constants";
import { longTextStyles } from "@/theme/tableStyles";
import { formatFileSize } from "@/utils/formaters";

export interface VolumeCardProps {
  onSelect: (checked: boolean) => void;
  selected: boolean;
  volume: DockerVolume;
}

const DOCKER_TOAST_META = { label: "Open Docker", to: "/docker" } as const;

const formatVolumeSize = (size?: number) => {
  if (size === undefined || size < 0) return "Size unavailable";
  return `Size: ${formatFileSize(size)}`;
};

const formatReferenceCount = (count?: number) => {
  if (count === undefined || count < 0) return "References unavailable";
  return `References: ${count}`;
};

const VolumeCard = ({ volume, selected, onSelect }: VolumeCardProps) => (
  <FrostedCard
    accent
    hoverLift
    style={{
      padding: CARD_PADDING_SM,
      /*
        Selection takes the accent line to full strength and holds the lift
        shadow, the same treatment DockerImageCard gives its selected state —
        so a checkbox-driven multi-select list reads as selected the same way
        a click-to-toggle one does.
      */
      ...(selected && {
        borderBottomColor: "var(--fc-accent)",
        boxShadow: "var(--fc-lift-shadow)",
      }),
    }}
  >
    {/* Header: checkbox + name + driver chip */}
    <div
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        gap: 8,
        marginBottom: 8,
      }}
    >
      <div
        style={{ display: "flex", alignItems: "center", gap: 8, minWidth: 0 }}
      >
        <AppCheckbox
          checked={selected}
          onChange={(e) => onSelect(e.target.checked)}
          size="small"
        />
        <AppTypography
          fontWeight={700}
          noWrap
          title={volume.Name}
          toastMeta={DOCKER_TOAST_META}
          variant="body2"
        >
          {volume.Name}
        </AppTypography>
      </div>
      <Chip
        label={volume.Driver}
        size="small"
        style={{ fontSize: "0.75rem" }}
        variant="soft"
      />
    </div>

    {/* Mountpoint */}
    <AppTypography
      style={{
        marginBottom: 4,
        fontFamily: "var(--app-font-mono)",
        fontSize: "0.8rem",
        ...longTextStyles,
      }}
      variant="body2"
    >
      {volume.Mountpoint || "-"}
    </AppTypography>

    {/* Meta chips */}
    <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
      <Chip
        label={`Scope: ${volume.Scope || "local"}`}
        size="small"
        variant="soft"
      />
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
  </FrostedCard>
);

export default VolumeCard;
