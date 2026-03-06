import TemperatureIcon from "@mui/icons-material/Thermostat";
import { Typography } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import React from "react";

import { linuxio } from "@/api";
import DashboardCard from "@/components/cards/DashboardCard";
import { useCapability } from "@/hooks/useCapabilities";

const MotherBoardInfo: React.FC = () => {
  const theme = useTheme();
  const { isEnabled: lmSensorsAvailable } = useCapability("lmSensorsAvailable");
  const { data: motherboardInfo } =
    linuxio.system.get_motherboard_info.useQuery({
      refetchInterval: 50000,
    });

  const visibleDetails = motherboardInfo ? (
    <div
      style={{ display: "flex", flexDirection: "column", width: "fit-content" }}
    >
      {[
        {
          label: "Board",
          value: `${motherboardInfo.baseboard.manufacturer} - ${motherboardInfo.baseboard.model}`,
        },
        {
          label: "BIOS",
          value: `${motherboardInfo.bios.vendor}, V.${motherboardInfo.bios.version}`,
        },
      ].map(({ label, value }, index, rows) => (
        <div
          key={label}
          style={{
            display: "flex",
            alignItems: "baseline",
            justifyContent: "flex-start",
            paddingTop: theme.spacing(0.5),
            paddingBottom: theme.spacing(0.5),
            borderBottom:
              index === rows.length - 1
                ? "none"
                : "1px solid var(--mui-palette-divider)",
            gap: theme.spacing(1),
          }}
        >
          <Typography
            variant="caption"
            color="text.secondary"
            sx={{
              textTransform: "uppercase",
              letterSpacing: "0.06em",
              fontSize: "0.62rem",
              flexShrink: 0,
            }}
          >
            {label}
          </Typography>
          <Typography variant="body2" fontWeight={500} noWrap>
            {value}
          </Typography>
        </div>
      ))}
    </div>
  ) : (
    <Typography variant="body2">No system information available.</Typography>
  );

  const IconText = lmSensorsAvailable
    ? motherboardInfo?.temperatures?.socket?.[0]
      ? `${motherboardInfo.temperatures.socket[0]}°C`
      : "--°C"
    : "N/A";

  return (
    <DashboardCard
      title="Motherboard"
      stats={visibleDetails}
      icon_text={IconText}
      icon={TemperatureIcon}
      iconProps={{ sx: { color: "text.secondary" } }}
      avatarIcon="bi:motherboard"
    />
  );
};

export default MotherBoardInfo;
