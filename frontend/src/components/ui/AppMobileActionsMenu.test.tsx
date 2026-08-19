import { describe, expect, it, vi } from "vitest";

import AppMobileActionsMenu from "@/components/ui/AppMobileActionsMenu";
import { render, screen } from "@/test/render";

describe("AppMobileActionsMenu", () => {
  it("uses the compact shared icon-row layout", () => {
    render(
      <AppMobileActionsMenu anchorEl={document.body} onClose={vi.fn()} open>
        <button type="button">Search</button>
        <button type="button">Refresh</button>
      </AppMobileActionsMenu>,
    );

    const menu = screen.getByRole("menu", { name: "Actions" });
    expect(menu.parentElement).toHaveStyle({ minWidth: "0px" });
    expect(menu.querySelector(".app-mobile-actions-menu")).toContainElement(
      screen.getByRole("button", { name: "Search" }),
    );
    expect(menu.querySelector(".app-mobile-actions-menu")).toContainElement(
      screen.getByRole("button", { name: "Refresh" }),
    );
  });
});
