import { describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import DashboardCard, { CardBadge } from "./DashboardCard";

vi.mock("@iconify/react", () => ({
  Icon: () => <span aria-hidden="true" />,
}));

describe("DashboardCard", () => {
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
