import { afterEach, describe, expect, it, vi } from "vitest";

import ViewModeToggle from "@/components/ui/ViewModeToggle";
import { render, screen } from "@/test/render";

vi.mock("@iconify/react", () => ({
  Icon: ({ icon }: { icon: string }) => (
    <span data-icon={icon} data-testid="view-icon" />
  ),
}));

describe("ViewModeToggle", () => {
  afterEach(() => {
    Reflect.deleteProperty(document, "startViewTransition");
  });

  it.each([
    ["table", "table", "Switch to card view", "mdi:card-multiple", "card"],
    ["table", "card", "Switch to table view", "mdi:view-list", "table"],
    ["list", "list", "Switch to card view", "mdi:card-multiple", "card"],
    ["list", "card", "Switch to list view", "mdi:view-list", "list"],
  ] as const)(
    "switches from %s/%s mode to the alternate presentation",
    async (alternateMode, viewMode, label, icon, nextMode) => {
      const onViewModeChange = vi.fn();
      const { user } = render(
        <ViewModeToggle
          alternateMode={alternateMode}
          onViewModeChange={onViewModeChange}
          viewMode={viewMode}
        />,
      );

      const button = screen.getByRole("button", { name: label });
      expect(button).toHaveAttribute("aria-label", label);
      expect(screen.getByTestId("view-icon")).toHaveAttribute(
        "data-icon",
        icon,
      );
      await user.hover(button);
      expect(await screen.findByRole("tooltip")).toHaveTextContent(label);

      await user.click(button);
      expect(onViewModeChange).toHaveBeenCalledWith(nextMode);
    },
  );

  it("updates the view inside a browser view transition", async () => {
    const onViewModeChange = vi.fn();
    const startViewTransition = vi.fn((update: () => void) => update());
    Object.defineProperty(document, "startViewTransition", {
      configurable: true,
      value: startViewTransition,
    });
    const { user } = render(
      <ViewModeToggle
        alternateMode="table"
        onViewModeChange={onViewModeChange}
        viewMode="table"
      />,
    );

    await user.click(
      screen.getByRole("button", { name: "Switch to card view" }),
    );

    expect(startViewTransition).toHaveBeenCalledOnce();
    expect(onViewModeChange).toHaveBeenCalledWith("card");
  });
});
