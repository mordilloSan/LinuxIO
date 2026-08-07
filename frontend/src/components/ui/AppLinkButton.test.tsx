import { describe, expect, it, vi } from "vitest";

import AppLinkButton from "@/components/ui/AppLinkButton";
import { fireEvent, render, screen } from "@/test/render";

describe("AppLinkButton", () => {
  it("renders a semantic anchor and passes target props", () => {
    render(
      <AppLinkButton href="/release" rel="noopener" target="_blank">
        Release Notes
      </AppLinkButton>,
    );
    const link = screen.getByRole("link", { name: "Release Notes" });
    expect(link).toHaveAttribute("href", "/release");
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", "noopener");
  });

  it("removes href and prevents activation while disabled", () => {
    const onClick = vi.fn();
    render(
      <AppLinkButton disabled href="/release" onClick={onClick}>
        Release Notes
      </AppLinkButton>,
    );
    const link = screen.getByRole("link", { name: "Release Notes" });
    expect(link).not.toHaveAttribute("href");
    expect(link).toHaveAttribute("aria-disabled", "true");
    expect(link).toHaveAttribute("tabindex", "-1");
    fireEvent.click(link);
    expect(onClick).not.toHaveBeenCalled();
  });

  it.each([
    ["secondary", "secondary"],
    ["error", "error"],
    ["inherit", undefined],
  ] as const)("applies the %s color contract", (color, mainVariable) => {
    render(
      <AppLinkButton color={color} href="/release" variant="contained">
        Release Notes
      </AppLinkButton>,
    );
    const link = screen.getByRole("link", { name: "Release Notes" });
    if (mainVariable) {
      expect(link.style.getPropertyValue("--_btn-main")).toBe(
        `var(--app-palette-${mainVariable}-main)`,
      );
    } else {
      expect(link.style.getPropertyValue("--_btn-main")).toBe("");
    }
  });
});
