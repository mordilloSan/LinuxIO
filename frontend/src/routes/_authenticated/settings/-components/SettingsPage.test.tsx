import { describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import SettingsPage from "./SettingsPage";

const mockDesktopViewport = (isDesktop: boolean) => {
  vi.mocked(window.matchMedia).mockImplementation(
    (query) =>
      ({
        addEventListener: vi.fn(),
        matches: isDesktop && query === "(min-width:960px)",
        media: query,
        onchange: null,
        removeEventListener: vi.fn(),
      }) as unknown as MediaQueryList,
  );
};

describe("SettingsPage navigation options", () => {
  it("offers only the sidebar on small screens", () => {
    mockDesktopViewport(false);

    render(<SettingsPage />, { appSettings: { navigationMode: "dock" } });

    expect(screen.queryByRole("tab", { name: "Dock" })).not.toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Sidebar" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
  });

  it("offers the saved dock preference on desktop", () => {
    mockDesktopViewport(true);

    render(<SettingsPage />, { appSettings: { navigationMode: "dock" } });

    expect(screen.getByRole("tab", { name: "Dock" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
  });
});
