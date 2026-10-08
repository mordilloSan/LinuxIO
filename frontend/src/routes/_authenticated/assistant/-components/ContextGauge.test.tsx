import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { render } from "@/test/render";

import ContextGauge from "./ContextGauge";

const fill = () =>
  document.querySelector<SVGCircleElement>(".assistant__gauge-fill")!;

describe("ContextGauge", () => {
  it("renders nothing until usage exists", () => {
    render(<ContextGauge usage={null} />);
    expect(screen.queryByRole("img")).toBeNull();
  });

  it("draws the used fraction and labels it", () => {
    render(<ContextGauge usage={{ used: 12000, size: 200000, cost: null }} />);
    expect(
      screen.getByRole("img", { name: "12k / 200k tokens (6%)" }),
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
    expect(screen.getByRole("img").getAttribute("aria-label")).toMatch(
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
    expect(screen.getByRole("img").getAttribute("aria-label")).toContain(
      "(100%)",
    );
  });

  it.each([
    [100000, "primary"],
    [140000, "warning"],
    [190000, "error"],
  ])("colours %i of 200000 tokens as %s", (used, level) => {
    render(<ContextGauge usage={{ used, size: 200000, cost: null }} />);
    expect(screen.getByRole("img")).toHaveClass(`assistant__gauge--${level}`);
  });
});
