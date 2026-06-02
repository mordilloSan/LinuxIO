import React from "react";

import AppTooltip from "@/components/ui/AppTooltip";

interface StatusDotProps {
  absolute?: boolean;
  color: string;
  size?: number;
  style?: React.CSSProperties;
  tooltip?: string;
}

const StatusDot: React.FC<StatusDotProps> = ({
  color,
  size = 10,
  absolute = false,
  tooltip,
  style,
}) => {
  const dot = (
    <span
      style={{
        display: absolute ? undefined : "inline-block",
        width: size,
        height: size,
        borderRadius: "50%",
        backgroundColor: color,
        flexShrink: 0,
        ...(absolute && {
          position: "absolute",
          top: 18,
          right: 8,
          cursor: "default",
        }),
        ...style,
      }}
    />
  );

  if (tooltip) {
    return (
      <AppTooltip arrow title={tooltip}>
        {dot}
      </AppTooltip>
    );
  }

  return dot;
};

export default StatusDot;
