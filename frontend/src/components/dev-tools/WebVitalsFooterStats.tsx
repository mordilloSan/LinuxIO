import { useSyncExternalStore } from "react";

import {
  getWebVitalsSnapshot,
  subscribeToWebVitals,
  WEB_VITAL_NAMES,
  type WebVitalName,
} from "@/performance/webVitalsStore";
import { useAppTheme } from "@/theme";

function formatWebVital(name: WebVitalName, value: number) {
  return name === "CLS" ? value.toFixed(3) : `${Math.round(value)} ms`;
}

export function WebVitalsFooterStats() {
  const theme = useAppTheme();
  const webVitals = useSyncExternalStore(
    subscribeToWebVitals,
    getWebVitalsSnapshot,
    getWebVitalsSnapshot,
  );

  return (
    <div
      aria-label="Web Vitals"
      style={{
        alignItems: "center",
        display: "flex",
        fontFamily: "monospace",
        fontSize: 12,
        gap: 8,
        whiteSpace: "nowrap",
      }}
      title="Local page-load metrics. INP needs an interaction; final values may update when the tab is hidden."
    >
      {WEB_VITAL_NAMES.map((name) => {
        const metric = webVitals.metrics[name];
        const color = metric
          ? metric.rating === "good"
            ? theme.palette.success.main
            : metric.rating === "needs-improvement"
              ? theme.palette.warning.main
              : theme.palette.error.main
          : theme.palette.text.secondary;

        return (
          <span key={name}>
            {name}{" "}
            <span style={{ color }}>
              {metric ? formatWebVital(name, metric.value) : "Pending"}
            </span>
          </span>
        );
      })}
    </div>
  );
}
