import { describe, expect, it } from "vitest";

import type {
  MonitoringContainerHistoryPoint,
  MonitoringContainerSample,
} from "@/api";

import {
  containerSamples,
  containerStackSeries,
  hasBlockIO,
} from "./containerHistory";

const sample = (
  id: string,
  name: string,
  values: Partial<MonitoringContainerSample> = {},
): MonitoringContainerSample => ({
  id,
  name,
  cpu_percent: 0,
  memory_mb: 0,
  sent_bytes_per_sec: 0,
  recv_bytes_per_sec: 0,
  ...values,
});

const point = (
  capturedAtMs: number,
  containers: MonitoringContainerSample[],
): MonitoringContainerHistoryPoint => ({
  captured_at_ms: capturedAtMs,
  containers,
});

describe("containerStackSeries", () => {
  const history = [
    point(1_000, [
      sample("aaa", "web", { cpu_percent: 1 }),
      sample("bbb", "db", { cpu_percent: 9 }),
    ]),
    // "web" was restarted out of this sample; "db" kept running.
    point(2_000, [sample("bbb", "db", { cpu_percent: 5 })]),
    point(3_000, [
      sample("aaa", "web", { cpu_percent: 3 }),
      sample("bbb", "db", { cpu_percent: 5 }),
    ]),
  ];

  it("orders bands by total usage over the window", () => {
    expect(
      containerStackSeries(history, "cpu", "").map((s) => s.label),
    ).toEqual(["db", "web"]);
  });

  it("gives each band a distinct hue", () => {
    const colors = containerStackSeries(history, "cpu", "").map((s) => s.color);
    expect(colors).toEqual(["hsl(0, 70%, 55%)", "hsl(180, 70%, 55%)"]);
  });

  it("contributes nothing for a sample the container is absent from", () => {
    const web = containerStackSeries(history, "cpu", "").find(
      (series) => series.label === "web",
    );
    expect(web?.points).toEqual([
      { t: 1_000, v: 1 },
      { t: 2_000, v: 0 },
      { t: 3_000, v: 3 },
    ]);
  });

  it("dims the containers a filter excludes without dropping them", () => {
    const series = containerStackSeries(history, "cpu", "web");
    expect(series.map((s) => [s.label, s.dimmed ?? false])).toEqual([
      ["db", true],
      ["web", false],
    ]);
    // Still stacked, so the top of the stack stays the true total.
    expect(series).toHaveLength(2);
  });

  it("matches any filter term", () => {
    const series = containerStackSeries(history, "cpu", "we db");
    expect(series.every((s) => s.dimmed !== true)).toBe(true);
  });

  it("reports memory in bytes", () => {
    const series = containerStackSeries(
      [point(1_000, [sample("aaa", "web", { memory_mb: 512 })])],
      "memory",
      "",
    );
    expect(series[0].points[0].v).toBe(512 * 1024 * 1024);
  });

  it("has no bands without history", () => {
    expect(containerStackSeries(undefined, "cpu", "")).toEqual([]);
  });
});

describe("containerSamples", () => {
  const fullId =
    "aaa1112223334445556667778889990001112223334445556667778889990000";

  it("matches the agent's short id against the full Docker id", () => {
    const samples = containerSamples(
      [point(1_000, [sample("aaa111", "web", { cpu_percent: 2 })])],
      fullId,
      "web",
    );
    expect(samples).toEqual([
      { t: 1_000, sample: expect.objectContaining({ cpu_percent: 2 }) },
    ]);
  });

  it("falls back to the name when a rollup dropped the id", () => {
    const samples = containerSamples(
      [point(1_000, [sample("", "web", { cpu_percent: 2 })])],
      fullId,
      "web",
    );
    expect(samples).toHaveLength(1);
  });

  it("drops the points taken while the container was not running", () => {
    const samples = containerSamples(
      [
        point(1_000, [sample("aaa111", "web")]),
        point(2_000, [sample("bbb222", "db")]),
      ],
      fullId,
      "web",
    );
    expect(samples.map(({ t }) => t)).toEqual([1_000]);
  });
});

describe("hasBlockIO", () => {
  it("is false when no sample carries block I/O", () => {
    expect(hasBlockIO([{ sample: sample("aaa", "web") }])).toBe(false);
  });

  it("is true once one sample carries it", () => {
    expect(
      hasBlockIO([
        { sample: sample("aaa", "web") },
        { sample: sample("aaa", "web", { read_bytes_per_sec: 0 }) },
      ]),
    ).toBe(true);
  });
});
