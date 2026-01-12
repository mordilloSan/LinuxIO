import TemperatureIcon from "@mui/icons-material/Thermostat";
import { Typography, Box } from "@mui/material";
import React from "react";

import linuxio from "@/api/react-query";
import GeneralCard from "@/components/cards/GeneralCard";

const MotherBoardInfo: React.FC = () => {
  const { data: motherboardInfo } =
    linuxio.system.get_motherboard_info.useQuery({
      refetchInterval: 50000,
    });

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
