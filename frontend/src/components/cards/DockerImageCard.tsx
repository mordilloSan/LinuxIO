import type { DockerUpdateCheckState } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";
import { longTextStyles, responsiveTextStyles } from "@/theme/tableStyles";

import "./DockerImageCard.css";

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

const DOCKER_TOAST_META = { label: "Open Docker", to: "/docker" } as const;

const DockerImageCard = ({
  image,
  selected,
  onSelect,
}: DockerImageCardProps) => {
  const theme = useAppTheme();
  const selectionLabel = `${selected ? "Deselect" : "Select"} image ${image.repo}`;

  const toggleSelection = () => onSelect(!selected);

  return (
    <AppButton
      aria-label={selectionLabel}
      onClick={toggleSelection}
      aria-pressed={selected}
      className="docker-image-card-button"
    >
      <FrostedCard
        accent
        className={`docker-image-card${selected ? " docker-image-card--selected" : ""}`}
        hoverLift
        style={{
          padding: 8,
          /*
            Selection takes the accent line to full strength. Inline, the way
            every other selectable card does it — a stylesheet rule would need
            !important to beat FrostedCard's inline accent colour, and important
            declarations outrank animations, which would silence the hold
            feedback on a selected card. Inline styles do not.
          */
          ...(selected && { borderBottomColor: "var(--dc-accent)" }),
        }}
      >
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
            <AppTypography
              fontWeight={700}
              noWrap
              title={image.repo}
              toastMeta={DOCKER_TOAST_META}
              variant="body2"
            >
              {image.repo}
            </AppTypography>
          </div>
          <div
            style={{
              display: "flex",
              flexWrap: "wrap",
              justifyContent: "flex-end",
              gap: theme.spacing(0.5),
            }}
          >
            {image.tags.map((tag) => (
              <AppTooltip
                contentWidth
                copyText={tag}
                key={tag}
                onlyWhenTruncated
                title={tag}
                toastMeta={DOCKER_TOAST_META}
              >
                <Chip
                  label={tag}
                  size="small"
                  style={{ fontSize: "0.75rem" }}
                  variant="soft"
                />
              </AppTooltip>
            ))}
          </div>
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
            style={{
              fontFamily: "var(--app-font-mono)",
              ...responsiveTextStyles,
            }}
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
          style={{
            fontFamily: "var(--app-font-mono)",
            fontSize: "0.75rem",
            marginBottom: 4,
            ...longTextStyles,
          }}
          variant="body2"
        >
          {image.id}
        </AppTypography>
      </FrostedCard>
    </AppButton>
  );
};

export default DockerImageCard;
