import type { TimeSeries } from "smoothie";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { render, waitFor } from "@/test/render";

import NetworkTrafficGraph, { centerZeroRange } from "./NetworkTrafficGraph";

const mocks = vi.hoisted(() => ({
  addTimeSeries: vi.fn(),
  options: undefined as undefined | Record<string, unknown>,
  rowsAt: undefined as undefined | ((timestamp: number) => unknown[]),
  stop: vi.fn(),
  streamTo: vi.fn(),
}));

vi.mock("smoothie", () => ({
  SmoothieChart: class {
    addTimeSeries = mocks.addTimeSeries;
    stop = mocks.stop;
    streamTo = mocks.streamTo;
    constructor(options: Record<string, unknown>) {
      mocks.options = options;
    }
  },
}));

vi.mock("@/components/charts/LiveChartHover", () => ({
  default: ({ rowsAt }: { rowsAt: (timestamp: number) => unknown[] }) => {
    mocks.rowsAt = rowsAt;
    return null;
  },
}));

describe("NetworkTrafficGraph", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.options = undefined;
    mocks.rowsAt = undefined;
  });

  it("renders sent positive and received traffic negative on the same chart", async () => {
    const timestamp = Date.now();
    const sent = { data: [[timestamp, 4]] } as unknown as TimeSeries;
    const received = { data: [[timestamp, -3]] } as unknown as TimeSeries;

    render(
      <NetworkTrafficGraph
        series={[
          { color: "#00ff00", label: "Sent", series: sent },
          { color: "#ff0000", label: "Received", series: received },
        ]}
      />,
    );

    await waitFor(() => {
      expect(mocks.addTimeSeries).toHaveBeenCalledTimes(2);
    });
    expect(mocks.addTimeSeries).toHaveBeenNthCalledWith(
      1,
      sent,
      expect.objectContaining({ strokeStyle: "#00ff00" }),
    );
    expect(mocks.addTimeSeries).toHaveBeenNthCalledWith(
      2,
      received,
      expect.objectContaining({ strokeStyle: "#ff0000" }),
    );
    expect(mocks.rowsAt?.(timestamp)).toEqual([
      { color: "#00ff00", label: "Sent", value: "+4.0 kB/s" },
      { color: "#ff0000", label: "Received", value: "−3.0 kB/s" },
    ]);
    // Filling to the canvas floor would tint the received half with sent
    // traffic, so the mirrored series are drawn as lines only.
    expect(mocks.addTimeSeries.mock.calls[0][1]).not.toHaveProperty(
      "fillStyle",
    );
  });

  it("keeps the zero line centred", async () => {
    render(
      <NetworkTrafficGraph
        series={[
          {
            color: "#00ff00",
            label: "Sent",
            series: { data: [] } as unknown as TimeSeries,
          },
        ]}
      />,
    );

    await waitFor(() => {
      expect(mocks.options).toBeDefined();
    });
    expect(mocks.options?.yRangeFunction).toBe(centerZeroRange);
  });
});

describe("centerZeroRange", () => {
  it("mirrors the busier direction onto the quieter one", () => {
    expect(centerZeroRange({ max: 20, min: -200 })).toEqual({
      max: 200,
      min: -200,
    });
    expect(centerZeroRange({ max: 200, min: -20 })).toEqual({
      max: 200,
      min: -200,
    });
  });

  it("holds a floor for an idle interface and an empty chart", () => {
    expect(centerZeroRange({ max: 0, min: 0 })).toEqual({ max: 1, min: -1 });
    expect(centerZeroRange({ max: Number.NaN, min: Number.NaN })).toEqual({
      max: 1,
      min: -1,
    });
  });
});
