import { Icon } from "@iconify/react";
import { memo, useState, type CSSProperties } from "react";

import { DevToolsPanel } from "@/components/dev-tools/DevToolsPanel";
import { WebVitalsFooterStats } from "@/components/dev-tools/WebVitalsFooterStats";
import AppButton from "@/components/ui/AppButton";
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
        <AppButton
          aria-expanded={isOpen}
          aria-label="Toggle developer tools"
          className="devtools-btn"
          keepTextOnMobile
          onClick={() => setIsOpen((prev) => !prev)}
          startIcon={
            <Icon
              height={16}
              icon="mdi:wrench"
              style={{ color: theme.palette.primary.main }}
              width={16}
            />
          }
          style={
            {
              gap: 3,
              border: "1px solid",
              borderColor: isOpen ? theme.palette.primary.main : "transparent",
              borderRadius: 4,
              padding: 4,
              boxShadow: isOpen ? shadowSm : "none",
              whiteSpace: "nowrap",
              minWidth: 90,
              transition:
                "background-color 0.2s, border-color 0.2s, box-shadow 0.2s",
              "--devtools-hover-border": theme.palette.primary.main,
              "--devtools-hover-shadow": shadowSm,
            } as CSSProperties
          }
          variant="text"
        >
          <AppTypography color="text.secondary" variant="caption">
            Dev Tools
          </AppTypography>
        </AppButton>
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
