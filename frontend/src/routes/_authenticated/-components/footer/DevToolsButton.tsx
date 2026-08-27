import { Icon } from "@iconify/react";
import { memo, useEffect, useState, type CSSProperties } from "react";

import { DevToolsPanel } from "@/components/dev-tools/DevToolsPanel";
import { WebVitalsFooterStats } from "@/components/dev-tools/WebVitalsFooterStats";
import AppButton from "@/components/ui/AppButton";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { shadowSm } from "@/theme/constants";
import {
  readPersistedState,
  writePersistedState,
} from "@/utils/persistedState";

// The footer Web Vitals readout is a per-browser preference, so it survives a
// reload through localStorage rather than a config round-trip.
const WEB_VITALS_STORAGE_KEY = "linuxio.devtoolsWebVitals";

const isBoolean = (value: unknown): value is boolean =>
  typeof value === "boolean";

const DevToolsButton = () => {
  const [isOpen, setIsOpen] = useState(false);
  const [isWebVitalsVisible, setIsWebVitalsVisible] = useState(
    () => readPersistedState(WEB_VITALS_STORAGE_KEY, isBoolean) ?? false,
  );

  useEffect(() => {
    writePersistedState(WEB_VITALS_STORAGE_KEY, isWebVitalsVisible);
  }, [isWebVitalsVisible]);

  // Only show in development mode
  if (!import.meta.env.DEV) {
    return null;
  }

  return (
    <>
      {isWebVitalsVisible && <WebVitalsFooterStats />}
      <div style={{ position: "relative", display: "inline-flex" }}>
        <AppTooltip placement="top" title="Dev Tools">
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
                style={{ color: "var(--app-palette-primary-main)" }}
                width={16}
              />
            }
            style={
              {
                gap: 3,
                border: "1px solid",
                borderColor: isOpen
                  ? "var(--app-palette-primary-main)"
                  : "transparent",
                borderRadius: 4,
                padding: 4,
                boxShadow: isOpen ? shadowSm : "none",
                whiteSpace: "nowrap",
                minWidth: 90,
                transition:
                  "background-color 0.2s, border-color 0.2s, box-shadow 0.2s",
                "--devtools-hover-border": "var(--app-palette-primary-main)",
                "--devtools-hover-shadow": shadowSm,
              } as CSSProperties
            }
            variant="text"
          >
            <AppTypography color="text.secondary" variant="caption">
              Dev Tools
            </AppTypography>
          </AppButton>
        </AppTooltip>
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
