import { screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { render } from "@/test/render";

vi.mock("@tanstack/react-query-devtools", () => ({
  ReactQueryDevtoolsPanel: () => <div data-testid="query-devtools" />,
}));

vi.mock("@tanstack/react-router-devtools", () => ({
  TanStackRouterDevtoolsPanel: ({ isOpen }: { isOpen?: boolean }) => (
    <div data-testid="router-devtools">{String(isOpen)}</div>
  ),
}));

const { DevToolsPanel } = await import("./DevToolsPanel");

describe("DevToolsPanel", () => {
  it("closes from the shared panel action", async () => {
    const onClose = vi.fn();
    const { user } = render(
      <DevToolsPanel
        isOpen
        isWebVitalsVisible={false}
        onClose={onClose}
        onToggleWebVitals={() => {}}
      />,
    );

    await user.click(
      screen.getByRole("button", { name: "Close developer tools" }),
    );

    expect(onClose).toHaveBeenCalledOnce();
  });

  it("delegates the Web Vitals footer toggle", async () => {
    const onToggleWebVitals = vi.fn();
    const { user } = render(
      <DevToolsPanel
        isOpen
        isWebVitalsVisible={false}
        onClose={() => {}}
        onToggleWebVitals={onToggleWebVitals}
      />,
    );

    await user.click(
      screen.getByRole("button", { name: "Show Web Vitals in Footer" }),
    );

    expect(onToggleWebVitals).toHaveBeenCalledOnce();
  });

  it("opens the router devtools in the same panel and closes query devtools", async () => {
    const { user } = render(
      <DevToolsPanel
        isOpen
        isWebVitalsVisible={false}
        onClose={() => {}}
        onToggleWebVitals={() => {}}
      />,
    );

    await user.click(
      screen.getByRole("button", { name: "Open TanStack Router Devtools" }),
    );

    expect(screen.getByRole("dialog")).toHaveStyle({ resize: "both" });
    expect(screen.getByTestId("devtools-modal-content")).toHaveStyle({
      overflow: "auto",
    });
    expect(screen.getByTestId("router-devtools")).toHaveTextContent("true");
    expect(screen.queryByTestId("query-devtools")).not.toBeInTheDocument();

    await user.click(
      screen.getByRole("button", { name: "Close development tools" }),
    );

    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();

    await user.click(
      screen.getByRole("button", { name: "Open TanStack Router Devtools" }),
    );

    await user.click(
      screen.getByRole("button", { name: "Open Tanstack Query Devtools" }),
    );

    expect(screen.queryByTestId("router-devtools")).not.toBeInTheDocument();
    expect(screen.getByTestId("query-devtools")).toBeInTheDocument();
  });
});
