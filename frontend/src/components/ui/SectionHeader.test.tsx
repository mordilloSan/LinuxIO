import { describe, expect, it, vi } from "vitest";

import SectionHeader from "@/components/ui/SectionHeader";
import { fireEvent, render, screen } from "@/test/render";

vi.mock("@iconify/react", () => ({
  Icon: () => <span aria-hidden="true" />,
}));

describe("SectionHeader", () => {
  it("exposes a named disclosure button and toggles its state", () => {
    const onToggle = vi.fn();
    const { rerender } = render(
      <SectionHeader
        controlsId="section-panel"
        expanded={false}
        onToggle={onToggle}
        title="Services"
      />,
    );

    const trigger = screen.getByRole("button", { name: "Services" });
    expect(trigger).toHaveAttribute("aria-controls", "section-panel");
    expect(trigger).toHaveAttribute("aria-expanded", "false");

    fireEvent.click(trigger);
    expect(onToggle).toHaveBeenCalledTimes(1);

    rerender(
      <SectionHeader
        controlsId="section-panel"
        expanded
        onToggle={onToggle}
        title="Services"
      />,
    );

    expect(trigger).toHaveAttribute("aria-expanded", "true");
  });
});
