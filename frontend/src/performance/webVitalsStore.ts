import type { Metric } from "web-vitals";

export const WEB_VITAL_NAMES = ["CLS", "INP", "LCP"] as const;
export const WEB_VITALS_STORAGE_KEY = "linuxio.webVitals";

export type WebVitalName = (typeof WEB_VITAL_NAMES)[number];

export interface WebVitalReading {
  name: WebVitalName;
  navigationType: Metric["navigationType"];
  rating: Metric["rating"];
  value: number;
}

export interface WebVitalsSnapshot {
  metrics: Partial<Record<WebVitalName, WebVitalReading>>;
  navigationStart: number;
}

type CoreWebVitalMetric = Metric & { name: WebVitalName };

const listeners = new Set<() => void>();

function currentNavigationStart() {
  return Math.round(globalThis.performance?.timeOrigin ?? Date.now());
}

let snapshot: WebVitalsSnapshot = {
  metrics: {},
  navigationStart: currentNavigationStart(),
};

function persistSnapshot() {
  try {
    sessionStorage.setItem(WEB_VITALS_STORAGE_KEY, JSON.stringify(snapshot));
  } catch {
    // Metrics are diagnostic-only; restricted storage must not affect the app.
  }
}

function publishSnapshot(next: WebVitalsSnapshot) {
  snapshot = next;
  persistSnapshot();
  for (const listener of listeners) listener();
}

export function resetWebVitalsSnapshot() {
  publishSnapshot({
    metrics: {},
    navigationStart: currentNavigationStart(),
  });
}

export function recordWebVital(metric: CoreWebVitalMetric) {
  publishSnapshot({
    ...snapshot,
    metrics: {
      ...snapshot.metrics,
      [metric.name]: {
        name: metric.name,
        navigationType: metric.navigationType,
        rating: metric.rating,
        value: metric.value,
      },
    },
  });
}

export function getWebVitalsSnapshot() {
  return snapshot;
}

export function subscribeToWebVitals(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}
