import { Icon } from "@iconify/react";
import React from "react";

import FrostedCard from "@/components/cards/FrostedCard";
import Chip from "@/components/ui/AppChip";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";
import { formatFileSize } from "@/utils/formaters";

interface DriveSmartData {
  smart_status?: { passed?: boolean };
  temperature?: { current?: number };
  nvme_smart_health_information_log?: { temperature?: number };
}

export interface DriveCardProps {
  name: string;
  model?: string;
  transport: string;
  sizeBytes: number;
  smart?: DriveSmartData;
  expanded: boolean;
  onClick: () => void;
  children?: React.ReactNode;
}

const getTemperature = (smart?: DriveSmartData): number | null => {
  if (!smart) return null;
  return (
    smart.nvme_smart_health_information_log?.temperature ??
    smart.temperature?.current ??
    null
  );
};

const getTemperatureColor = (temp: number | null): string => {
  if (temp === null) return "text.secondary";
  if (temp > 70) return "error.main";
  if (temp > 50) return "warning.main";
  return "success.main";
};

const getHealthColor = (
  smart?: DriveSmartData,
): "success" | "error" | "warning" | "default" => {
  if (!smart?.smart_status) return "default";
  const passed = smart.smart_status.passed;
  if (passed === true) return "success";
  if (passed === false) return "error";
  return "warning";
};

const DriveCard: React.FC<DriveCardProps> = ({
  name,
  model,
  transport,
  sizeBytes,
  smart,
  expanded,
  onClick,
  children,
}) => {
  const theme = useAppTheme();
  const temperature = getTemperature(smart);

  return (
    <FrostedCard
      hoverLift={!expanded}
      style={{
        padding: 8,
        position: "relative",
        cursor: "pointer",
      }}
      onClick={onClick}
    >
      {transport.toLowerCase() === "usb" ? (
        <AppTooltip title="Create Bootable USB" arrow>
          <div
            className="fc-opacity-hover"
            style={{
              position: "absolute",
              top: 8,
              right: 8,
              cursor: "pointer",
            }}
            onClick={(e) => {
              e.stopPropagation();
              // TODO: Add handler for bootable USB creation
            }}
          >
            <Icon
              icon="mdi:pencil"
              width={20}
              color={theme.palette.text.secondary}
            />
          </div>
        </AppTooltip>
      ) : temperature !== null ? (
        <AppTooltip title="Drive Temperature" placement="top" arrow>
          <div
            style={{
              position: "absolute",
              top: 12,
              right: 12,
              display: "flex",
              alignItems: "center",
              gap: 2,
            }}
          >
            <AppTypography
              variant="body2"
              fontWeight={600}
              color={getTemperatureColor(temperature)}
            >
              {temperature}°C
            </AppTypography>
          </div>
        </AppTooltip>
      ) : null}

      <div
        style={{
          display: "flex",
          alignItems: "center",
          marginBottom: 6,
        }}
      >
        <Icon
          icon={transport === "nvme" ? "mdi:harddisk" : "mdi:harddisk-plus"}
          width={32}
          color={theme.palette.primary.main}
        />
        <div style={{ marginLeft: 6, flexGrow: 1, minWidth: 0 }}>
          <AppTypography variant="subtitle1" fontWeight={600} noWrap>
            /dev/{name}
          </AppTypography>
          <AppTypography
            variant="body2"
            color="text.secondary"
            noWrap
            title={model || "Unknown Model"}
          >
            {model || "Unknown Model"}
          </AppTypography>
        </div>
      </div>

      <div style={{ display: "flex", gap: 4, flexWrap: "wrap" }}>
        <Chip
          label={formatFileSize(sizeBytes)}
          size="small"
          color="primary"
          variant="soft"
        />
        <Chip
          label={transport.toUpperCase()}
          size="small"
          variant="soft"
        />
        {smart?.smart_status && (
          <Chip
            label={
              getHealthColor(smart) === "success"
                ? "Healthy"
                : getHealthColor(smart) === "error"
                  ? "Failing"
                  : "Unknown"
            }
            size="small"
            color={getHealthColor(smart)}
            variant="soft"
          />
        )}
      </div>

      {children}
    </FrostedCard>
  );
};

export default DriveCard;
