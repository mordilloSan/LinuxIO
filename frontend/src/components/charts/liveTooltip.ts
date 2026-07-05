import type { SmoothieChart } from "smoothie";

import { formatChartClockWithSeconds } from "@/components/charts/timeFormat";

import "@/components/charts/chart-tooltip.css";

export interface LiveTooltipRow {
  color: string;
  value: string;
  label?: string;
}

/**
 * Smoothie anchors its tooltip's top-left corner on the pointer with no
 * built-in flipping. Shift it to the other side of the crosshair when the
 * cursor is in the right half of the canvas, like the hardware history
 * charts. Call from tooltipFormatter, which runs on every tooltip update;
 * smoothie only ever writes top/left, so the transform is ours.
 */
export function flipSmoothieTooltip(chart: SmoothieChart | null): void {
  if (!chart) return;
  const internals = chart as unknown as {
    mouseX?: number;
    clientWidth?: number;
    getTooltipEl?: () => HTMLElement;
  };
  if (!internals.getTooltipEl || internals.mouseX === undefined) return;
  const width = internals.clientWidth ?? 0;
  internals.getTooltipEl().style.transform =
    width > 0 && internals.mouseX > width / 2
      ? "translateX(calc(-100% - 12px))"
      : "translateX(12px)";
}

/**
 * Tooltip HTML for the smoothie-rendered tooltips, laid out like the
 * HistoryAreaChart tooltip: timestamp header, then one row per series with a
 * color chip, bold value and secondary label. Only feed it app-generated
 * strings — smoothie injects the result via innerHTML.
 */
export function liveTooltipHTML(
  timestamp: number,
  rows: LiveTooltipRow[],
): string {
  const header = `<div class="chart-tooltip-time">${formatChartClockWithSeconds(timestamp)}</div>`;
  const body = rows
    .map(
      (row) =>
        `<div class="chart-tooltip-row">` +
        `<span class="chart-tooltip-chip" style="background:${row.color}"></span>` +
        `<span class="chart-tooltip-value">${row.value}</span>` +
        (row.label
          ? `<span class="chart-tooltip-label">${row.label}</span>`
          : "") +
        `</div>`,
    )
    .join("");
  return header + body;
}
