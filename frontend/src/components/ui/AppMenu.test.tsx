import { screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";

import AppMenu, { AppMenuItem } from "@/components/ui/AppMenu";
import { render } from "@/test/render";

const MenuHarness = () => {
  const [anchorEl, setAnchorEl] = useState<HTMLButtonElement | null>(null);

  return (
    <>
      <button
        onClick={(event) => setAnchorEl(event.currentTarget)}
        type="button"
      >
        Open menu
      </button>
      <AppMenu
        anchorEl={anchorEl}
        onClose={() => setAnchorEl(null)}
        open={Boolean(anchorEl)}
      >
        <AppMenuItem>Action</AppMenuItem>
      </AppMenu>
    </>
  );
};

describe("AppMenu", () => {
  it("names the menu and provides shared keyboard navigation", async () => {
    const onClose = vi.fn();
    const { user } = render(
      <AppMenu
        anchorPosition={{ top: 20, left: 20 }}
        ariaLabel="Account actions"
        id="account-actions"
        onClose={onClose}
        open
      >
        <AppMenuItem>First action</AppMenuItem>
        <AppMenuItem>Second action</AppMenuItem>
      </AppMenu>,
    );

    expect(
      screen.getByRole("menu", { name: "Account actions" }),
    ).toHaveAttribute("id", "account-actions");

    const first = screen.getByRole("menuitem", { name: "First action" });
    const second = screen.getByRole("menuitem", { name: "Second action" });
    await waitFor(() => expect(first).toHaveFocus());

    await user.keyboard("{ArrowDown}");
    expect(second).toHaveFocus();

    await user.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalledOnce();
  });

  it("restores its connected trigger when closing owned focus", async () => {
    const { user } = render(<MenuHarness />);
    const trigger = screen.getByRole("button", { name: "Open menu" });

    await user.click(trigger);
    await waitFor(() =>
      expect(screen.getByRole("menuitem", { name: "Action" })).toHaveFocus(),
    );
    await user.keyboard("{Escape}");

    await waitFor(() => expect(trigger).toHaveFocus());
  });
});
