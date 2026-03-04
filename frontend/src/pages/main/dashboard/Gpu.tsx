import ThermostatIcon from "@mui/icons-material/Thermostat";
import { Typography } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import React from "react";

import { linuxio } from "@/api";
import DashboardCard from "@/components/cards/DashboardCard";

const GpuInfo: React.FC = () => {
  const theme = useTheme();
  const {
    data: gpus,
    isPending: isLoading,
    isError,
  } = linuxio.system.get_gpu_info.useQuery({
    refetchInterval: 50_000,
  });

  let content: React.ReactNode = null;

  if (isLoading) {
    content = <Typography variant="body2">Loading…</Typography>;
  } else if (isError || !gpus || gpus.length === 0) {
    content = (
      <Typography variant="body2">No GPU information available.</Typography>
    );
  } else {
    content = (
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          width: "fit-content",
        }}
      >
        {gpus.flatMap((gpu, idx) =>
          [
            {
              label: "GPU",
              value: `${gpu.vendor} — ${gpu.model}`,
              key: `gpu-${idx}`,
            },
            { label: "Driver", value: gpu.driver, key: `driver-${idx}` },
            { label: "Address", value: gpu.address, key: `address-${idx}` },
          ].map(({ label, value, key }, index, rows) => (
            <div
              key={key}
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
          )),
        )}
      </div>
    );
  }

  return (
    <DashboardCard
      title="GPU"
      stats={content}
      icon={ThermostatIcon}
      iconProps={{ sx: { color: "text.secondary" } }}
      avatarIcon="bi:gpu-card"
    />
  );
};

export default GpuInfo;
