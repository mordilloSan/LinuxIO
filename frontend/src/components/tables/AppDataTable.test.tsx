import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import AppDataTable from "@/components/tables/AppDataTable";
import type { AppDataTableColumnDef } from "@/components/tables/AppDataTable.types";
import { render, screen } from "@/test/render";

const virtualizerSpies = vi.hoisted(() => ({
  measure: vi.fn(),
}));

vi.mock("@tanstack/react-virtual", () => ({
  useVirtualizer: ({
    count,
    getItemKey,
  }: {
    count: number;
    getItemKey: (index: number) => string | number;
  }) => ({
    getTotalSize: () => count * 48,
    getVirtualItems: () =>
      Array.from({ length: count }, (_, index) => ({
        end: (index + 1) * 48,
        index,
        key: getItemKey(index),
        lane: 0,
        size: 48,
        start: index * 48,
      })),
    measure: virtualizerSpies.measure,
    measureElement: vi.fn(),
    resizeItem: vi.fn(),
    scrollToIndex: vi.fn(),
  }),
}));

interface TableRow {
  id: string;
  name: string;
  status: string;
}

const renderName = vi.fn(
  ({ row }: { row: { original: TableRow } }) => row.original.name,
);
const renderStatus = vi.fn(
  ({ row }: { row: { original: TableRow } }) => row.original.status,
);

const columns: AppDataTableColumnDef<TableRow>[] = [
  {
    id: "name",
    header: "Name",
    cell: renderName,
    meta: {
      getCellRenderKey: (row) => {
        const item = row as TableRow;
        return [item.id, item.name];
      },
    },
  },
  {
    id: "status",
    header: "Status",
    cell: renderStatus,
    meta: {
      getCellRenderKey: (row) => {
        const item = row as TableRow;
        return [item.id, item.status];
      },
    },
  },
];

const initialRows: TableRow[] = [
  { id: "one", name: "Alpha", status: "running" },
  { id: "two", name: "Beta", status: "stopped" },
];

function TestTable({
  data = initialRows,
  expandedContent,
  selectedRowId,
  tableColumns = columns,
}: {
  data?: TableRow[];
  expandedContent?: (row: { original: TableRow }) => ReactNode;
  selectedRowId?: string;
  tableColumns?: AppDataTableColumnDef<TableRow>[];
}) {
  return (
    <AppDataTable
      columns={tableColumns}
      data={data}
      fillAvailable={false}
      getRowId={(row) => row.id}
      height={200}
      renderExpandedContent={expandedContent}
      selectedRowId={selectedRowId}
    />
  );
}

describe("AppDataTable", () => {
  it("renders only cells whose field render key changed", () => {
    const view = render(<TestTable />);

    expect(renderName).toHaveBeenCalledTimes(2);
    expect(renderStatus).toHaveBeenCalledTimes(2);

    view.rerender(<TestTable selectedRowId="two" />);

    expect(screen.getByText("Beta").closest('[role="row"]')).toHaveClass(
      "app-dt__row--selected",
    );
    expect(renderName).toHaveBeenCalledTimes(2);
    expect(renderStatus).toHaveBeenCalledTimes(2);

    view.rerender(
      <TestTable
        data={[initialRows[0], { ...initialRows[1], status: "running" }]}
        selectedRowId="two"
      />,
    );

    expect(renderName).toHaveBeenCalledTimes(2);
    expect(renderStatus).toHaveBeenCalledTimes(3);
  });

  it("does not retain a stale renderer when a column definition changes", () => {
    const view = render(<TestTable />);
    const replacementColumns: AppDataTableColumnDef<TableRow>[] = [
      {
        ...columns[0],
        cell: ({ row }) => `renamed:${row.original.name}`,
      },
      columns[1],
    ];

    view.rerender(<TestTable tableColumns={replacementColumns} />);

    expect(screen.getByText("renamed:Alpha")).toBeInTheDocument();
    expect(screen.getByText("renamed:Beta")).toBeInTheDocument();
  });

  it("toggles the detail panel when the row itself is clicked", async () => {
    const view = render(
      <TestTable
        expandedContent={({ original }) => (
          <div>{`Details for ${original.name}`}</div>
        )}
      />,
    );
    const row = screen.getByText("Alpha").closest('[role="row"]')!;

    expect(row).toHaveClass("app-dt__row--interactive");
    expect(row).toHaveAttribute("aria-expanded", "false");

    await view.user.click(row);
    expect(screen.getByText("Details for Alpha")).toBeInTheDocument();
    expect(row).toHaveAttribute("aria-expanded", "true");
  });

  it("toggles detail entries without resetting the virtualizer", async () => {
    virtualizerSpies.measure.mockClear();
    const view = render(
      <TestTable
        expandedContent={({ original }) => (
          <div>{`Details for ${original.name}`}</div>
        )}
      />,
    );
    const row = screen.getByText("Alpha").closest('[role="row"]')!;

    await view.user.click(row);
    expect(screen.getByText("Details for Alpha")).toBeInTheDocument();
    expect(virtualizerSpies.measure).not.toHaveBeenCalled();

    await view.user.click(row);
    expect(virtualizerSpies.measure).not.toHaveBeenCalled();
  });

  it("does not rerender stable explicit-key cells when live rows prepend", () => {
    const view = render(<TestTable />);

    expect(renderName).toHaveBeenCalledTimes(2);
    expect(renderStatus).toHaveBeenCalledTimes(2);

    view.rerender(
      <TestTable
        data={[
          { id: "new", name: "Newest", status: "running" },
          ...initialRows,
        ]}
      />,
    );

    expect(screen.getByText("Newest")).toBeInTheDocument();
    expect(renderName).toHaveBeenCalledTimes(3);
    expect(renderStatus).toHaveBeenCalledTimes(3);
  });
});
