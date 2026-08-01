import { describe, expect, it } from "vitest";

import { render, screen } from "@/test/render";

import DashboardCardSkeleton from "./DashboardCardSkeleton";

describe("DashboardCardSkeleton", () => {
  it("preserves split DashboardCard chrome without a spinner", () => {
    const { container } = render(
      <DashboardCardSkeleton layout="split" title="System Health" />,
    );

    const card = screen.getByLabelText("Loading System Health card");
    expect(card).toHaveAttribute("aria-busy", "true");
    expect(card).toHaveClass("dashboard-card-skeleton");
    expect(card).toHaveStyle({
      display: "flex",
      flexDirection: "column",
      minHeight: "220px",
    });
    expect(container.querySelector(".page-loader")).not.toBeInTheDocument();

    expect(container.querySelector(".app-typo--h5")).toBeInTheDocument();
    expect(container.querySelector(".app-skeleton--circular")).toHaveStyle({
      height: "38px",
      width: "38px",
    });
    expect(
      container.querySelector(".dashboard-card-skeleton__chart"),
    ).toHaveStyle({ height: "90px", width: "100%" });
  });

  it("omits the chart placeholder for stats-only cards", () => {
    const { container } = render(
      <DashboardCardSkeleton layout="stats" title="System Overview" />,
    );

    expect(
      container.querySelector(".dashboard-card-skeleton__chart"),
    ).not.toBeInTheDocument();
    expect(container.querySelectorAll(".app-typo--body2")).toHaveLength(3);
  });
});
