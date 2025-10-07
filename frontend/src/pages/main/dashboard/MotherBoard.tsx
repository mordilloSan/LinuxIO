import TemperatureIcon from "@mui/icons-material/Thermostat";
import { Typography, Box } from "@mui/material";
import { useQuery } from "@tanstack/react-query";
import React from "react";

import GeneralCard from "@/components/cards/GeneralCard";
import axios from "@/utils/axios";

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
  const { data: motherboardInfo } = useQuery<MotherboardInfo>({
    queryKey: ["motherboardInfo"],
    queryFn: async () => {
      const res = await axios.get("/system/baseboard");
      return res.data;
    },
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
