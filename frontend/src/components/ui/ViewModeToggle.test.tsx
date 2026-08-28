import { afterEach, describe, expect, it, vi } from "vitest";

import ViewModeToggle from "@/components/ui/ViewModeToggle";
import { render, screen, waitFor } from "@/test/render";

vi.mock("@iconify/react", () => ({
  Icon: ({ icon }: { icon: string }) => (
    <span data-icon={icon} data-testid="view-icon" />
  ),
}));

const motionMocks = vi.hoisted(() => ({ animate: vi.fn() }));

vi.mock("motion/react", async (importOriginal) => ({
  ...(await importOriginal<typeof import("motion/react")>()),
  animate: motionMocks.animate,
}));

describe("ViewModeToggle", () => {
  afterEach(() => {
    motionMocks.animate.mockReset();
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

  it("animates the route content after changing views", async () => {
    const onViewModeChange = vi.fn();
    const { user } = render(
      <>
        <div data-app-route-content>
          <div data-app-view-mode-content data-testid="view-mode-content" />
        </div>
        <ViewModeToggle
          alternateMode="table"
          onViewModeChange={onViewModeChange}
          viewMode="table"
        />
      </>,
    );

    const button = screen.getByRole("button", {
      name: "Switch to card view",
    });
    const content = screen.getByTestId("view-mode-content");
    await user.click(button);

    expect(onViewModeChange).toHaveBeenCalledWith("card");
    expect(content.style.opacity).toBe("0");
    await waitFor(() =>
      expect(motionMocks.animate).toHaveBeenCalledWith(
        content,
        { opacity: [0, 1], y: [6, 0] },
        { duration: 0.2, ease: [0, 0, 0.2, 1] },
      ),
    );
  });
});
