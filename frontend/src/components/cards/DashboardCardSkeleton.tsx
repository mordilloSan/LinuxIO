import AppCardContent from "@/components/ui/AppCardContent";
import AppSkeleton from "@/components/ui/AppSkeleton";
import { cardHeight } from "@/theme/constants";

import { DASHBOARD_CARD_LAYOUT } from "./DashboardCard";
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
      accent
      aria-busy="true"
      aria-label={`Loading ${title} card`}
      className="dashboard-card-skeleton"
      hoverLift
      style={{
        minHeight: cardHeight,
        display: "flex",
        flexDirection: "column",
      }}
    >
      <AppCardContent>
        <div
          style={{
            alignItems: "center",
            display: "flex",
            justifyContent: "space-between",
            marginBottom: DASHBOARD_CARD_LAYOUT.headerMarginBottom,
          }}
        >
          <AppSkeleton textVariant="h5" width={titleWidth} />
          <AppSkeleton
            height={DASHBOARD_CARD_LAYOUT.avatarSize}
            variant="circular"
            width={DASHBOARD_CARD_LAYOUT.avatarSize}
          />
        </div>

        {layout === "split" ? (
          <div
            style={{
              display: "flex",
              flexDirection: "row",
              gap: DASHBOARD_CARD_LAYOUT.splitRowGap,
              marginTop: DASHBOARD_CARD_LAYOUT.contentMarginTop,
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
                height: DASHBOARD_CARD_LAYOUT.chartHeight,
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
          <div style={{ marginTop: DASHBOARD_CARD_LAYOUT.statsOnlyMarginTop }}>
            {stats}
          </div>
        )}
      </AppCardContent>
    </FrostedCard>
  );
};

DashboardCardSkeleton.displayName = "DashboardCardSkeleton";

export default DashboardCardSkeleton;
