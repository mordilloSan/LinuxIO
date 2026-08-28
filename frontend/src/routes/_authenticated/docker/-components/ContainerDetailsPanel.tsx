import { Icon } from "@iconify/react";

import type { ContainerInfo } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import type { ContainerInfoSection } from "@/components/docker/ContainerInfoSections";
import ContainerInfoSections from "@/components/docker/ContainerInfoSections";
import Chip from "@/components/ui/AppChip";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTypography from "@/components/ui/AppTypography";
import { getContainerStatusColor } from "@/constants/statusColors";
import {
  getContainerDisplayState,
  getContainerName,
} from "@/utils/dockerContainer";

interface ContainerDetailsPanelProps {
  container: ContainerInfo;
  onClose?: () => void;
  sections?: ContainerInfoSection[];
  showStatus?: boolean;
  subtitle?: string;
  title?: string;
  withHeader?: boolean;
}

const ContainerDetailsPanel = ({
  container,
  onClose,
  sections = ["monitoring"],
  showStatus = true,
  subtitle = "Live metrics",
  title,
  withHeader = true,
}: ContainerDetailsPanelProps) => {
  const name = getContainerName(container);
  const displayState = getContainerDisplayState(container);
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
        gap: "var(--app-space-4)",
        overflowY: "auto",
      }}
    >
      {withHeader && (
        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            alignItems: "flex-start",
            gap: "var(--app-space-4)",
            marginBottom: "var(--app-space-2)",
            minWidth: 0,
          }}
        >
          <div style={{ flex: 1, minWidth: 0 }}>
            <AppTypography
              component="div"
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
                gap: "var(--app-space-4)",
                flexShrink: 0,
              }}
            >
              {showStatus && (
                <Chip
                  color={getContainerStatusColor(displayState)}
                  label={displayState}
                  size="xsmall"
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
