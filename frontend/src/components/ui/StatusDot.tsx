import React from "react";

import AppTooltip from "@/components/ui/AppTooltip";

interface StatusDotProps {
  color: string;
  size?: number;
  absolute?: boolean;
  tooltip?: string;
  style?: React.CSSProperties;
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
      <AppTooltip title={tooltip} arrow>
        {dot}
      </AppTooltip>
    );
  }

  return dot;
};

export default StatusDot;
