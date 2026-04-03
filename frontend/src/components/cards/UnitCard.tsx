import React from "react";

import FrostedCard from "@/components/cards/FrostedCard";
import { getServiceStatusColor } from "@/constants/statusColors";

export interface UnitListItem {
  name: string;
  description?: string;
  load_state: string;
  active_state: string;
  sub_state: string;
  unit_file_state: string;
}

const baseCardStyle: React.CSSProperties = {
  padding: 12,
  display: "flex",
  flexDirection: "column",
  height: "100%",
  cursor: "pointer",
  transition:
    "transform 0.2s, box-shadow 0.2s, border 0.3s ease-in-out, margin 0.3s ease-in-out",
  borderBottomWidth: 2,
  borderBottomStyle: "solid",
};

const cardStyle: React.CSSProperties = {
  ...baseCardStyle,
  borderBottomColor:
    "color-mix(in srgb, var(--svc-status-color), transparent 70%)",
};

const selectedCardStyle: React.CSSProperties = {
  ...baseCardStyle,
  width: "100%",
  borderBottomColor: "var(--svc-status-color)",
};

interface UnitCardProps<T extends UnitListItem> {
  item: T;
  isSelected: boolean;
  onExpand: (name: string | null) => void;
  renderSummaryRows: (item: T) => React.ReactNode;
  renderSelectedRows?: (item: T) => React.ReactNode;
  renderActions?: (item: T) => React.ReactNode;
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

  return (
    <FrostedCard
      onClick={() => onExpand(isSelected ? null : item.name)}
      hoverLift={!isSelected}
      className="fc-svc-card"
      style={
        {
          "--svc-status-color": statusColor,
          ...(isSelected ? selectedCardStyle : cardStyle),
        } as React.CSSProperties
      }
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
          <div
            style={{
              fontSize: "0.875rem",
              fontWeight: "bold",
              whiteSpace: "nowrap",
              overflow: "hidden",
              textOverflow: "ellipsis",
            }}
          >
            {item.name}
          </div>
          {item.description && (
            <div
              style={{
                marginTop: 2,
                fontSize: "0.7rem",
                color: "var(--app-palette-text-secondary)",
                whiteSpace: "nowrap",
                overflow: "hidden",
                textOverflow: "ellipsis",
              }}
              title={item.description}
            >
              {item.description}
            </div>
          )}
        </div>
        <span
          style={{
            display: "inline-block",
            width: 8,
            height: 8,
            borderRadius: "50%",
            backgroundColor: statusColor,
            flexShrink: 0,
            marginTop: 4,
          }}
        />
      </div>

      <div
        style={{ flex: 1, display: "flex", flexDirection: "column" }}
        className="svc-card-details"
      >
        <div style={{ flex: 1 }} className="svc-rows-wrapper">
          {renderSummaryRows(item)}
          {isSelected && renderSelectedRows?.(item)}
        </div>
        {isSelected && renderActions && (
          <div onClick={(e) => e.stopPropagation()}>{renderActions(item)}</div>
        )}
      </div>
    </FrostedCard>
  );
}

export default UnitCard;
