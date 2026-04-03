import { Icon } from "@iconify/react";
import React from "react";

import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import AppButton from "@/components/ui/AppButton";
import AppDivider from "@/components/ui/AppDivider";
import AppTypography from "@/components/ui/AppTypography";

export interface DockerResourceListCardProps {
  icon: React.ReactNode;
  title: string;
  subtitle: React.ReactNode;
  onViewAll: () => void;
  columnHeaders: { label: string; hiddenXs?: boolean }[];
  gridClassName: string;
  children: React.ReactNode;
  isEmpty: boolean;
  emptyText: string;
  footerText: string;
  scrollHeight: number;
}

const DockerResourceListCard: React.FC<DockerResourceListCardProps> = ({
  icon,
  title,
  subtitle,
  onViewAll,
  columnHeaders,
  gridClassName,
  children,
  isEmpty,
  emptyText,
  footerText,
  scrollHeight,
}) => (
  <FrostedCard>
    <div
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        padding: 8,
        paddingBottom: 6,
      }}
    >
      <CardIconHeader icon={icon} title={title} subtitle={subtitle} />
      <AppButton size="small" onClick={onViewAll} style={{ flexShrink: 0 }}>
        <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
          View All
          <Icon icon="mdi:chevron-right" width={20} height={20} />
        </span>
      </AppButton>
    </div>

    <div className={gridClassName} style={{ paddingInline: 8, paddingBlock: 3 }}>
      {columnHeaders.map(({ label, hiddenXs }) => (
        <AppTypography
          key={label}
          variant="overline"
          color="text.secondary"
          className={hiddenXs ? "dd-hidden-xs" : undefined}
          style={{ fontSize: "0.65rem" }}
        >
          {label}
        </AppTypography>
      ))}
    </div>
    <AppDivider />

    <div
      className="custom-scrollbar"
      style={{ maxHeight: scrollHeight, overflowY: "auto" }}
    >
      {isEmpty ? (
        <div
          style={{ paddingInline: 8, paddingBlock: 12, textAlign: "center" }}
        >
          <AppTypography variant="body2" color="text.secondary">
            {emptyText}
          </AppTypography>
        </div>
      ) : (
        children
      )}
    </div>

    <AppDivider />
    <div style={{ paddingInline: 8, paddingBlock: 4 }}>
      <AppTypography variant="caption" color="text.secondary">
        {footerText}
      </AppTypography>
    </div>
  </FrostedCard>
);

export default DockerResourceListCard;
