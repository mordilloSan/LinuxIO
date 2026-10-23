import { StrictMode } from "react";
import { SmoothieChart } from "smoothie";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { act, render } from "@/test/render";

import LiveChart, { type LiveChartSeries } from "./LiveChart";
import { getLiveSeries, sampleLiveSeries } from "./liveSeriesStore";

vi.mock("@/components/charts/LiveChartHover", () => ({
  default: () => null,
}));

vi.mock("@/components/charts/useLiveSeries", () => ({
  useLiveSeries: (ids: readonly string[]) => ids.map(getLiveSeries),
}));

const format = (value: number) => `${value}!`;
const fetchHistory = async () => ({});

describe("LiveChart lifetime", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    // Keep Smoothie's real series timers; jsdom cannot draw the canvas.
    vi.spyOn(SmoothieChart.prototype, "streamTo").mockImplementation(() => {});
  });

  it.each([
    {
      name: "one series",
      series: [{ color: "primary", format, id: "test:one", value: 50 }],
    },
    {
      name: "two series",
      series: [
        { color: "rx", format, id: "test:a", label: "A", value: 100 },
        { color: "tx", format, id: "test:b", label: "B", value: 50 },
      ],
    },
  ] satisfies { name: string; series: LiveChartSeries[] }[])(
    "$name releases timers and retains history across remounts",
    ({ series }) => {
      const timestamp = Date.now();
      const buffers = series.map((entry) => getLiveSeries(entry.id));
      for (const buffer of buffers) {
        buffer.clear();
        buffer.append(timestamp, 42);
      }

      for (let mount = 0; mount < 2; mount += 1) {
        const view = render(
          <StrictMode>
            <LiveChart
              fetchHistory={fetchHistory}
              series={series}
              streamDelayMs={1000}
            />
          </StrictMode>,
          { seedConfig: false },
        );
        // One timer per series buffer plus the once-a-second sampler.
        expect(vi.getTimerCount()).toBe(series.length + 1);

        act(() => {
          vi.advanceTimersByTime(3000);
        });
        view.unmount();

        expect(vi.getTimerCount()).toBe(0);
        for (const buffer of buffers) {
          expect(sampleLiveSeries(buffer, timestamp, 0)).toBe(42);
        }
      }
    },
  );

  it("samples the latest reading into each series every second", () => {
    const buffer = getLiveSeries("test:sampled");
    buffer.clear();
    const view = render(
      <LiveChart
        fetchHistory={fetchHistory}
        series={[{ color: "rx", format, id: "test:sampled", value: 7 }]}
        streamDelayMs={1000}
      />,
      { seedConfig: false },
    );

    act(() => {
      vi.advanceTimersByTime(1000);
    });

    expect(sampleLiveSeries(buffer, Date.now(), 1000)).toBe(7);
    view.unmount();
  });

  it("renders a legend line per series when asked", () => {
    const view = render(
      <LiveChart
        fetchHistory={fetchHistory}
        legend
        series={[
          { color: "rx", format, id: "test:l1", label: "Rx", value: 3 },
          { color: "tx", format, id: "test:l2", label: "Tx", value: 4 },
        ]}
        streamDelayMs={1000}
      />,
      { seedConfig: false },
    );

    expect(view.getByText("Rx: 3!")).toBeInTheDocument();
    expect(view.getByText("Tx: 4!")).toBeInTheDocument();
    view.unmount();
  });
});
