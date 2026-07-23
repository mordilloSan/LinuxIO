import { useNavigate } from "@tanstack/react-router";
import { describe, expect, it, vi } from "vitest";

import {
  SidebarContext,
  type SidebarContextType,
} from "@/contexts/SidebarContext";
import { useCloseMobileSidebarOnNavigate } from "@/hooks/useCloseMobileSidebarOnNavigate";
import { renderWithTanStackRouter, screen } from "@/test/render";

function makeSidebar(
  overrides: Partial<SidebarContextType>,
): SidebarContextType {
  return {
    collapsed: false,
    isDesktop: true,
    mobileOpen: false,
    setMobileOpen: vi.fn(),
    sidebarWidth: 240,
    toggleCollapse: vi.fn(),
    toggleMobileOpen: vi.fn(),
    ...overrides,
  };
}

function Consumer() {
  useCloseMobileSidebarOnNavigate();
  return null;
}

function Harness({ sidebar }: { sidebar: SidebarContextType }) {
  const navigate = useNavigate();
  return (
    <SidebarContext.Provider value={sidebar}>
      <Consumer />
      <button onClick={() => navigate({ to: "/docker" })}>go</button>
    </SidebarContext.Provider>
  );
}

describe("useCloseMobileSidebarOnNavigate", () => {
  it("closes the mobile drawer after navigating", async () => {
    const setMobileOpen = vi.fn();
    const sidebar = makeSidebar({ isDesktop: false, setMobileOpen });
    const { user } = renderWithTanStackRouter(<Harness sidebar={sidebar} />);

    setMobileOpen.mockClear(); // ignore the on-mount close

    await user.click(await screen.findByRole("button", { name: "go" }));

    expect(setMobileOpen).toHaveBeenCalledWith(false);
  });

  it("leaves the sidebar untouched on desktop", async () => {
    const setMobileOpen = vi.fn();
    const sidebar = makeSidebar({ isDesktop: true, setMobileOpen });
    const { user } = renderWithTanStackRouter(<Harness sidebar={sidebar} />);

    setMobileOpen.mockClear();

    await user.click(await screen.findByRole("button", { name: "go" }));

    expect(setMobileOpen).not.toHaveBeenCalled();
  });
});
