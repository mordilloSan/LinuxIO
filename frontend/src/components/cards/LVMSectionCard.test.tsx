import { describe, expect, it, vi } from "vitest";

import LVMSectionCard from "@/components/cards/LVMSectionCard";
import { fireEvent, render, screen } from "@/test/render";

vi.mock("@iconify/react", () => ({
  Icon: () => <span aria-hidden="true" />,
}));

describe("LVMSectionCard", () => {
  it("exposes disclosure semantics and toggles its panel", () => {
    const onToggle = vi.fn();
    const { container, rerender } = render(
      <LVMSectionCard
        accent="#fff"
        count={2}
        expanded={false}
        icon="mdi:database"
        onToggle={onToggle}
        subtitle="Logical volumes"
        title="LVM"
      >
        <div>Panel content</div>
      </LVMSectionCard>,
    );

    const trigger = screen.getByRole("button", { name: /LVM/ });
    const panelId = trigger.getAttribute("aria-controls");
    expect(panelId).toBeTruthy();
    expect(trigger).toHaveAttribute("aria-expanded", "false");
    expect(
      container.ownerDocument.getElementById(panelId ?? ""),
    ).toBeInTheDocument();

    fireEvent.click(trigger);
    expect(onToggle).toHaveBeenCalledTimes(1);

    rerender(
      <LVMSectionCard
        accent="#fff"
        count={2}
        expanded
        icon="mdi:database"
        onToggle={onToggle}
        subtitle="Logical volumes"
        title="LVM"
      >
        <div>Panel content</div>
      </LVMSectionCard>,
    );

    expect(trigger).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByText("Panel content")).toBeInTheDocument();
  });
});
