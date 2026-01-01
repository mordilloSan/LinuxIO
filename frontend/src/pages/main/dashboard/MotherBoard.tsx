import TemperatureIcon from "@mui/icons-material/Thermostat";
import { Typography, Box } from "@mui/material";
import React from "react";

import { linuxio } from "@/api/linuxio";
import GeneralCard from "@/components/cards/GeneralCard";

interface MotherboardInfo {
  baseboard: {
    manufacturer: string;
    model: string;
  };
  bios: {
    vendor: string;
    version: string;
  };
  temperatures?: {
    socket: number[];
  };
}

const MotherBoardInfo: React.FC = () => {
  const { data: motherboardInfo } = linuxio.call<MotherboardInfo>(
    "system",
    "get_motherboard_info",
    [],
    { refetchInterval: 50000 },
  );

  const visibleDetails = motherboardInfo ? (
    <Box sx={{ display: "flex", gap: 1, flexDirection: "column" }}>
      <Typography variant="body1">
        {`${motherboardInfo.baseboard.manufacturer} - ${motherboardInfo.baseboard.model}`}
      </Typography>
      <Typography variant="body1">
        {`${motherboardInfo.bios.vendor}, V.${motherboardInfo.bios.version}`}
      </Typography>
    </Box>
  ) : (
    <Typography variant="body2">No system information available.</Typography>
  );

  const IconText = motherboardInfo?.temperatures?.socket?.[0]
    ? `${motherboardInfo.temperatures.socket[0]}°C`
    : "--°C";

  return (
    <GeneralCard
      title="Motherboard"
      stats={visibleDetails}
      icon_text={IconText}
      icon={TemperatureIcon}
      iconProps={{ sx: { color: "grey" } }}
      avatarIcon="bi:motherboard"
    />
  );
};

export default MotherBoardInfo;
