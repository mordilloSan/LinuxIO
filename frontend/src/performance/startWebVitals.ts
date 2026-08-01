import { onCLS, onINP, onLCP } from "web-vitals";

import { recordWebVital, resetWebVitalsSnapshot } from "./webVitalsStore";

let started = false;

/**
 * Measure browser-local Core Web Vitals for development diagnostics without
 * sending telemetry anywhere. The current page snapshot is available in the
 * development panel and in sessionStorage under `linuxio.webVitals`.
 */
export function startWebVitals() {
  if (started) return;
  started = true;
  resetWebVitalsSnapshot();

  const options = { reportAllChanges: true };
  onCLS(recordWebVital, options);
  onINP(recordWebVital, options);
  onLCP(recordWebVital, options);
}
