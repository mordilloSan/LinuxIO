import { describe, expect, it, vi } from "vitest";

import AppVirtualDataTable from "@/components/tables/AppVirtualDataTable";
import type { AppVirtualDataTableColumnDef } from "@/components/tables/AppVirtualDataTable";
import { render, screen } from "@/test/render";

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

const columns: AppVirtualDataTableColumnDef<TableRow>[] = [
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
  selectedRowId,
  tableColumns = columns,
}: {
  data?: TableRow[];
  selectedRowId?: string;
  tableColumns?: AppVirtualDataTableColumnDef<TableRow>[];
}) {
  return (
    <AppVirtualDataTable
      columns={tableColumns}
      data={data}
      fillAvailable={false}
      getRowId={(row) => row.id}
      height={200}
      selectedRowId={selectedRowId}
    />
  );
}

describe("AppVirtualDataTable", () => {
  it("renders only cells whose field render key changed", () => {
    const view = render(<TestTable />);

    expect(renderName).toHaveBeenCalledTimes(2);
    expect(renderStatus).toHaveBeenCalledTimes(2);

    view.rerender(<TestTable selectedRowId="two" />);

    expect(screen.getByText("Beta").closest('[role="row"]')).toHaveClass(
      "app-vdt__row--selected",
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
    const replacementColumns: AppVirtualDataTableColumnDef<TableRow>[] = [
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
});
