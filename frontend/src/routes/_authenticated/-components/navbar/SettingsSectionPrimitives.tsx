import { Icon } from "@iconify/react";
import { useId, useState, type ReactNode } from "react";

import FrostedCard from "@/components/cards/FrostedCard";
import AppCollapse from "@/components/ui/AppCollapse";
import AppIconButton from "@/components/ui/AppIconButton";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import { useAppTheme } from "@/theme";

/**
 * "default" is the original settings-panel stack (body2 label, single-line
 * value/detail). "compact" trims the label to caption size for dense grids
 * of many metrics (auto-update status, etc) and can show a status dot next
 * to the value. "stat" is the bigger icon+value tile used for headline
 * numbers, and allows the value/detail to wrap instead of truncating.
 */
export const StatusMetric = ({
  detail,
  icon,
  label,
  monoDetail,
  statusColor,
  value,
  variant = "default",
}: {
  detail?: ReactNode;
  icon?: string;
  label: ReactNode;
  monoDetail?: boolean;
  statusColor?: string;
  value: ReactNode;
  variant?: "compact" | "default" | "stat";
}) => {
  const theme = useAppTheme();
  const isStat = variant === "stat";
  const title =
    typeof value === "string" || typeof value === "number"
      ? String(value)
      : undefined;
  const detailTitle = typeof detail === "string" ? detail : undefined;

  const valueNode = isStat ? (
    <AppTypography
      fontWeight={700}
      style={{ fontSize: "1.35rem", lineHeight: 1.2 }}
      variant="body2"
    >
      {value}
    </AppTypography>
  ) : (
    <AppTypography
      fontWeight={500}
      noWrap
      title={variant === "default" ? title : undefined}
      variant="body2"
    >
      {value}
    </AppTypography>
  );

  return (
    <div
      style={
        isStat
          ? {
              display: "flex",
              flexDirection: "column",
              gap: theme.spacing(2),
              minWidth: 0,
            }
          : { minWidth: 0 }
      }
    >
      {icon ? (
        <div
          style={{
            alignItems: "center",
            color: theme.palette.primary.main,
            display: "inline-flex",
          }}
        >
          <Icon height={20} icon={icon} width={20} />
        </div>
      ) : null}
      <AppTypography
        color="text.secondary"
        variant={variant === "default" ? "body2" : "caption"}
      >
        {label}
      </AppTypography>
      {statusColor ? (
        <div
          style={{
            alignItems: "center",
            display: "flex",
            gap: 6,
            minWidth: 0,
          }}
        >
          <StatusDot color={statusColor} size={7} />
          {valueNode}
        </div>
      ) : (
        valueNode
      )}
      {detail ? (
        <AppTypography
          color="text.secondary"
          noWrap={!isStat}
          style={{
            display: "block",
            ...(monoDetail ? { fontFamily: "var(--app-font-mono)" } : {}),
          }}
          title={monoDetail ? detailTitle : undefined}
          variant="caption"
        >
          {detail}
        </AppTypography>
      ) : null}
    </div>
  );
};

export const StatusGroupLabel = ({ children }: { children: ReactNode }) => (
  <AppTypography
    color="text.secondary"
    fontWeight={600}
    style={{ textTransform: "uppercase" }}
    variant="caption"
  >
    {children}
  </AppTypography>
);

export const SectionCard = ({
  icon,
  title,
  subtitle,
  titleAdornment,
  indicator,
  collapsible = false,
  defaultCollapsed = false,
  children,
}: {
  icon: string;
  title: string;
  subtitle?: ReactNode;
  titleAdornment?: ReactNode;
  indicator?: ReactNode;
  collapsible?: boolean;
  defaultCollapsed?: boolean;
  children: ReactNode;
}) => {
  const theme = useAppTheme();
  const contentId = useId();
  const [expanded, setExpanded] = useState(!defaultCollapsed);
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

export const ToggleCard = ({
  label,
  description,
  checked,
  disabled,
  onChange,
}: {
  label: string;
  description: string;
  checked: boolean;
  disabled: boolean;
  onChange: (checked: boolean) => void;
}) => {
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
