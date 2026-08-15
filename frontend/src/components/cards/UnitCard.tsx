import type { CSSProperties, ReactNode } from "react";

import FrostedCard from "@/components/cards/FrostedCard";
import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import { getServiceStatusColor } from "@/constants/statusColors";
import { TRANSITION_SLOW_CSS } from "@/theme/constants";

export interface UnitListItem {
  active_state: string;
  description?: string;
  load_state: string;
  name: string;
  sub_state: string;
  unit_file_state: string;
}

const baseCardStyle: CSSProperties = {
  padding: 12,
  display: "flex",
  flexDirection: "column",
  height: "100%",
  borderBottomWidth: 2,
  borderBottomStyle: "solid",
};

const cardStyle: CSSProperties = {
  ...baseCardStyle,
  borderBottomColor:
    "color-mix(in srgb, var(--svc-status-color), transparent 70%)",
};

const selectedCardStyle: CSSProperties = {
  ...baseCardStyle,
  width: "100%",
  borderBottomColor: "transparent",
};

interface UnitCardProps<T extends UnitListItem> {
  isSelected: boolean;
  item: T;
  onExpand: (name: string | null) => void;
  renderActions?: (item: T) => ReactNode;
  renderSelectedRows?: (item: T) => ReactNode;
  renderSummaryRows: (item: T) => ReactNode;
}

function UnitCard<T extends UnitListItem>({
  item,
  isSelected,
  onExpand,
  renderSummaryRows,
  renderSelectedRows,
  renderActions,
}: UnitCardProps<T>) {
  const statusColor = getServiceStatusColor(item.active_state);
  const detailsId = `unit-card-${item.name.replace(/[^a-zA-Z0-9_-]/g, "-")}`;

  return (
    <FrostedCard
      className="fc-svc-card"
      hoverLift={!isSelected}
      style={
        {
          "--svc-status-color": statusColor,
          ...(isSelected ? selectedCardStyle : cardStyle),
          transition: `transform var(--hover-lift-duration) var(--hover-lift-ease), box-shadow var(--hover-lift-duration) var(--hover-lift-ease), border ${TRANSITION_SLOW_CSS}, margin ${TRANSITION_SLOW_CSS}`,
        } as CSSProperties
      }
    >
      <AppButton
        aria-controls={detailsId}
        aria-expanded={isSelected}
        aria-label={`${isSelected ? "Collapse" : "Expand"} ${item.name}`}
        className="fc-svc-card__trigger"
        color="inherit"
        fullWidth
        onClick={() => onExpand(isSelected ? null : item.name)}
        style={{
          alignItems: "stretch",
          color: "inherit",
          flexDirection: "column",
          justifyContent: "flex-start",
          padding: 0,
          textAlign: "left",
        }}
      >
        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            alignItems: "flex-start",
            marginBottom: 12,
            gap: 8,
          }}
        >
          <div style={{ flex: 1, minWidth: 0 }}>
            <AppTypography
              component="div"
              fontSize="0.875rem"
              fontWeight="bold"
              noWrap
              title={item.name}
              variant="body2"
            >
              {item.name}
            </AppTypography>
            {/*
              Always rendered, invisibly when the unit has no description, so a
              description-less card keeps the same height as its neighbours.
            */}
            <AppTypography
              aria-hidden={item.description ? undefined : true}
              component="div"
              color="text.secondary"
              fontSize="0.7rem"
              noWrap
              style={{
                marginTop: 2,
                visibility: item.description ? undefined : "hidden",
              }}
              title={item.description}
              variant="caption"
            >
              {item.description || " "}
            </AppTypography>
          </div>
          <StatusDot color={statusColor} size={8} style={{ marginTop: 4 }} />
        </div>

        <div
          className="svc-card-details"
          id={detailsId}
          style={{ flex: 1, display: "flex", flexDirection: "column" }}
        >
          <div className="svc-rows-wrapper" style={{ flex: 1 }}>
            {renderSummaryRows(item)}
            {isSelected && renderSelectedRows?.(item)}
          </div>
        </div>
      </AppButton>
      {isSelected && renderActions && (
        <div style={{ marginTop: 12 }}>{renderActions(item)}</div>
      )}
    </FrostedCard>
  );
}

export default UnitCard;
