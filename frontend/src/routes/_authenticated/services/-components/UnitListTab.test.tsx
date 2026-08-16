import { describe, expect, it, vi } from "vitest";

import { render } from "@/test/render";

import UnitListTab from "./UnitListTab";

describe("UnitListTab", () => {
  it("keeps the table viewport in a height-constrained flex chain", () => {
    const { container } = render(
      <UnitListTab
        compareItems={() => 0}
        data={[]}
        matchesSearch={() => true}
        onSelectedChange={vi.fn()}
        renderCardsView={() => null}
        renderDetailPanel={() => null}
        renderTableView={() => <div data-testid="table-viewport" />}
        searchPlaceholder="Search units"
        surfaceId="services.list"
        viewMode="table"
      />,
    );

    const viewport = container.querySelector('[data-testid="table-viewport"]');
    const tableContainer = viewport?.parentElement;
    const layout = tableContainer?.parentElement;

    expect(layout).toHaveStyle({
      display: "flex",
      flexDirection: "column",
      height: "100%",
      minHeight: "0",
    });
    expect(tableContainer).toHaveStyle({
      display: "flex",
      flex: "1 1 0",
      flexDirection: "column",
      minHeight: "0",
    });
  });
});
