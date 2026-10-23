import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import DashboardCard, { CardBadge } from "./DashboardCard";

const mocks = vi.hoisted(() => ({ typographyRender: vi.fn() }));

vi.mock("@iconify/react", () => ({
  Icon: () => <span aria-hidden="true" />,
}));

vi.mock("@/components/ui/AppTypography", () => ({
  default: ({ children }: { children?: ReactNode }) => {
    mocks.typographyRender(children);
    return <span>{children}</span>;
  },
}));

describe("DashboardCard", () => {
  it("does not rerender its static title when live content changes", () => {
    mocks.typographyRender.mockClear();
    const { rerender } = render(
      <DashboardCard
        avatarIcon="mdi:server"
        stats={<span>Stats 1</span>}
        title="System"
      />,
    );

    expect(mocks.typographyRender).toHaveBeenCalledTimes(1);

    rerender(
      <DashboardCard
        avatarIcon="mdi:server"
        stats={<span>Stats 2</span>}
        title="System"
      />,
    );

    expect(screen.getByText("Stats 2")).toBeInTheDocument();
    expect(mocks.typographyRender).toHaveBeenCalledTimes(1);
  });

  it("uses a named menu trigger for badge options", async () => {
    const onSelect = vi.fn();
    const { user } = render(
      <DashboardCard
        avatarIcon="mdi:server"
        headerExtras={
          <CardBadge
            icon="mdi:thermometer"
            onSelect={onSelect}
            options={[{ label: "Celsius", value: "c" }]}
            text="22 °C"
          />
        }
        stats={<span>Stats</span>}
        title="System"
      />,
    );

    const trigger = screen.getByRole("button", { name: "Select 22 °C" });
    expect(trigger).toHaveAttribute("aria-haspopup", "menu");
    expect(trigger).toHaveAttribute("aria-expanded", "false");

    await user.click(trigger);

    expect(trigger).toHaveAttribute("aria-expanded", "true");
    await user.click(screen.getByRole("menuitem", { name: "Celsius" }));
    expect(onSelect).toHaveBeenCalledWith("c");
  });
});
