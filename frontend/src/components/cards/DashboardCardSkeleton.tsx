import AppCardContent from "@/components/ui/AppCardContent";
import AppSkeleton from "@/components/ui/AppSkeleton";
import { useAppTheme } from "@/theme";
import { cardHeight } from "@/theme/constants";
import { getAccentCardStyles } from "@/theme/surfaces";

import FrostedCard from "./FrostedCard";

export type DashboardCardSkeletonLayout = "split" | "stats";

interface DashboardCardSkeletonProps {
  layout?: DashboardCardSkeletonLayout;
  title: string;
}

const statWidths = ["12ch", "9ch", "14ch"];

const DashboardCardSkeleton = ({
  layout = "split",
  title,
}: DashboardCardSkeletonProps) => {
  const theme = useAppTheme();
  const primaryColor = theme.palette.primary.main;
  const titleWidth = `${Math.min(Math.max(title.length, 6), 18)}ch`;
  const stats = (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: 8,
        width: "100%",
      }}
    >
      {statWidths.map((width) => (
        <AppSkeleton key={width} textVariant="body2" width={width} />
      ))}
    </div>
  );

  return (
    <FrostedCard
      aria-busy="true"
      aria-label={`Loading ${title} card`}
      className="dashboard-card-skeleton"
      style={{
        minHeight: cardHeight,
        display: "flex",
        flexDirection: "column",
        ...getAccentCardStyles(primaryColor),
      }}
    >
      <AppCardContent>
        <div
          style={{
            alignItems: "center",
            display: "flex",
            justifyContent: "space-between",
            marginBottom: 4,
          }}
        >
          <AppSkeleton textVariant="h5" width={titleWidth} />
          <AppSkeleton height={38} variant="circular" width={38} />
        </div>

        {layout === "split" ? (
          <div
            style={{
              display: "flex",
              flexDirection: "row",
              gap: 8,
              marginTop: 12,
            }}
          >
            <div
              style={{
                alignItems: "center",
                display: "flex",
                flex: 1,
                minWidth: 0,
                overflow: "hidden",
              }}
            >
              {stats}
            </div>
            <div
              style={{
                alignItems: "center",
                display: "flex",
                flex: 1,
                height: 120,
                justifyContent: "center",
                minWidth: 0,
                overflow: "hidden",
              }}
            >
              <AppSkeleton
                className="dashboard-card-skeleton__chart"
                height={90}
                variant="rectangular"
                width="100%"
              />
            </div>
          </div>
        ) : (
          <div style={{ marginTop: 28 }}>{stats}</div>
        )}
      </AppCardContent>
    </FrostedCard>
  );
};

DashboardCardSkeleton.displayName = "DashboardCardSkeleton";

export default DashboardCardSkeleton;
