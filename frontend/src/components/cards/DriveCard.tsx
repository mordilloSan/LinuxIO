import { Icon } from "@iconify/react";
import React from "react";

import FrostedCard from "@/components/cards/FrostedCard";
import Chip from "@/components/ui/AppChip";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";
import { formatFileSize } from "@/utils/formaters";

interface DriveSmartData {
  nvme_smart_health_information_log?: { temperature?: unknown };
  smart_status?: { passed?: boolean };
  temperature?: { current?: unknown };
}

export interface DriveCardProps {
  children?: React.ReactNode;
  expanded: boolean;
  model?: string;
  name: string;
  onClick: () => void;
  sizeBytes: number;
  smart?: DriveSmartData;
  transport: string;
}

const getSmartNumber = (value: unknown): number | null => {
  if (typeof value === "number") return value;
  if (typeof value === "string") {
    const parsed = parseFloat(value.replace(/,/g, ""));
    return Number.isNaN(parsed) ? null : parsed;
  }
  if (value && typeof value === "object") {
    return getSmartNumber((value as { value?: unknown }).value);
  }
  return null;
};

const getTemperature = (smart?: DriveSmartData): number | null => {
  if (!smart) return null;
  return getSmartNumber(
    smart.nvme_smart_health_information_log?.temperature ??
      smart.temperature?.current ??
      null,
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
      onClick={onClick}
      style={{
        padding: 8,
        position: "relative",
        cursor: "pointer",
      }}
    >
      {transport.toLowerCase() === "usb" ? (
        <AppTooltip arrow title="Create Bootable USB">
          <div
            className="fc-opacity-hover"
            onClick={(e) => {
              e.stopPropagation();
              // TODO: Add handler for bootable USB creation
            }}
            style={{
              position: "absolute",
              top: 8,
              right: 8,
              cursor: "pointer",
            }}
          >
            <Icon
              color={theme.palette.text.secondary}
              icon="mdi:pencil"
              width={20}
            />
          </div>
        </AppTooltip>
      ) : temperature !== null ? (
        <AppTooltip arrow placement="top" title="Drive Temperature">
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
              color={getTemperatureColor(temperature)}
              fontWeight={600}
              variant="body2"
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
          color={theme.palette.primary.main}
          icon={transport === "nvme" ? "mdi:harddisk" : "mdi:harddisk-plus"}
          width={32}
        />
        <div style={{ marginLeft: 6, flexGrow: 1, minWidth: 0 }}>
          <AppTypography fontWeight={600} noWrap variant="subtitle1">
            /dev/{name}
          </AppTypography>
          <AppTypography
            color="text.secondary"
            noWrap
            title={model || "Unknown Model"}
            variant="body2"
          >
            {model || "Unknown Model"}
          </AppTypography>
        </div>
      </div>

      <div style={{ display: "flex", gap: 4, flexWrap: "wrap" }}>
        <Chip
          color="primary"
          label={formatFileSize(sizeBytes)}
          size="small"
          variant="soft"
        />
        <Chip label={transport.toUpperCase()} size="small" variant="soft" />
        {smart?.smart_status && (
          <Chip
            color={getHealthColor(smart)}
            label={
              getHealthColor(smart) === "success"
                ? "Healthy"
                : getHealthColor(smart) === "error"
                  ? "Failing"
                  : "Unknown"
            }
            size="small"
            variant="soft"
          />
        )}
      </div>

      {children}
    </FrostedCard>
  );
};

export default DriveCard;
