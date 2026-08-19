import { describe, expect, it, vi } from "vitest";

import { ConfigContext } from "@/contexts/ConfigContext";
import { createConfigContextValue, render, screen } from "@/test/render";

import SettingsPage from "./SettingsPage";

// The shared render helper hands every tree an inert setKey; an inner provider
// overrides it so a write can be observed without reaching for the real one.
const renderWithConfigSpy = (
  appSettings: Parameters<typeof createConfigContextValue>[0],
  setKey: ReturnType<typeof vi.fn>,
) =>
  render(
    <ConfigContext.Provider
      value={{ ...createConfigContextValue(appSettings), setKey }}
    >
      <SettingsPage />
    </ConfigContext.Provider>,
  );

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

describe("SettingsPage dock colors", () => {
  it("shows the palette editor only for the accent family", () => {
    const { unmount } = render(<SettingsPage />, {
      appSettings: { dockTileColors: "accent" },
    });

    expect(
      screen.getByRole("group", { name: "Palette range" }),
    ).toBeInTheDocument();

    unmount();
    render(<SettingsPage />, {
      appSettings: { dockTileColors: "mono" },
    });

    expect(
      screen.queryByRole("group", { name: "Palette range" }),
    ).not.toBeInTheDocument();
  });
});

describe("SettingsPage theme mode", () => {
  it("reflects the saved mode and persists a switch", async () => {
    const setKey = vi.fn();
    const { user } = renderWithConfigSpy({ theme: "DARK" }, setKey);

    expect(screen.getByRole("tab", { name: "Dark" })).toHaveAttribute(
      "aria-selected",
      "true",
    );

    await user.click(screen.getByRole("tab", { name: "Light" }));

    expect(setKey).toHaveBeenCalledWith("theme", "LIGHT");
  });

  it("shows light as selected once it is the saved mode", () => {
    const { unmount } = renderWithConfigSpy({ theme: "LIGHT" }, vi.fn());

    expect(screen.getByRole("tab", { name: "Light" })).toHaveAttribute(
      "aria-selected",
      "true",
    );

    unmount();
  });
});
