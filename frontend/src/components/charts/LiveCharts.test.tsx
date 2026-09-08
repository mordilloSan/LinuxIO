import { StrictMode } from "react";
import { SmoothieChart } from "smoothie";
import { beforeEach, describe, expect, it, vi } from "vitest";

import DriveGraph from "@/routes/_authenticated/-dashboard/DriveGraph";
import NetworkGraph from "@/routes/_authenticated/-dashboard/NetworkGraph";
import ProcessorGraph from "@/routes/_authenticated/-dashboard/ProcessorGraph";
import NetworkTrafficGraph from "@/routes/_authenticated/network/-components/NetworkTrafficGraph";
import { act, render } from "@/test/render";

import { getLiveSeries, sampleLiveSeries } from "./liveSeriesStore";

vi.mock("@/components/charts/LiveChartHover", () => ({
  default: () => null,
}));

vi.mock("@/components/charts/useLiveSeries", () => ({
  useLiveSeries: (ids: readonly string[]) => ids.map(getLiveSeries),
}));

describe("live chart lifetime", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    // Keep Smoothie's real series timers; jsdom cannot draw the canvas.
    vi.spyOn(SmoothieChart.prototype, "streamTo").mockImplementation(() => {});
  });

  it.each([
    {
      name: "CPU",
      ids: ["cpu:usage"],
      samplingTimers: 1,
      chart: () => <ProcessorGraph usage={50} />,
    },
    {
      name: "disk",
      ids: ["disk:read", "disk:write"],
      samplingTimers: 1,
      chart: () => <DriveGraph readBytesPerSec={100} writeBytesPerSec={50} />,
    },
    {
      name: "dashboard network",
      ids: ["network:rx:test0", "network:tx:test0"],
      samplingTimers: 1,
      chart: () => <NetworkGraph interfaceName="test0" rx={100} tx={50} />,
    },
    {
      name: "network detail",
      ids: ["network:rx:inbound:test0", "network:tx:test0"],
      samplingTimers: 0,
      chart: () => (
        <NetworkTrafficGraph
          series={[
            {
              colorKey: "rx",
              label: "Received",
              series: getLiveSeries("network:rx:inbound:test0"),
            },
            {
              colorKey: "tx",
              label: "Sent",
              series: getLiveSeries("network:tx:test0"),
            },
          ]}
        />
      ),
    },
  ])(
    "$name releases timers and retains history across remounts",
    ({ chart, ids, samplingTimers }) => {
      const timestamp = Date.now();
      const series = ids.map(getLiveSeries);
      for (const entry of series) {
        entry.clear();
        entry.append(timestamp, 42);
      }

      for (let mount = 0; mount < 2; mount += 1) {
        const view = render(<StrictMode>{chart()}</StrictMode>, {
          seedConfig: false,
        });
        expect(vi.getTimerCount()).toBe(series.length + samplingTimers);

        act(() => {
          vi.advanceTimersByTime(3000);
        });
        view.unmount();

        expect(vi.getTimerCount()).toBe(0);
        for (const entry of series) {
          expect(sampleLiveSeries(entry, timestamp, 0)).toBe(42);
        }
      }
    },
  );
});
