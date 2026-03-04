import TemperatureIcon from "@mui/icons-material/Thermostat";
import { Stack, Typography } from "@mui/material";
import React from "react";

import { linuxio } from "@/api";
import DashboardCard from "@/components/cards/DashboardCard";

const MotherBoardInfo: React.FC = () => {
  const { data: motherboardInfo } =
    linuxio.system.get_motherboard_info.useQuery({
      refetchInterval: 50000,
    });

  const visibleDetails = motherboardInfo ? (
    <Stack
      sx={{ display: "flex", flexDirection: "column", width: "fit-content" }}
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
      ].map(({ label, value }) => (
        <Stack
          key={label}
          direction="row"
          alignItems="baseline"
          sx={{
            justifyContent: "flex-start",
            py: 0.5,
            borderBottom: "1px solid",
            borderColor: "divider",
            "&:last-child": { borderBottom: "none" },
            gap: 1,
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
        </Stack>
      ))}
    </Stack>
  ) : (
    <Typography variant="body2">No system information available.</Typography>
  );

  const IconText = motherboardInfo?.temperatures?.socket?.[0]
    ? `${motherboardInfo.temperatures.socket[0]}°C`
    : "--°C";

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
