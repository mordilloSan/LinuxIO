import { useTheme } from "@mui/material/styles";

import { linuxio } from "@/api";
import DashboardCard from "@/components/cards/DashboardCard";
import ErrorMessage from "@/components/errors/Error";
import { GradientCircularGauge } from "@/components/gauge/CirularGauge";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppTypography from "@/components/ui/AppTypography";
import { formatFileSize } from "@/utils/formaters";

const calculatePercentage = (used: number, total: number) =>
  ((used / total) * 100).toFixed(2);

const MemoryUsage = () => {
  const theme = useTheme();
  const {
    data: memoryData,
    isPending,
    isError,
  } = linuxio.system.get_memory_info.useQuery({
    refetchInterval: 2000,
  });

  const ramUsagePercentage = memoryData?.system?.active
    ? parseFloat(
        calculatePercentage(memoryData.system.active, memoryData.system.total),
      )
    : 0;

  const data = {
    title: "Memory Usage",
    stats2: isError ? (
      <ErrorMessage />
    ) : isPending ? (
      <ComponentLoader />
    ) : (
      <GradientCircularGauge
        value={ramUsagePercentage}
        gradientColors={[
          theme.chart.tx,
          theme.palette.warning.main,
          theme.palette.error.main,
        ]}
        size={108}
        thickness={9.8}
        showPercentage={true}
      />
    ),
    stats: (
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          alignSelf: "flex-start",
          width: "fit-content",
        }}
      >
        {[
          {
            label: "Total",
            value: formatFileSize(memoryData?.system?.total ?? 0, 2),
          },
          {
            label: "Used",
            value: formatFileSize(memoryData?.system?.active ?? 0, 2),
          },
          {
            label: "Docker",
            value: formatFileSize(memoryData?.docker?.used ?? 0, 2),
          },
          {
            label: "Swap",
            value: `${formatFileSize((memoryData?.system?.swapTotal ?? 0) - (memoryData?.system?.swapFree ?? 0), 2)}/${formatFileSize(memoryData?.system?.swapTotal ?? 0, 2)}`,
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
                  : "1px solid var(--app-palette-divider)",
              gap: theme.spacing(1),
            }}
          >
            <AppTypography
              variant="caption"
              color="text.secondary"
              style={{
                textTransform: "uppercase",
                letterSpacing: "0.06em",
                fontSize: "0.62rem",
                flexShrink: 0,
              }}
            >
              {label}
            </AppTypography>
            <AppTypography variant="body2" fontWeight={500} noWrap>
              {value}
            </AppTypography>
          </div>
        ))}
      </div>
    ),
    avatarIcon: "la:memory",
  };

  return <DashboardCard {...data} />;
};

export default MemoryUsage;
