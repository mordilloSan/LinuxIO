import { linuxio } from "@/api";
import DashboardCard from "@/components/cards/DashboardCard";
import ErrorMessage from "@/components/errors/Error";
import { GradientCircularGauge } from "@/components/gauge/CirularGauge";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";
import { formatFileSize } from "@/utils/formaters";

const calculatePercentage = (used: number, total: number) =>
  ((used / total) * 100).toFixed(2);

const MemoryUsage = () => {
  const theme = useAppTheme();
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
  const swapUsed = Math.max(
    (memoryData?.system?.swapTotal ?? 0) - (memoryData?.system?.swapFree ?? 0),
    0,
  );

  const data = {
    title: "Memory Usage",
    stats2: isError ? (
      <ErrorMessage />
    ) : isPending ? (
      <ComponentLoader />
    ) : (
      <GradientCircularGauge
        gradientColors={[
          theme.chart.tx,
          theme.palette.warning.main,
          theme.palette.error.main,
        ]}
        showPercentage={true}
        size={108}
        thickness={9.8}
        value={ramUsagePercentage}
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
            label: "Usage",
            value: `${formatFileSize(memoryData?.system?.active ?? 0, 2)} / ${formatFileSize(memoryData?.system?.total ?? 0, 2)}`,
          },
          {
            label: "Swap",
            value: `${formatFileSize(swapUsed, 2)} / ${formatFileSize(memoryData?.system?.swapTotal ?? 0, 2)}`,
          },
          {
            label: "Docker",
            value: formatFileSize(memoryData?.docker?.used ?? 0, 2),
          },
          {
            label: "ZFS ARC",
            value: formatFileSize(memoryData?.zfs?.arc ?? 0, 2),
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
              color="text.secondary"
              style={{
                textTransform: "uppercase",
                letterSpacing: "0.06em",
                fontSize: "0.62rem",
                flexShrink: 0,
              }}
              variant="caption"
            >
              {label}
            </AppTypography>
            <AppTypography fontWeight={500} noWrap variant="body2">
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
