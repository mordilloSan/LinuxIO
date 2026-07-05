import type { SmoothieChart } from "smoothie";

import "@/components/charts/live-tooltip.css";

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

const timeFormatter = new Intl.DateTimeFormat(undefined, {
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
});

export function formatLiveTooltipTime(timestamp: number): string {
  return timeFormatter.format(timestamp);
}

/**
 * Tooltip HTML for the smoothie live charts, laid out like the
 * HistoryAreaChart tooltip: timestamp header, then one row per series with a
 * color chip, bold value and secondary label. Only feed it app-generated
 * strings — smoothie injects the result via innerHTML.
 */
export function liveTooltipHTML(
  timestamp: number,
  rows: LiveTooltipRow[],
): string {
  const header = `<div class="live-tooltip-time">${timeFormatter.format(timestamp)}</div>`;
  const body = rows
    .map(
      (row) =>
        `<div class="live-tooltip-row">` +
        `<span class="live-tooltip-chip" style="background:${row.color}"></span>` +
        `<span class="live-tooltip-value">${row.value}</span>` +
        (row.label
          ? `<span class="live-tooltip-label">${row.label}</span>`
          : "") +
        `</div>`,
    )
    .join("");
  return header + body;
}
