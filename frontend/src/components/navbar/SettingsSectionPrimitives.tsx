import { Icon } from "@iconify/react";
import React from "react";

import FrostedCard from "@/components/cards/FrostedCard";
import AppCollapse from "@/components/ui/AppCollapse";
import AppIconButton from "@/components/ui/AppIconButton";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";

export const StatusMetric: React.FC<{
  label: React.ReactNode;
  value: React.ReactNode;
  detail?: React.ReactNode;
}> = ({ label, value, detail }) => {
  const title =
    typeof value === "string" || typeof value === "number"
      ? String(value)
      : undefined;
  return (
    <div style={{ minWidth: 0 }}>
      <AppTypography color="text.secondary" variant="body2">
        {label}
      </AppTypography>
      <AppTypography fontWeight={500} noWrap title={title} variant="body2">
        {value}
      </AppTypography>
      {detail ? (
        <AppTypography
          color="text.secondary"
          noWrap
          style={{ display: "block" }}
          variant="caption"
        >
          {detail}
        </AppTypography>
      ) : null}
    </div>
  );
};

export const StatusGroupLabel: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => (
  <AppTypography
    color="text.secondary"
    fontWeight={600}
    style={{ textTransform: "uppercase" }}
    variant="caption"
  >
    {children}
  </AppTypography>
);

export const SectionCard: React.FC<{
  icon: string;
  title: string;
  subtitle?: React.ReactNode;
  titleAdornment?: React.ReactNode;
  indicator?: React.ReactNode;
  collapsible?: boolean;
  defaultCollapsed?: boolean;
  children: React.ReactNode;
}> = ({
  icon,
  title,
  subtitle,
  titleAdornment,
  indicator,
  collapsible = false,
  defaultCollapsed = false,
  children,
}) => {
  const theme = useAppTheme();
  const contentId = React.useId();
  const [expanded, setExpanded] = React.useState(!defaultCollapsed);
  const headerGap = subtitle ? 2.75 : 1.5;
  const content = collapsible ? (
    <AppCollapse in={expanded} unmountOnExit>
      {children}
    </AppCollapse>
  ) : (
    children
  );

  return (
    <FrostedCard style={{ padding: 12, position: "relative" }}>
      {indicator}
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: theme.spacing(1.5),
          marginBottom: expanded || !collapsible ? theme.spacing(headerGap) : 0,
        }}
      >
        <div
          style={{
            display: "inline-flex",
            alignItems: "center",
            justifyContent: "center",
            width: 36,
            height: 36,
            borderRadius: 8,
            background: theme.palette.action.hover,
            color: theme.palette.primary.main,
            flexShrink: 0,
          }}
        >
          <Icon height={22} icon={icon} width={22} />
        </div>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: theme.spacing(1),
            }}
          >
            <AppTypography component="h3" fontWeight={600} variant="body2">
              {title}
            </AppTypography>
            {titleAdornment}
          </div>
          {subtitle ? (
            <AppTypography color="text.secondary" variant="caption">
              {subtitle}
            </AppTypography>
          ) : null}
        </div>
        {collapsible ? (
          <AppTooltip title={expanded ? "Collapse" : "Expand"}>
            <AppIconButton
              aria-controls={contentId}
              aria-expanded={expanded}
              aria-label={`${expanded ? "Collapse" : "Expand"} ${title}`}
              onClick={() => setExpanded((value) => !value)}
              size="small"
            >
              <Icon
                height={18}
                icon={expanded ? "mdi:chevron-up" : "mdi:chevron-down"}
                width={18}
              />
            </AppIconButton>
          </AppTooltip>
        ) : null}
      </div>
      {collapsible ? <div id={contentId}>{content}</div> : content}
    </FrostedCard>
  );
};

export const ToggleCard: React.FC<{
  label: string;
  description: string;
  checked: boolean;
  disabled: boolean;
  onChange: (checked: boolean) => void;
}> = ({ label, description, checked, disabled, onChange }) => {
  const theme = useAppTheme();
  return (
    <FrostedCard
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        gap: theme.spacing(1.5),
        minHeight: 62,
        padding: 12,
      }}
    >
      <div style={{ minWidth: 0 }}>
        <AppTypography
          fontWeight={600}
          style={{ lineHeight: 1.25 }}
          variant="body2"
        >
          {label}
        </AppTypography>
        <AppTypography
          color="text.secondary"
          noWrap
          style={{ lineHeight: 1.35 }}
          variant="caption"
        >
          {description}
        </AppTypography>
      </div>
      <AppSwitch
        aria-label={label}
        checked={checked}
        disabled={disabled}
        onChange={(_, nextChecked) => onChange(nextChecked)}
      />
    </FrostedCard>
  );
};
