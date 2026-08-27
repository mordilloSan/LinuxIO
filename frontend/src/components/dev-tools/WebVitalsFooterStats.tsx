import { useSyncExternalStore } from "react";

import AppTypography from "@/components/ui/AppTypography";
import {
  getWebVitalsSnapshot,
  subscribeToWebVitals,
  WEB_VITAL_NAMES,
  type WebVitalName,
} from "@/performance/webVitalsStore";

function formatWebVital(name: WebVitalName, value: number) {
  return name === "CLS" ? value.toFixed(3) : `${Math.round(value)} ms`;
}

export function WebVitalsFooterStats() {
  const webVitals = useSyncExternalStore(
    subscribeToWebVitals,
    getWebVitalsSnapshot,
    getWebVitalsSnapshot,
  );

  return (
    <AppTypography
      aria-label="Web Vitals"
      component="div"
      style={{
        alignItems: "center",
        display: "flex",
        fontFamily: "var(--app-font-mono)",
        gap: 8,
        whiteSpace: "nowrap",
      }}
      title="Local page-load metrics. INP needs an interaction; final values may update when the tab is hidden."
      variant="body2"
    >
      {WEB_VITAL_NAMES.map((name) => {
        const metric = webVitals.metrics[name];
        const color = metric
          ? metric.rating === "good"
            ? "var(--app-palette-success-main)"
            : metric.rating === "needs-improvement"
              ? "var(--app-palette-warning-main)"
              : "var(--app-palette-error-main)"
          : "var(--app-palette-text-secondary)";

        return (
          <span key={name}>
            {name}{" "}
            <span style={{ color }}>
              {metric ? formatWebVital(name, metric.value) : "Pending"}
            </span>
          </span>
        );
      })}
    </AppTypography>
  );
}
