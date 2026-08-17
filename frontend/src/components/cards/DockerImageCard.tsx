import type { DockerUpdateCheckState } from "@/api";
import DockerResourceCard from "@/components/cards/DockerResourceCard";
import Chip from "@/components/ui/AppChip";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { GAP_MD, GAP_SM, GAP_XS } from "@/theme/constants";
import { longTextStyles, responsiveTextStyles } from "@/theme/tableStyles";

export interface DockerImageRow {
  containers: number;
  created: string;
  id: string;
  repo: string;
  shortId: string;
  size: string;
  tags: string[];
  updateAvailable?: boolean;
  updateCheckReason?: string;
  updateCheckState?: DockerUpdateCheckState;
}

export interface DockerImageCardProps {
  image: DockerImageRow;
  onSelect: (checked: boolean) => void;
  selected: boolean;
}

const DockerImageCard = ({
  image,
  selected,
  onSelect,
}: DockerImageCardProps) => {
  return (
    <DockerResourceCard
      icon="mdi:layers"
      label={`image ${image.repo}`}
      onSelect={onSelect}
      selected={selected}
      subtitle={`${image.size} MB · ${image.created}`}
      title={image.repo}
    >
      <div
        style={{
          display: "flex",
          flexWrap: "wrap",
          gap: GAP_XS,
          marginBottom: GAP_MD,
        }}
      >
        {image.tags.map((tag) => (
          <AppTooltip
            contentWidth
            copyText={tag}
            key={tag}
            onlyWhenTruncated
            title={tag}
          >
            <Chip label={tag} size="small" variant="soft" />
          </AppTooltip>
        ))}
      </div>

      <AppTypography
        color="text.secondary"
        style={{
          fontFamily: "var(--app-font-mono)",
          marginBottom: GAP_MD,
          ...responsiveTextStyles,
        }}
        variant="body2"
      >
        ID: {image.shortId}
      </AppTypography>

      <div
        style={{
          display: "flex",
          flexWrap: "wrap",
          gap: GAP_SM,
          marginBottom: GAP_MD,
        }}
      >
        <Chip
          color={image.containers > 0 ? "success" : "default"}
          label={`Used by ${image.containers}`}
          size="small"
          variant="soft"
        />
        {image.updateAvailable && (
          <Chip
            color="warning"
            label="Update available"
            size="small"
            variant="soft"
          />
        )}
        {image.updateCheckState === "uncheckable" && (
          <Chip
            color="info"
            label="Cannot check"
            size="small"
            title={image.updateCheckReason}
            variant="soft"
          />
        )}
      </div>

      <AppTypography color="text.secondary" variant="caption">
        Full ID
      </AppTypography>
      <AppTypography
        color="text.secondary"
        style={{
          fontFamily: "var(--app-font-mono)",
          marginBottom: GAP_XS,
          ...longTextStyles,
        }}
        variant="body2"
      >
        {image.id}
      </AppTypography>
    </DockerResourceCard>
  );
};

export default DockerImageCard;
