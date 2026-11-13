import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import UnitKindPage from "./UnitKindPage";
import { serviceKind } from "./unitKinds";

const service = {
  active_state: "active",
  name: "nginx.service",
  unit_file_state: "enabled",
};

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return { ...actual, useSuspenseQuery: () => ({ data: [service] }) };
});

vi.mock("@/hooks/useViewMode", () => ({
  useViewMode: () => ["card", vi.fn()],
}));

vi.mock("@/components/tabbar", () => ({
  RoutedTabActions: () => null,
}));

// The list hands its cards view the items; the cards view shows whether it was
// given actions to render.
vi.mock("./UnitListTab", () => ({
  default: ({
    data,
    renderCardsView,
  }: {
    data: unknown[];
    renderCardsView: (props: Record<string, unknown>) => ReactNode;
  }) => <>{renderCardsView({ items: data })}</>,
}));

vi.mock("./UnitViews", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./UnitViews")>();
  return {
    ...actual,
    UnitCardsView: ({ renderActions }: { renderActions?: unknown }) => (
      <p>{renderActions ? "with actions" : "without actions"}</p>
    ),
  };
});

const renderPage = (privileged: boolean) =>
  render(
    <UnitKindPage
      kind={serviceKind}
      listQueryOptions={{ queryKey: ["services"] }}
      onSelectedChange={vi.fn()}
      selectedQueryOptions={undefined}
    />,
    { auth: { privileged } },
  );

describe("UnitKindPage", () => {
  it("gives a session with sudo the unit actions", () => {
    renderPage(true);

    expect(screen.getByText("with actions")).toBeInTheDocument();
  });

  it("shows the units without actions to a session without sudo", () => {
    renderPage(false);

    expect(screen.getByText("without actions")).toBeInTheDocument();
  });
});
