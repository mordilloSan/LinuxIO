import { Icon } from "@iconify/react";
import React from "react";

import type { ContainerInfo } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import ContainerInfoSections, {
  ContainerInfoSection,
} from "@/components/docker/ContainerInfoSections";
import Chip from "@/components/ui/AppChip";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTypography from "@/components/ui/AppTypography";
import { getContainerStatusColor } from "@/constants/statusColors";
import { useAppTheme } from "@/theme";

const getContainerName = (container: ContainerInfo) =>
  container.Names?.[0]?.replace("/", "") || container.Id.slice(0, 12);

const getDisplayState = (container: ContainerInfo) => {
  const status = container.Status.toLowerCase();
  if (status.includes("unhealthy")) return "Unhealthy";
  if (status.includes("healthy")) return "Healthy";
  if (container.State === "running") return "Running";
  if (container.State === "exited") return "Stopped";
  if (container.State === "dead") return "Dead";
  return container.State || "Unknown";
};

interface ContainerDetailsPanelProps {
  container: ContainerInfo;
  onClose?: () => void;
  sections?: ContainerInfoSection[];
  showStatus?: boolean;
  subtitle?: string;
  title?: string;
  withHeader?: boolean;
}

const ContainerDetailsPanel: React.FC<ContainerDetailsPanelProps> = ({
  container,
  onClose,
  sections = ["monitoring"],
  showStatus = true,
  subtitle = "Live metrics",
  title,
  withHeader = true,
}) => {
  const theme = useAppTheme();
  const name = getContainerName(container);
  const displayState = getDisplayState(container);
  const headerTitle = title ?? name;

  return (
    <FrostedCard
      className="custom-scrollbar"
      style={{
        padding: 12,
        height: "100%",
        minHeight: 0,
        minWidth: 0,
        flex: 1,
        display: "flex",
        flexDirection: "column",
        gap: theme.spacing(1.25),
        overflowY: "auto",
      }}
    >
      {withHeader && (
        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            alignItems: "flex-start",
            gap: theme.spacing(1),
            marginBottom: theme.spacing(0.5),
            minWidth: 0,
          }}
        >
          <div style={{ flex: 1, minWidth: 0 }}>
            <AppTypography
              component="div"
              fontSize="0.875rem"
              fontWeight={700}
              noWrap
              title={headerTitle}
              variant="body2"
            >
              {headerTitle}
            </AppTypography>
            <AppTypography
              color="text.secondary"
              component="div"
              fontSize="0.7rem"
              noWrap
              variant="caption"
            >
              {subtitle}
            </AppTypography>
          </div>
          {(showStatus || onClose) && (
            <div
              style={{
                display: "flex",
                alignItems: "center",
                gap: theme.spacing(0.75),
                flexShrink: 0,
              }}
            >
              {showStatus && (
                <Chip
                  color={getContainerStatusColor(displayState)}
                  label={displayState}
                  size="small"
                  style={{ fontSize: "0.75rem" }}
                  variant="soft"
                />
              )}
              {onClose && (
                <AppIconButton
                  aria-label="Close container details"
                  onClick={onClose}
                  size="small"
                >
                  <Icon height={18} icon="mdi:close" width={18} />
                </AppIconButton>
              )}
            </div>
          )}
        </div>
      )}

      <ContainerInfoSections container={container} sections={sections} />
    </FrostedCard>
  );
};

export default ContainerDetailsPanel;
