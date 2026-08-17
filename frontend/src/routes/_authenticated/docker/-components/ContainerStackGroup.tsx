import { Icon } from "@iconify/react";
import type { ReactNode } from "react";

import type { ContainerInfo } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import DockerIcon from "@/components/docker/DockerIcon";
import AppButton from "@/components/ui/AppButton";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import { getContainerStatusColor } from "@/constants/statusColors";
import { useAppTheme } from "@/theme";
import { CARD_PADDING_LG } from "@/theme/constants";

import {
  formatStackSummary,
  getStackDisplayState,
  summarizeStack,
} from "./containerStacks";

import "./container-stacks.css";

interface ContainerStackBandProps {
  children: ReactNode;
  containers: ContainerInfo[];
  onToggle: () => void;
  project: string;
}

/**
 * The expanded form of a compose stack in card view: chrome drawn around the
 * member cards rather than a surface of its own, so the cards inside stay
 * exactly the cards they are when loose.
 */
export function ContainerStackBand({
  children,
  containers,
  onToggle,
  project,
}: ContainerStackBandProps) {
  const summary = summarizeStack(containers);
  const displayState = getStackDisplayState(summary);

  return (
    <section aria-label={`Stack ${project}`} className="container-stack-band">
      <button
        aria-expanded
        aria-label={`Collapse stack ${project}`}
        className="container-stack-band__header"
        onClick={onToggle}
        type="button"
      >
        <Icon
          className="container-stack__chevron"
          height={18}
          icon="mdi:chevron-down"
          width={18}
        />
        <Icon
          className="container-stack__icon"
          height={18}
          icon="mdi:layers-outline"
          width={18}
        />
        <AppTypography fontWeight={600} noWrap title={project} variant="body2">
          {project}
        </AppTypography>
        <AppTypography color="text.secondary" noWrap variant="caption">
          {formatStackSummary(summary)}
        </AppTypography>
        <span className="container-stack-band__spacer" />
        <StatusDot
          color={getContainerStatusColor(displayState)}
          size={8}
          tooltip={displayState}
        />
      </button>
      {children}
    </section>
  );
}

interface ContainerStackSummaryCardProps {
  containers: ContainerInfo[];
  onExpand: () => void;
  project: string;
}

/**
 * The collapsed form: one card in the flow, where the whole stack folded to.
 * Laid out like a collapsed ContainerCard — 48px icon, name button, status dot
 * top right — with the member summary sitting where the action strip would.
 * The icon identifier is the project name, the same fallback the backend's
 * ResolveIconIdentifier applies to unlabelled containers and compose stacks.
 */
export function ContainerStackSummaryCard({
  containers,
  onExpand,
  project,
}: ContainerStackSummaryCardProps) {
  const theme = useAppTheme();
  const summary = summarizeStack(containers);
  const displayState = getStackDisplayState(summary);

  return (
    <FrostedCard
      accent
      hoverLift
      style={{
        padding: CARD_PADDING_LG,
        display: "flex",
        flexDirection: "column",
        height: "100%",
        width: "100%",
        minWidth: 0,
        position: "relative",
      }}
    >
      <div
        style={{
          alignItems: "center",
          display: "flex",
          gap: 4,
          position: "absolute",
          right: 8,
          top: 14,
        }}
      >
        {summary.updateAvailable && (
          <AppTooltip arrow title="Update available">
            <span
              aria-label="Update available"
              role="img"
              style={{
                alignItems: "center",
                color: theme.palette.warning.main,
                display: "flex",
              }}
            >
              <Icon aria-hidden icon="mdi:alert" width={16} />
            </span>
          </AppTooltip>
        )}
        <StatusDot
          color={getContainerStatusColor(displayState)}
          tooltip={displayState}
        />
      </div>

      <div
        style={{
          display: "flex",
          flexDirection: "row",
          alignItems: "center",
          width: "100%",
        }}
      >
        <div
          style={{
            width: 48,
            height: 48,
            minWidth: 48,
            minHeight: 48,
            flexShrink: 0,
            marginRight: 6,
            alignSelf: "flex-start",
          }}
        >
          <DockerIcon
            alt={project}
            identifier={project.toLowerCase()}
            size={48}
          />
        </div>
        <div style={{ flex: 0.95, minWidth: 0 }}>
          <AppButton
            aria-expanded={false}
            aria-label={`Expand stack ${project}`}
            color="inherit"
            fullWidth
            onClick={onExpand}
            style={{
              alignItems: "flex-start",
              color: "inherit",
              justifyContent: "flex-start",
              minWidth: 0,
              padding: 0,
              textAlign: "left",
            }}
          >
            <AppTypography
              fontWeight={600}
              noWrap
              style={{
                marginLeft: 4,
                marginRight: 0.4,
                marginBottom: 2,
                fontSize: "1.05rem",
              }}
              title={project}
              variant="subtitle1"
            >
              {project}
            </AppTypography>
          </AppButton>
          <AppTypography
            color="text.secondary"
            noWrap
            style={{ display: "block", marginLeft: 4 }}
            variant="caption"
          >
            {formatStackSummary(summary)}
          </AppTypography>
        </div>
      </div>
    </FrostedCard>
  );
}
