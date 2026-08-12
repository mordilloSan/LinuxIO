/**
 * Row shape shared by the live-chart tooltips. Every live chart renders its
 * tooltip through LiveChartHover, which owns the markup; smoothie's own
 * tooltip is disabled so the crosshair can stay synchronized across charts.
 */
export interface LiveTooltipRow {
  color: string;
  value: string;
  label?: string;
}
