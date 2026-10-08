import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { render } from "@/test/render";

import ModeMenu from "./ModeMenu";

const modes = {
  currentModeId: "plan",
  availableModes: [
    { id: "default", name: "Default", description: "Ask before editing" },
    { id: "plan", name: "Plan", description: "Plan without editing" },
    { id: "custom", name: "Custom" },
  ],
};

describe("ModeMenu", () => {
  it("names the current mode on the trigger", () => {
    render(<ModeMenu disabled={false} modes={modes} onSetMode={vi.fn()} />);
    expect(
      screen.getByRole("button", { name: "Mode: Plan" }),
    ).toHaveTextContent("Plan");
  });

  it("announces its shortcut", () => {
    render(<ModeMenu disabled={false} modes={modes} onSetMode={vi.fn()} />);
    expect(screen.getByRole("button", { name: "Mode: Plan" })).toHaveAttribute(
      "aria-keyshortcuts",
      "Shift+Tab",
    );
  });

  it("follows current_mode_update without user action", () => {
    const { rerender } = render(
      <ModeMenu disabled={false} modes={modes} onSetMode={vi.fn()} />,
    );
    rerender(
      <ModeMenu
        disabled={false}
        modes={{ ...modes, currentModeId: "default" }}
        onSetMode={vi.fn()}
      />,
    );
    expect(
      screen.getByRole("button", { name: "Mode: Default" }),
    ).toBeInTheDocument();
  });

  it("lists each mode with its description and checks the selected one", async () => {
    render(<ModeMenu disabled={false} modes={modes} onSetMode={vi.fn()} />);
    await userEvent.click(screen.getByRole("button", { name: "Mode: Plan" }));
    const menu = await screen.findByRole("menu", { name: "Modes" });
    expect(within(menu).getByText("Modes")).toBeInTheDocument();
    const items = within(menu).getAllByRole("menuitem");
    expect(items).toHaveLength(3);
    expect(items[0]).toHaveTextContent("Ask before editing");
    expect(items[1]).toHaveTextContent("Plan without editing");
    expect(items[1].querySelector(".app-menu__item-end")).not.toBeNull();
    expect(items[0].querySelector(".app-menu__item-end")).toBeNull();
  });

  it("selects a mode and closes", async () => {
    const onSetMode = vi.fn();
    render(<ModeMenu disabled={false} modes={modes} onSetMode={onSetMode} />);
    await userEvent.click(screen.getByRole("button", { name: "Mode: Plan" }));
    await userEvent.click(
      await screen.findByRole("menuitem", { name: /Default/ }),
    );
    expect(onSetMode).toHaveBeenCalledWith("default");
    expect(screen.queryByRole("menu", { name: "Modes" })).toBeNull();
  });
});
