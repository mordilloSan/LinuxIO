import React from "react";

import FrostedCard from "@/components/cards/FrostedCard";
import AppCheckbox from "@/components/ui/AppCheckbox";
import Chip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";
import { responsiveTextStyles, longTextStyles } from "@/theme/tableStyles";

export interface DockerImageRow {
  id: string;
  repo: string;
  tag: string;
  shortId: string;
  size: string;
  created: string;
  containers: number;
}

export interface DockerImageCardProps {
  image: DockerImageRow;
  selected: boolean;
  onSelect: (checked: boolean) => void;
}

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
            size="small"
            checked={selected}
            onChange={(e) => onSelect(e.target.checked)}
          />
          <AppTypography variant="body2" fontWeight={700} noWrap>
            {image.repo}
          </AppTypography>
        </div>
        <Chip
          label={image.tag}
          size="small"
          variant="soft"
          style={{ fontSize: "0.75rem" }}
        />
      </div>

      <div
        style={{
          display: "grid",
          gap: theme.spacing(0.5),
          marginBottom: theme.spacing(1.5),
        }}
      >
        <AppTypography variant="body2" style={responsiveTextStyles}>
          Size: {image.size} MB
        </AppTypography>
        <AppTypography
          variant="body2"
          style={{ fontFamily: "monospace", ...responsiveTextStyles }}
        >
          ID: {image.shortId}
        </AppTypography>
        <AppTypography
          variant="body2"
          style={{ fontSize: "0.82rem", ...responsiveTextStyles }}
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
          label={`Used by ${image.containers}`}
          size="small"
          variant="soft"
          color={image.containers > 0 ? "success" : "default"}
        />
      </div>

      <AppTypography variant="caption" color="text.secondary">
        Full ID
      </AppTypography>
      <AppTypography
        variant="body2"
        style={{
          fontFamily: "monospace",
          fontSize: "0.75rem",
          marginBottom: 4,
          ...longTextStyles,
        }}
      >
        {image.id}
      </AppTypography>
    </FrostedCard>
  );
};

export default DockerImageCard;
