import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { render } from "@/test/render";

import ContextGauge from "./ContextGauge";

const fill = () =>
  document.querySelector<SVGCircleElement>(".assistant__gauge-fill")!;

describe("ContextGauge", () => {
  it("shows an empty ring until usage exists", () => {
    render(<ContextGauge usage={null} />);
    expect(
      screen.getByRole("button", { name: "Context usage not reported yet" }),
    ).toHaveAttribute("aria-disabled", "true");
    expect(fill().getAttribute("stroke-dasharray")).toMatch(/^0\.00 /);
  });

  it("compacts on click", async () => {
    const onCompact = vi.fn();
    render(
      <ContextGauge
        onCompact={onCompact}
        usage={{ used: 12000, size: 200000, cost: null }}
      />,
    );
    await userEvent.click(
      screen.getByRole("button", {
        name: "12k / 200k tokens (6%) · Click to compact",
      }),
    );
    expect(onCompact).toHaveBeenCalledOnce();
  });

  it("draws the used fraction and labels it", () => {
    render(<ContextGauge usage={{ used: 12000, size: 200000, cost: null }} />);
    expect(
      screen.getByRole("button", { name: "12k / 200k tokens (6%)" }),
    ).toBeInTheDocument();
    const [dash, circumference] = fill()
      .getAttribute("stroke-dasharray")!
      .split(" ")
      .map(Number);
    expect(dash / circumference).toBeCloseTo(0.06, 2);
  });

  it("adds the cost to the label", () => {
    render(
      <ContextGauge
        usage={{
          used: 12345,
          size: 200000,
          cost: { amount: 0.42, currency: "USD" },
        }}
      />,
    );
    expect(screen.getByRole("button").getAttribute("aria-label")).toMatch(
      /^12\.3k \/ 200k tokens \(6%\) · .*0\.42/,
    );
  });

  it("clamps at 100 percent", () => {
    render(<ContextGauge usage={{ used: 300000, size: 200000, cost: null }} />);
    const [dash, circumference] = fill()
      .getAttribute("stroke-dasharray")!
      .split(" ")
      .map(Number);
    expect(dash).toBe(circumference);
    expect(screen.getByRole("button").getAttribute("aria-label")).toContain(
      "(100%)",
    );
  });

  it.each([
    [100000, "primary"],
    [140000, "warning"],
    [190000, "error"],
  ])("colours %i of 200000 tokens as %s", (used, level) => {
    render(<ContextGauge usage={{ used, size: 200000, cost: null }} />);
    expect(document.querySelector(".assistant__gauge")).toHaveClass(
      `assistant__gauge--${level}`,
    );
  });
});
