import type { CSSProperties, ReactNode } from "react";

import InfoRow from "@/components/ui/InfoRow";

interface DashboardStatRow {
  label: string;
  onEdit?: () => void;
  rowStyle?: CSSProperties;
  value: ReactNode;
  valueStyle?: CSSProperties;
  valueTitle?: string;
}

interface DashboardStatRowsProps {
  containerStyle?: CSSProperties;
  rows: readonly DashboardStatRow[];
}

const DashboardStatRows = ({
  containerStyle,
  rows,
}: DashboardStatRowsProps) => (
  <div
    style={{
      display: "flex",
      flexDirection: "column",
      alignSelf: "flex-start",
      width: "fit-content",
      ...containerStyle,
    }}
  >
    {rows.map(({ label, onEdit, rowStyle, value, valueStyle, valueTitle }) => (
      <InfoRow
        align="start"
        key={label}
        label={label}
        onEdit={onEdit}
        rowStyle={rowStyle}
        valueStyle={valueStyle}
        valueTitle={valueTitle}
        variant="list"
      >
        {value}
      </InfoRow>
    ))}
  </div>
);

export default DashboardStatRows;
