import { Icon } from "@iconify/react";
import React from "react";

import FrostedCard from "@/components/cards/FrostedCard";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";

export const StatusMetric: React.FC<{
  label: string;
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
  subtitle: string;
  indicator?: React.ReactNode;
  children: React.ReactNode;
}> = ({ icon, title, subtitle, indicator, children }) => {
  const theme = useAppTheme();
  return (
    <FrostedCard style={{ padding: 12, position: "relative" }}>
      {indicator}
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: theme.spacing(1.5),
          marginBottom: theme.spacing(2.75),
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
        <div>
          <AppTypography component="h3" fontWeight={600} variant="body2">
            {title}
          </AppTypography>
          <AppTypography color="text.secondary" variant="caption">
            {subtitle}
          </AppTypography>
        </div>
      </div>
      {children}
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
