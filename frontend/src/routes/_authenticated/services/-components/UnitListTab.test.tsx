import { describe, expect, it, vi } from "vitest";

import { act, render } from "@/test/render";

import UnitListTab from "./UnitListTab";

describe("UnitListTab", () => {
  it.each(["handled", "app-dialog-root", "app-fullscreen-dialog-root"])(
    "preserves selection when Escape belongs to %s",
    (owner) => {
      const onSelectedChange = vi.fn();
      render(
        <UnitListTab
          compareItems={() => 0}
          data={[]}
          matchesSearch={() => true}
          onSelectedChange={onSelectedChange}
          renderCardsView={() => null}
          renderDetailPanel={() => null}
          renderTableView={() => null}
          searchPlaceholder="Search units"
          selected="one.service"
          surfaceId="services.list"
          viewMode="table"
        />,
      );
      const overlay = document.createElement("div");
      const event = new KeyboardEvent("keydown", {
        key: "Escape",
        cancelable: true,
      });
      if (owner === "handled") event.preventDefault();
      else {
        overlay.className = owner;
        document.body.append(overlay);
      }
      act(() => {
        window.dispatchEvent(event);
      });
      overlay.remove();
      expect(onSelectedChange).not.toHaveBeenCalled();

      const unhandled = new KeyboardEvent("keydown", {
        key: "Escape",
        cancelable: true,
      });
      act(() => {
        window.dispatchEvent(unhandled);
      });
      expect(onSelectedChange).toHaveBeenCalledExactlyOnceWith(null);
      expect(unhandled.defaultPrevented).toBe(true);
    },
  );

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
