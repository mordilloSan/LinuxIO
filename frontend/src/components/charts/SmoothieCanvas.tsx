import React, { useEffect, useImperativeHandle, useRef } from "react";
import type { SmoothieChart } from "smoothie";

type SmoothieChartWithTooltip = SmoothieChart & {
  tooltipEl?: HTMLElement;
};

interface SmoothieCanvasProps extends React.ComponentPropsWithoutRef<"canvas"> {
  chartRef: React.RefObject<SmoothieChart | null>;
}

const SmoothieCanvas = React.forwardRef<HTMLCanvasElement, SmoothieCanvasProps>(
  ({ chartRef, ...props }, forwardedRef) => {
    const canvasRef = useRef<HTMLCanvasElement>(null);
    const tooltipRef = useRef<HTMLElement | null>(null);

    useImperativeHandle(forwardedRef, () => canvasRef.current!);

    useEffect(() => {
      const canvas = canvasRef.current;
      if (!canvas) return;

      const onMove = (evt: MouseEvent) => {
        const chart = chartRef.current as SmoothieChartWithTooltip | null;
        if (chart?.tooltipEl && chart.tooltipEl !== tooltipRef.current) {
          tooltipRef.current = chart.tooltipEl;
        }

        const tooltip = tooltipRef.current;
        if (!tooltip || tooltip.style.display === "none") return;

        const canvasRect = canvas.getBoundingClientRect();
        const mouseRelX = evt.clientX - canvasRect.left;
        if (mouseRelX > canvasRect.width / 2) {
          tooltip.style.left = `${Math.round(evt.pageX) - tooltip.offsetWidth - 10}px`;
        }
      };

      canvas.addEventListener("mousemove", onMove);
      return () => {
        canvas.removeEventListener("mousemove", onMove);
      };
    }, [chartRef]);

    return <canvas ref={canvasRef} {...props} />;
  },
);

SmoothieCanvas.displayName = "SmoothieCanvas";

export default SmoothieCanvas;
