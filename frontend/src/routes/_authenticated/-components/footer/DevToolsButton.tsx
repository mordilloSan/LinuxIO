import { Icon } from "@iconify/react";
import { memo, useState, type CSSProperties } from "react";

import { DevToolsPanel } from "@/components/dev-tools/DevToolsPanel";
import { WebVitalsFooterStats } from "@/components/dev-tools/WebVitalsFooterStats";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";
import { shadowSm } from "@/theme/constants";

const DevToolsButton = () => {
  const theme = useAppTheme();
  const [isOpen, setIsOpen] = useState(false);
  const [isWebVitalsVisible, setIsWebVitalsVisible] = useState(false);

  // Only show in development mode
  if (!import.meta.env.DEV) {
    return null;
  }

  return (
    <>
      {isWebVitalsVisible && <WebVitalsFooterStats />}
      <div style={{ position: "relative", display: "inline-flex" }}>
        <div
          className="devtools-btn"
          onClick={() => setIsOpen((prev) => !prev)}
          onKeyDown={(event) => {
            if (event.key === "Enter" || event.key === " ") {
              event.preventDefault();
              setIsOpen((prev) => !prev);
            }
          }}
          role="button"
          style={
            {
              cursor: "pointer",
              display: "flex",
              alignItems: "center",
              gap: 3,
              border: "1px solid",
              borderColor: isOpen ? theme.palette.primary.main : "transparent",
              borderRadius: 4,
              padding: 4,
              boxShadow: isOpen ? shadowSm : "none",
              whiteSpace: "nowrap",
              minWidth: 90,
              transition: "all 0.2s",
              "--devtools-hover-border": theme.palette.primary.main,
              "--devtools-hover-shadow": shadowSm,
            } as CSSProperties
          }
          tabIndex={0}
        >
          <Icon
            height={16}
            icon="mdi:wrench"
            style={{ color: theme.palette.primary.main }}
            width={16}
          />
          <AppTypography color="text.secondary" variant="caption">
            Dev Tools
          </AppTypography>
        </div>
      </div>
      <DevToolsPanel
        isOpen={isOpen}
        isWebVitalsVisible={isWebVitalsVisible}
        onClose={() => setIsOpen(false)}
        onToggleWebVitals={() => setIsWebVitalsVisible((visible) => !visible)}
      />
    </>
  );
};

export default memo(DevToolsButton);
