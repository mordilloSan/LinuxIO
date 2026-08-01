import { beforeEach, describe, expect, it, vi } from "vitest";
import type { CLSMetric, INPMetric, LCPMetric } from "web-vitals";

const mocks = vi.hoisted(() => ({
  onCLS: vi.fn<(callback: (metric: CLSMetric) => void) => void>(),
  onINP: vi.fn<(callback: (metric: INPMetric) => void) => void>(),
  onLCP: vi.fn<(callback: (metric: LCPMetric) => void) => void>(),
}));

vi.mock("web-vitals", () => mocks);

beforeEach(() => {
  vi.resetModules();
});

describe("startWebVitals", () => {
  it("registers each Core Web Vital once and records compact local values", async () => {
    const { startWebVitals } = await import("./startWebVitals");
    const { getWebVitalsSnapshot, WEB_VITALS_STORAGE_KEY } =
      await import("./webVitalsStore");

    startWebVitals();
    startWebVitals();

    expect(mocks.onCLS).toHaveBeenCalledTimes(1);
    expect(mocks.onINP).toHaveBeenCalledTimes(1);
    expect(mocks.onLCP).toHaveBeenCalledTimes(1);

    const reportCLS = mocks.onCLS.mock.calls[0][0];
    reportCLS({
      delta: 0.04,
      entries: [],
      id: "cls-id",
      name: "CLS",
      navigationId: 1,
      navigationType: "navigate",
      rating: "good",
      value: 0.04,
    });

    expect(getWebVitalsSnapshot().metrics.CLS).toEqual({
      name: "CLS",
      navigationType: "navigate",
      rating: "good",
      value: 0.04,
    });
    expect(sessionStorage.getItem(WEB_VITALS_STORAGE_KEY)).toContain('"CLS"');
    expect(sessionStorage.getItem(WEB_VITALS_STORAGE_KEY)).not.toContain(
      "cls-id",
    );
  });
});
