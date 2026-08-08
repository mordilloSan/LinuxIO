import { screen, waitFor } from "@testing-library/react";
import { useState, type ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import { render } from "@/test/render";

import {
  RoutedTabActions,
  RoutedTabLayout,
  type RoutedTab,
} from "./RoutedTabContainer";

const mocks = vi.hoisted(() => ({ linkRender: vi.fn() }));

vi.mock("@tanstack/react-router", () => ({
  Link: ({
    children,
    className,
    role,
    to,
  }: {
    children: ReactNode;
    className?: string;
    role?: string;
    to: string;
  }) => {
    mocks.linkRender(to);
    return (
      <a className={className} href={to} role={role}>
        {children}
      </a>
    );
  },
}));

const tabs = [
  { label: "Users", to: "/accounts" },
  { label: "Groups", to: "/accounts/groups" },
] as const satisfies readonly RoutedTab[];

const LayoutHarness = () => {
  const [panelVersion, setPanelVersion] = useState(0);

  return (
    <>
      <button
        onClick={() => setPanelVersion((version) => version + 1)}
        type="button"
      >
        Update panel
      </button>
      <RoutedTabLayout tabs={tabs}>
        <RoutedTabActions>
          <button type="button">Route action</button>
        </RoutedTabActions>
        <div>Panel {panelVersion}</div>
      </RoutedTabLayout>
    </>
  );
};

describe("RoutedTabLayout render boundary", () => {
  it("does not rerender tab links for action registration or panel updates", async () => {
    const { user } = render(<LayoutHarness />);

    expect(await screen.findByText("Route action")).toBeInTheDocument();
    await waitFor(() => expect(mocks.linkRender).toHaveBeenCalledTimes(2));

    await user.click(screen.getByRole("button", { name: "Update panel" }));

    expect(screen.getByText("Panel 1")).toBeInTheDocument();
    expect(mocks.linkRender).toHaveBeenCalledTimes(2);
  });
});
