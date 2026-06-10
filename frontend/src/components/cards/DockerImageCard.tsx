import React from "react";

import FrostedCard from "@/components/cards/FrostedCard";
import AppCheckbox from "@/components/ui/AppCheckbox";
import Chip from "@/components/ui/AppChip";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";
import { longTextStyles, responsiveTextStyles } from "@/theme/tableStyles";

export interface DockerImageRow {
  containers: number;
  created: string;
  id: string;
  repo: string;
  shortId: string;
  size: string;
  tag: string;
  updateAvailable?: boolean;
}

export interface DockerImageCardProps {
  image: DockerImageRow;
  onSelect: (checked: boolean) => void;
  selected: boolean;
}

const DOCKER_TOAST_META = { href: "/docker", label: "Open Docker" };

const DockerImageCard: React.FC<DockerImageCardProps> = ({
  image,
  selected,
  onSelect,
}) => {
  const theme = useAppTheme();

  return (
    <FrostedCard style={{ padding: 8 }}>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          gap: theme.spacing(1),
          marginBottom: theme.spacing(1),
        }}
      >
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: theme.spacing(1),
          }}
        >
          <AppCheckbox
            checked={selected}
            onChange={(e) => onSelect(e.target.checked)}
            size="small"
          />
          <AppTypography
            copyText={image.repo}
            fontWeight={700}
            noWrap
            title={image.repo}
            toastMeta={DOCKER_TOAST_META}
            variant="body2"
          >
            {image.repo}
          </AppTypography>
        </div>
        <AppTooltip
          contentWidth
          copyText={image.tag}
          onlyWhenTruncated
          title={image.tag}
          toastMeta={DOCKER_TOAST_META}
        >
          <Chip
            label={image.tag}
            size="small"
            style={{ fontSize: "0.75rem" }}
            variant="soft"
          />
        </AppTooltip>
      </div>

      <div
        style={{
          display: "grid",
          gap: theme.spacing(0.5),
          marginBottom: theme.spacing(1.5),
        }}
      >
        <AppTypography style={responsiveTextStyles} variant="body2">
          Size: {image.size} MB
        </AppTypography>
        <AppTypography
          style={{ fontFamily: "monospace", ...responsiveTextStyles }}
          variant="body2"
        >
          ID: {image.shortId}
        </AppTypography>
        <AppTypography
          style={{ fontSize: "0.82rem", ...responsiveTextStyles }}
          variant="body2"
        >
          Created: {image.created}
        </AppTypography>
      </div>

      <div
        style={{
          display: "flex",
          gap: theme.spacing(1),
          marginBottom: theme.spacing(1.5),
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
      </div>

      <AppTypography color="text.secondary" variant="caption">
        Full ID
      </AppTypography>
      <AppTypography
        style={{
          fontFamily: "monospace",
          fontSize: "0.75rem",
          marginBottom: 4,
          ...longTextStyles,
        }}
        variant="body2"
      >
        {image.id}
      </AppTypography>
    </FrostedCard>
  );
};

export default DockerImageCard;
