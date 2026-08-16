import { useState, type ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import AppDataTable from "@/components/tables/AppDataTable";
import type { AppDataTableColumnDef } from "@/components/tables/AppDataTable";
import { act, render, screen } from "@/test/render";

interface SelectableRow {
  id: string;
  name: string;
}

const rows: SelectableRow[] = [{ id: "bridge", name: "bridge" }];

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
const renderNameHeader = vi.fn(() => "Name");
const getRowAttributes = vi.fn(() => ({}));

const tableColumns: AppDataTableColumnDef<TableRow>[] = [
  {
    id: "name",
    header: renderNameHeader,
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

const tableRows: TableRow[] = [
  { id: "one", name: "Alpha", status: "running" },
  { id: "two", name: "Beta", status: "stopped" },
];

const getTableRowId = (row: TableRow) => row.id;

function TestTable({
  columns = tableColumns,
  data = tableRows,
  expandedContent,
  onRowClick,
  selectedRowId,
}: {
  columns?: AppDataTableColumnDef<TableRow>[];
  data?: TableRow[];
  expandedContent?: (row: { original: TableRow }) => ReactNode;
  onRowClick?: () => void;
  selectedRowId?: string;
}) {
  return (
    <AppDataTable
      columns={columns}
      data={data}
      getRowAttributes={getRowAttributes}
      getRowId={getTableRowId}
      onRowClick={onRowClick}
      renderExpandedContent={expandedContent}
      selectedRowId={selectedRowId}
    />
  );
}

function SelectableTable() {
  const [selected, setSelected] = useState<Set<string>>(() => new Set());
  const columns: AppDataTableColumnDef<SelectableRow>[] = [
    {
      id: "select",
      header: "Select",
      cell: ({ row }) => (
        <input
          aria-label={`Select ${row.original.name}`}
          checked={selected.has(row.original.id)}
          onChange={(event) => {
            setSelected((current) => {
              const next = new Set(current);
              if (event.target.checked) {
                next.add(row.original.id);
              } else {
                next.delete(row.original.id);
              }
              return next;
            });
          }}
          type="checkbox"
        />
      ),
      meta: {
        getCellRenderKey: (row) => {
          const item = row as SelectableRow;
          return [item.id, selected.has(item.id)];
        },
      },
    },
    {
      accessorKey: "name",
      header: "Name",
    },
  ];

  return (
    <AppDataTable
      ariaLabel="Selectable rows"
      columns={columns}
      data={rows}
      getRowId={(row) => row.id}
    />
  );
}

describe("AppDataTable", () => {
  beforeEach(() => {
    getRowAttributes.mockClear();
    renderName.mockClear();
    renderNameHeader.mockClear();
    renderStatus.mockClear();
  });

  it("rerenders memoized cells when their render key changes", async () => {
    const { user } = render(<SelectableTable />);
    const checkbox = screen.getByRole("checkbox", { name: "Select bridge" });

    expect(checkbox).not.toBeChecked();
    await user.click(checkbox);
    expect(checkbox).toBeChecked();
    await user.click(checkbox);
    expect(checkbox).not.toBeChecked();
  });

  it("isolates selection and field updates to affected rows and cells", () => {
    const view = render(<TestTable />);

    expect(getRowAttributes).toHaveBeenCalledTimes(2);
    expect(renderName).toHaveBeenCalledTimes(2);
    expect(renderNameHeader).toHaveBeenCalledTimes(1);
    expect(renderStatus).toHaveBeenCalledTimes(2);

    view.rerender(<TestTable selectedRowId="two" />);

    expect(screen.getByText("Beta").closest('[role="row"]')).toHaveClass(
      "app-vdt__row--selected",
    );
    // The row boundary updates selection chrome without re-running cell
    // formatters; the compiler may also reuse pure row-attribute derivation.
    expect(getRowAttributes).toHaveBeenCalledTimes(2);
    expect(renderName).toHaveBeenCalledTimes(2);
    expect(renderNameHeader).toHaveBeenCalledTimes(1);
    expect(renderStatus).toHaveBeenCalledTimes(2);

    view.rerender(
      <TestTable
        data={[tableRows[0], { ...tableRows[1], status: "running" }]}
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
        ...tableColumns[0],
        header: "Renamed",
        cell: ({ row }) => `renamed:${row.original.name}`,
      },
      tableColumns[1],
    ];

    view.rerender(<TestTable columns={replacementColumns} />);

    expect(screen.getByText("renamed:Alpha")).toBeInTheDocument();
    expect(screen.getByText("renamed:Beta")).toBeInTheDocument();
    expect(
      screen.getByRole("columnheader", { name: "Renamed" }),
    ).toBeInTheDocument();
  });

  it("updates index-sensitive cells when stable rows are reordered", () => {
    const indexedColumns: AppDataTableColumnDef<TableRow>[] = [
      {
        id: "name",
        header: "Name",
        cell: ({ row }) => `${row.index}:${row.original.name}`,
        meta: {
          getCellRenderKey: (row) => (row as TableRow).id,
        },
      },
    ];
    const view = render(<TestTable columns={indexedColumns} />);

    expect(screen.getByText("0:Alpha")).toBeInTheDocument();
    expect(screen.getByText("1:Beta")).toBeInTheDocument();

    view.rerender(
      <TestTable
        columns={indexedColumns}
        data={[tableRows[1], tableRows[0]]}
      />,
    );

    expect(screen.getByText("0:Beta")).toBeInTheDocument();
    expect(screen.getByText("1:Alpha")).toBeInTheDocument();
  });

  it("does not construct collapsed detail content", async () => {
    const renderExpandedContent = vi.fn(
      ({ original }: { original: TableRow }) => (
        <div>{`Details for ${original.name}`}</div>
      ),
    );
    const view = render(<TestTable expandedContent={renderExpandedContent} />);

    expect(renderExpandedContent).not.toHaveBeenCalled();

    await view.user.click(
      screen.getAllByRole("button", { name: "Expand row" })[0],
    );

    expect(screen.getByText("Details for Alpha")).toBeInTheDocument();
    expect(renderExpandedContent).toHaveBeenCalledTimes(1);

    view.rerender(
      <TestTable expandedContent={renderExpandedContent} selectedRowId="two" />,
    );

    expect(renderExpandedContent).toHaveBeenCalledTimes(1);
  });

  it("toggles the detail panel when the row itself is clicked", async () => {
    const renderExpandedContent = ({ original }: { original: TableRow }) => (
      <div>{`Details for ${original.name}`}</div>
    );
    const view = render(<TestTable expandedContent={renderExpandedContent} />);
    const row = screen.getByText("Alpha").closest('[role="row"]')!;

    expect(row).toHaveClass("app-vdt__row--interactive");
    expect(row).toHaveAttribute("aria-expanded", "false");

    await view.user.click(row);
    expect(screen.getByText("Details for Alpha")).toBeInTheDocument();
    expect(row).toHaveAttribute("aria-expanded", "true");

    await view.user.click(row);
    expect(screen.queryByText("Details for Alpha")).not.toBeInTheDocument();
  });

  it("leaves clicks on row controls to the control", async () => {
    const onControlClick = vi.fn();
    const columnsWithAction: AppDataTableColumnDef<TableRow>[] = [
      tableColumns[0],
      {
        id: "action",
        header: "Action",
        cell: ({ row }) => (
          <button onClick={onControlClick} type="button">
            {`Restart ${row.original.name}`}
          </button>
        ),
      },
    ];
    const view = render(
      <TestTable
        columns={columnsWithAction}
        expandedContent={({ original }) => (
          <div>{`Details for ${original.name}`}</div>
        )}
      />,
    );

    await view.user.click(
      screen.getByRole("button", { name: "Restart Alpha" }),
    );

    expect(onControlClick).toHaveBeenCalledTimes(1);
    expect(screen.queryByText("Details for Alpha")).not.toBeInTheDocument();
  });

  it("lets a table's own row-click handler keep the click", async () => {
    const onRowClick = vi.fn();
    const view = render(
      <TestTable
        expandedContent={({ original }) => (
          <div>{`Details for ${original.name}`}</div>
        )}
        onRowClick={onRowClick}
      />,
    );

    await view.user.click(screen.getByText("Alpha").closest('[role="row"]')!);

    expect(onRowClick).toHaveBeenCalledTimes(1);
    expect(screen.queryByText("Details for Alpha")).not.toBeInTheDocument();
  });

  it("keeps rows expanded when polled data is replaced", async () => {
    const renderExpandedContent = ({ original }: { original: TableRow }) => (
      <div>{`Details for ${original.name}`}</div>
    );
    const view = render(<TestTable expandedContent={renderExpandedContent} />);

    await view.user.click(
      screen.getAllByRole("button", { name: "Expand row" })[0],
    );
    expect(screen.getByText("Details for Alpha")).toBeInTheDocument();

    // What a refetch looks like: same row ids, new array and row objects, one
    // drifting field (Docker's "Up 4 minutes" status). TanStack's auto-reset
    // runs in a queueMicrotask, so the collapse this guards against only shows
    // up after the flush that the async act below performs.
    await act(async () => {
      view.rerender(
        <TestTable
          data={tableRows.map((row) =>
            row.id === "one" ? { ...row, status: "running (2m)" } : { ...row },
          )}
          expandedContent={renderExpandedContent}
        />,
      );
    });

    expect(screen.getByText("Details for Alpha")).toBeInTheDocument();
  });
});
