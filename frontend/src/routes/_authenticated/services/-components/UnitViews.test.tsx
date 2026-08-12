import { describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import { UnitTableView } from "./UnitViews";

vi.mock("@tanstack/react-virtual", () => ({
  useVirtualizer: ({ count }: { count: number }) => ({
    getTotalSize: () => count * 48,
    getVirtualItems: () =>
      Array.from({ length: count }, (_, index) => ({
        end: (index + 1) * 48,
        index,
        key: index,
        lane: 0,
        size: 48,
        start: index * 48,
      })),
    measure: vi.fn(),
    measureElement: vi.fn(),
    resizeItem: vi.fn(),
    scrollToIndex: vi.fn(),
  }),
}));

interface UnitRow {
  id: string;
  name: string;
  status: string;
}

const tableColumns = [
  { field: "name", headerName: "Name" },
  { field: "status", headerName: "Status" },
];
const getRowKey = (row: UnitRow) => row.id;
const renderMainRow = vi.fn((row: UnitRow) => (
  <>
    <span>{row.name}</span>
    <span>{row.status}</span>
  </>
));
const initialRows: UnitRow[] = [
  { id: "one", name: "Alpha", status: "running" },
  { id: "two", name: "Beta", status: "stopped" },
];

function TestUnitTable({ data }: { data: UnitRow[] }) {
  return (
    <UnitTableView
      data={data}
      desktopColumns={tableColumns}
      emptyMessage="No units"
      getRowKey={getRowKey}
      mobileColumns={tableColumns}
      renderMainRow={renderMainRow}
    />
  );
}

describe("UnitTableView", () => {
  it("reuses unchanged formatted rows and refreshes changed or reordered rows", () => {
    const view = render(<TestUnitTable data={initialRows} />);

    expect(renderMainRow).toHaveBeenCalledTimes(2);

    view.rerender(<TestUnitTable data={[...initialRows]} />);
    expect(renderMainRow).toHaveBeenCalledTimes(2);

    const changedSecondRow = { ...initialRows[1], status: "running" };
    view.rerender(<TestUnitTable data={[initialRows[0], changedSecondRow]} />);

    expect(screen.getAllByText("running")).toHaveLength(2);
    expect(renderMainRow).toHaveBeenCalledTimes(3);

    view.rerender(<TestUnitTable data={[changedSecondRow, initialRows[0]]} />);

    expect(renderMainRow).toHaveBeenCalledTimes(5);
  });
});
