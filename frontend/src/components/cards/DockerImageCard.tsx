import type { ReactNode } from "react";

import DockerResourceCard from "@/components/cards/DockerResourceCard";
import Chip from "@/components/ui/AppChip";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { GAP_MD, GAP_XS } from "@/theme/constants";

export interface DockerImageRow {
  containers: number;
  created: string;
  id: string;
  repo: string;
  size: string;
  tags: string[];
}

export interface DockerImageCardProps {
  actions?: ReactNode;
  image: DockerImageRow;
  onOpen?: () => void;
  selected?: boolean;
}

const DockerImageCard = ({
  image,
  actions,
  onOpen,
  selected,
}: DockerImageCardProps) => {
  const usageTooltip = `Used by ${image.containers} ${
    image.containers === 1 ? "container" : "containers"
  }`;

  return (
    <DockerResourceCard
      headerRight={
        <AppTooltip title={usageTooltip}>
          <Chip
            color={image.containers > 0 ? "success" : "default"}
            label={image.containers}
            size="small"
            variant="soft"
          />
        </AppTooltip>
      }
      actions={actions}
      icon="mdi:layers"
      label={`image ${image.repo}`}
      onOpen={onOpen}
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
        copyText={image.id}
        noWrap
        style={{
          marginBottom: GAP_XS,
        }}
        title={image.id}
        variant="body2"
      >
        <span style={{ fontWeight: 700 }}>Full ID: </span>
        <AppTypography
          color="text.secondary"
          component="span"
          style={{ fontFamily: "var(--app-font-mono)" }}
          variant="caption"
        >
          {image.id}
        </AppTypography>
      </AppTypography>
    </DockerResourceCard>
  );
};

export default DockerImageCard;
