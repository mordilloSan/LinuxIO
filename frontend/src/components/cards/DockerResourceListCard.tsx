import { Icon } from "@iconify/react";
import React from "react";

import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import AppButton from "@/components/ui/AppButton";
import AppDivider from "@/components/ui/AppDivider";
import AppTypography from "@/components/ui/AppTypography";

export interface DockerResourceListCardProps {
  children: React.ReactNode;
  columnHeaders: { label: string; hiddenXs?: boolean }[];
  emptyText: string;
  footerText: string;
  gridClassName: string;
  icon: React.ReactNode;
  isEmpty: boolean;
  onViewAll: () => void;
  scrollHeight: number;
  subtitle: React.ReactNode;
  title: string;
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
    <CardIconHeader
      icon={icon}
      right={
        <AppButton onClick={onViewAll} size="small" style={{ flexShrink: 0 }}>
          <span
            style={{ display: "inline-flex", alignItems: "center", gap: 6 }}
          >
            View All
            <Icon height={20} icon="mdi:chevron-right" width={20} />
          </span>
        </AppButton>
      }
      style={{ padding: 8, paddingBottom: 6 }}
      subtitle={subtitle}
      title={title}
    />

    <div
      className={gridClassName}
      style={{ paddingInline: 8, paddingBlock: 3 }}
    >
      {columnHeaders.map(({ label, hiddenXs }) => (
        <AppTypography
          className={hiddenXs ? "dd-hidden-xs" : undefined}
          color="text.secondary"
          key={label}
          style={{ fontSize: "0.65rem" }}
          variant="overline"
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
          <AppTypography color="text.secondary" variant="body2">
            {emptyText}
          </AppTypography>
        </div>
      ) : (
        children
      )}
    </div>

    <AppDivider />
    <div style={{ paddingInline: 8, paddingBlock: 4 }}>
      <AppTypography color="text.secondary" variant="caption">
        {footerText}
      </AppTypography>
    </div>
  </FrostedCard>
);

export default DockerResourceListCard;
