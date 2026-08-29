import { beforeEach, describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import MainLayout from "./MainLayout";

const layoutMocks = vi.hoisted(() => ({
  isDesktop: false,
  storageMode: "home",
  toggleMobileOpen: vi.fn(),
}));

vi.mock("@tanstack/react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-router")>()),
  Outlet: () => <div>Route content</div>,
  useLocation: () => ({ pathname: "/settings" }),
}));

vi.mock("@/components/loaders/BootstrapLoaderReady", () => ({
  default: () => null,
}));
vi.mock("@/hooks/useConfig", () => ({
  useConfigStorageMode: () => layoutMocks.storageMode,
  useConfigValue: () => ["dock", vi.fn()],
}));
vi.mock("./footer/Footer", () => ({ default: () => null }));
vi.mock("./navbar/Navbar", () => ({
  default: ({
    dockMode,
    onDrawerToggle,
  }: {
    dockMode: boolean;
    onDrawerToggle?: () => void;
  }) => (
    <div data-dock-mode={String(dockMode)} data-testid="navbar">
      {onDrawerToggle ? (
        <button onClick={onDrawerToggle} type="button">
          Open drawer
        </button>
      ) : null}
    </div>
  ),
}));
vi.mock("./sidebar/Sidebar", () => ({
  default: () => <aside aria-label="Primary navigation" />,
}));
vi.mock("./sidebar/useCloseMobileSidebarOnNavigate", () => ({
  useCloseMobileSidebarOnNavigate: () => undefined,
}));
vi.mock("./sidebar/useSidebar", () => ({
  default: () => ({
    isDesktop: layoutMocks.isDesktop,
    sidebarWidth: 260,
    toggleMobileOpen: layoutMocks.toggleMobileOpen,
  }),
}));
vi.mock("./sidebar/useSidebarItems", () => ({
  useSidebarItems: () => [],
}));
vi.mock("./update/useUpdateInfo", () => ({
  useUpdateInfo: () => ({ dismissUpdate: vi.fn(), updateInfo: null }),
}));

describe("MainLayout navigation mode", () => {
  beforeEach(() => {
    layoutMocks.isDesktop = false;
    layoutMocks.storageMode = "home";
  });

  it("forces the sidebar drawer when a small screen has a dock preference", async () => {
    const { user } = render(<MainLayout />);

    expect(
      screen.getByRole("complementary", { name: "Primary navigation" }),
    ).toBeInTheDocument();
    expect(screen.getByTestId("navbar")).toHaveAttribute(
      "data-dock-mode",
      "false",
    );

    await user.click(screen.getByRole("button", { name: "Open drawer" }));
    expect(layoutMocks.toggleMobileOpen).toHaveBeenCalledOnce();
  });

  it("keeps the dock preference on desktop", () => {
    layoutMocks.isDesktop = true;

    render(<MainLayout />);

    expect(
      screen.queryByRole("complementary", { name: "Primary navigation" }),
    ).not.toBeInTheDocument();
    expect(screen.getByTestId("navbar")).toHaveAttribute(
      "data-dock-mode",
      "true",
    );
    expect(
      screen.queryByRole("button", { name: "Open drawer" }),
    ).not.toBeInTheDocument();
  });

  it.each([
    [
      "fallback",
      "Using fallback settings storage",
      "Home settings storage is unavailable. Settings are being saved in LinuxIO's persistent fallback storage.",
    ],
    [
      "memory",
      "Settings are temporary",
      "Persistent settings storage is unavailable. Changes will be lost when you refresh or sign out.",
    ],
  ])("warns when config storage uses %s mode", (mode, title, message) => {
    layoutMocks.storageMode = mode;

    render(<MainLayout />);

    const warning = screen.getByRole("status");
    expect(warning).toHaveTextContent(title);
    expect(warning).toHaveTextContent(message);
  });
});
