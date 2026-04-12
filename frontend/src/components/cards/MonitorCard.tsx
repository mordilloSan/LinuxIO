import { Icon } from "@iconify/react";
import React from "react";

import type { MonitoringRange } from "@/api";
import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import AppSelect from "@/components/ui/AppSelect";
import { cardHeight } from "@/constants";
import { useAppTheme } from "@/theme";

const RANGE_OPTIONS: { value: MonitoringRange; label: string }[] = [
  { value: "1m", label: "1m" },
  { value: "5m", label: "5m" },
  { value: "15m", label: "15m" },
  { value: "60m", label: "60m" },
  { value: "6h", label: "6h" },
  { value: "24h", label: "24h" },
  { value: "7d", label: "7d" },
  { value: "30d", label: "30d" },
];

const RangeDropdown: React.FC<{
  value: MonitoringRange;
  onChange: (value: MonitoringRange) => void;
  color: string;
}> = ({ value, onChange }) => {
  const theme = useAppTheme();

  return (
    <AppSelect
      size="small"
      variant="standard"
      disableUnderline
      value={value}
      onChange={(event) => onChange(event.target.value as MonitoringRange)}
      style={{
        ["--app-select-input-font-size" as string]: "0.68rem",
        marginLeft: 0,
        color: theme.palette.text.secondary,
        fontSize: "0.75rem",
        lineHeight: theme.typography.body2.lineHeight,
      }}
    >
      {RANGE_OPTIONS.map((option) => (
        <option key={option.value} value={option.value}>
          {option.label}
        </option>
      ))}
    </AppSelect>
  );
};

const MonitorCard: React.FC<{
  title: string;
  avatarIcon: string;
  accentColor: string;
  range: MonitoringRange;
  onRangeChange: (value: MonitoringRange) => void;
  controls?: React.ReactNode;
  leftControls?: React.ReactNode;
  chart: React.ReactNode;
}> = ({
  title,
  avatarIcon,
  accentColor,
  range,
  onRangeChange,
  controls,
  leftControls,
  chart,
}) => {
  return (
    <FrostedCard
      style={{
        minHeight: cardHeight,
        display: "flex",
        flexDirection: "column",
        padding: 6,
      }}
    >
      <CardIconHeader
        icon={
          <Icon icon={avatarIcon} width={28} height={28} color={accentColor} />
        }
        title={title}
        titleSuffix={leftControls}
        right={
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: 6,
              flexShrink: 0,
            }}
          >
            <RangeDropdown
              value={range}
              onChange={onRangeChange}
              color={accentColor}
            />
            {controls}
          </div>
        }
        style={{ marginBottom: 8 }}
      />

      <div style={{ display: "flex", flexWrap: "wrap", gap: 16, flex: 1 }}>
        <div
          style={{
            flex: 1,
            minWidth: 0,
            display: "flex",
            alignItems: "stretch",
          }}
        >
          {chart}
        </div>
      </div>
    </FrostedCard>
  );
};

export default MonitorCard;
