import { describe, expect, it } from "vitest";

import { topologyLive } from "@/test/dockerTopologyFixture";
import { render, screen } from "@/test/render";

import { formatHtopUptime, HtopHeader } from "./HtopTab";

const GiB = 1024 ** 3;

const base = topologyLive(0);
const live = {
  ...base,
  uptime_seconds: 3 * 86_400 + 4 * 3_600 + 12 * 60 + 5,
  cpu: {
    ...base.cpu,
    per_core_percent: [34.2, 150, 0],
    load_average: [0.42, 0.381, 0.3],
  },
  memory: {
    ...base.memory,
    total_bytes: 16 * GiB,
    used_bytes: 12 * GiB,
    buffers_bytes: 1 * GiB,
    cached_bytes: 8 * GiB,
    swap_total_bytes: 2 * GiB,
    swap_free_bytes: 1.5 * GiB,
  },
};

const widths = (meter: HTMLElement) =>
  [...meter.querySelectorAll<HTMLElement>(".htop-meter__fill")].map(
    (fill) => fill.style.width,
  );

describe("HtopHeader", () => {
  it("draws one meter per core, clamped to the track", () => {
    render(
      <HtopHeader
        count={{ total: 312, running: 2, sleeping: 300, thread: 1024 }}
        live={live}
      />,
    );

    expect(widths(screen.getByRole("meter", { name: "0" }))).toEqual(["34.2%"]);
    expect(widths(screen.getByRole("meter", { name: "1" }))).toEqual(["100%"]);
    expect(screen.getByRole("meter", { name: "2" })).toHaveAttribute(
      "aria-valuetext",
      "0.0%",
    );
    expect(screen.getByText("312, 1024 thr; 2 running")).toBeInTheDocument();
    expect(screen.getByText("0.42 0.38 0.30")).toBeInTheDocument();
    expect(screen.getByText("3 days, 04:12:05")).toBeInTheDocument();
  });

  it("stacks memory segments without overflowing the total", () => {
    render(<HtopHeader live={live} />);

    // 75% used leaves room for 6.25% buffers and only 18.75% of the cache.
    expect(widths(screen.getByRole("meter", { name: "Mem" }))).toEqual([
      "75%",
      "6.25%",
      "18.75%",
    ]);
    expect(widths(screen.getByRole("meter", { name: "Swp" }))).toEqual(["25%"]);
    expect(screen.getByText("—")).toBeInTheDocument();
  });
});

describe("formatHtopUptime", () => {
  it("prints htop's clock, with days only when there are some", () => {
    expect(formatHtopUptime(59)).toBe("00:00:59");
    expect(formatHtopUptime(86_400 + 61)).toBe("1 day, 00:01:01");
  });
});
