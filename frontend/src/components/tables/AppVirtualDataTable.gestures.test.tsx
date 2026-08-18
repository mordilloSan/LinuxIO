import { useState, type ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import AppVirtualDataTable from "@/components/tables/AppVirtualDataTable";
import type { AppVirtualDataTableColumnDef } from "@/components/tables/AppVirtualDataTable";
import AppCheckbox from "@/components/ui/AppCheckbox";
import { useReorderableSurface } from "@/hooks/useReorderableSurface";
import { useReorderableTableDnd } from "@/hooks/useReorderableTableDnd";
import { act, fireEvent, render, screen, waitFor } from "@/test/render";

vi.mock("@tanstack/react-virtual", async () =>
  (await import("@/test/reactVirtualMock")).reactVirtualMock(),
);

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
const getRowAttributes = vi.fn(() => ({}));

const tableColumns: AppVirtualDataTableColumnDef<TableRow>[] = [
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

const tableRows: TableRow[] = [
  { id: "one", name: "Alpha", status: "running" },
  { id: "two", name: "Beta", status: "stopped" },
];

const getTableRowId = (row: TableRow) => row.id;

function TestTable({
  columns = tableColumns,
  data = tableRows,
  expandedContent,
  onClearSelection,
  onRowClick,
  onRowDoubleClick,
  selectedRowId,
}: {
  columns?: AppVirtualDataTableColumnDef<TableRow>[];
  data?: TableRow[];
  expandedContent?: (row: { original: TableRow }) => ReactNode;
  onClearSelection?: () => void;
  onRowClick?: () => void;
  onRowDoubleClick?: () => void;
  selectedRowId?: string;
}) {
  return (
    <AppVirtualDataTable
      columns={columns}
      data={data}
      fillAvailable
      getRowAttributes={getRowAttributes}
      getRowId={getTableRowId}
      onClearSelection={onClearSelection}
      onRowClick={onRowClick}
      onRowDoubleClick={onRowDoubleClick}
      renderExpandedContent={expandedContent}
      selectedRowId={selectedRowId}
    />
  );
}

function SelectableTable() {
  const [selected, setSelected] = useState<Set<string>>(() => new Set());
  const columns: AppVirtualDataTableColumnDef<SelectableRow>[] = [
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
    <AppVirtualDataTable
      ariaLabel="Selectable rows"
      columns={columns}
      data={rows}
      fillAvailable
      getRowId={(row) => row.id}
    />
  );
}

/**
 * The docker list tables: hold-to-reorder rows, an expandable detail row, and a
 * leading checkbox column whose defs are rebuilt whenever the selection changes.
 * Arming the hold re-renders the surface, and a rebuilt column def replaces the
 * pressed input's DOM node — which used to swallow the click outright.
 */
function ReorderableSelectableTable() {
  const [selected, setSelected] = useState<Set<string>>(() => new Set());
  const surface = useReorderableSurface({
    getId: getTableRowId,
    items: tableRows,
    surface: "test.rows",
  });
  const dnd = useReorderableTableDnd<TableRow, TableRow>({ surface });
  const columns: AppVirtualDataTableColumnDef<TableRow>[] = [
    {
      id: "select",
      header: "Select",
      cell: ({ row }) => (
        <AppCheckbox
          aria-label={`Select ${row.original.name}`}
          checked={selected.has(row.original.id)}
          onChange={(event) =>
            setSelected((current) => {
              const next = new Set(current);
              if (event.target.checked) next.add(row.original.id);
              else next.delete(row.original.id);
              return next;
            })
          }
          size="small"
        />
      ),
      meta: { align: "center", width: "40px" },
    },
    tableColumns[0],
  ];

  return (
    <>
      <div data-testid="selected-count">{selected.size}</div>
      <AppVirtualDataTable
        columns={columns}
        data={tableRows}
        dnd={dnd}
        fillAvailable
        getRowId={getTableRowId}
        renderExpandedContent={({ original }) => (
          <div>{`Details for ${original.name}`}</div>
        )}
      />
    </>
  );
}

// The row gestures from docs/table-row-gestures.md, exercised against the one
// table primitive. Ported from the retired non-virtualized AppDataTable suite.
describe("AppVirtualDataTable gestures", () => {
  beforeEach(() => {
    getRowAttributes.mockClear();
    renderName.mockClear();
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

  it("isolates field updates to affected cells", () => {
    const view = render(<TestTable />);

    expect(renderName).toHaveBeenCalledTimes(2);
    expect(renderStatus).toHaveBeenCalledTimes(2);

    view.rerender(<TestTable selectedRowId="two" />);

    expect(screen.getByText("Beta").closest('[role="row"]')).toHaveClass(
      "app-dt__row--selected",
    );
    // Selection chrome updates without re-running any cell formatter.
    expect(renderName).toHaveBeenCalledTimes(2);
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

  it("updates index-sensitive cells when stable rows are reordered", () => {
    // Per the column-meta contract, an explicit render key must include the
    // row index when the renderer reads its position.
    const indexedColumns: AppVirtualDataTableColumnDef<TableRow>[] = [
      {
        id: "name",
        header: "Name",
        cell: ({ row }) => `${row.index}:${row.original.name}`,
        meta: {
          getCellRenderKey: (row, rowIndex) => [(row as TableRow).id, rowIndex],
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
    expect(renderExpandedContent).toHaveBeenCalled();
    expect(screen.queryByText("Details for Beta")).not.toBeInTheDocument();
  });

  it("leaves clicks on row controls to the control", async () => {
    const onControlClick = vi.fn();
    const columnsWithAction: AppVirtualDataTableColumnDef<TableRow>[] = [
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

  it("waits out the double-click window before expanding, then expands", async () => {
    const onRowDoubleClick = vi.fn();
    const view = render(
      <TestTable
        expandedContent={({ original }) => (
          <div>{`Details for ${original.name}`}</div>
        )}
        onRowDoubleClick={onRowDoubleClick}
      />,
    );

    await view.user.click(screen.getByText("Alpha").closest('[role="row"]')!);

    // Still closed the instant the click lands — the second click of a double
    // click would otherwise close it again.
    expect(screen.queryByText("Details for Alpha")).not.toBeInTheDocument();
    await waitFor(() =>
      expect(screen.getByText("Details for Alpha")).toBeInTheDocument(),
    );
    expect(onRowDoubleClick).not.toHaveBeenCalled();
  });

  it("double-clicking a row runs its gesture and leaves the panel closed", async () => {
    const onRowDoubleClick = vi.fn();
    const view = render(
      <TestTable
        expandedContent={({ original }) => (
          <div>{`Details for ${original.name}`}</div>
        )}
        onRowDoubleClick={onRowDoubleClick}
      />,
    );

    await view.user.dblClick(
      screen.getByText("Alpha").closest('[role="row"]')!,
    );

    expect(onRowDoubleClick).toHaveBeenCalledTimes(1);
    // The deferred expand from the first click was cancelled, so the panel
    // never flashes open.
    await expect.poll(() => screen.queryByText("Details for Alpha")).toBeNull();
  });

  it("expands with no delay when no double-click gesture is bound", async () => {
    const view = render(
      <TestTable
        expandedContent={({ original }) => (
          <div>{`Details for ${original.name}`}</div>
        )}
      />,
    );

    await view.user.click(screen.getByText("Alpha").closest('[role="row"]')!);

    expect(screen.getByText("Details for Alpha")).toBeInTheDocument();
  });

  it("collapses every expanded row on Escape, then clears the selection", async () => {
    const onClearSelection = vi.fn();
    const view = render(
      <TestTable
        expandedContent={({ original }) => (
          <div>{`Details for ${original.name}`}</div>
        )}
        onClearSelection={onClearSelection}
      />,
    );

    await view.user.click(
      screen.getAllByRole("button", { name: "Expand row" })[0],
    );
    await view.user.click(
      screen.getAllByRole("button", { name: "Expand row" })[0],
    );
    expect(screen.getByText("Details for Alpha")).toBeInTheDocument();
    expect(screen.getByText("Details for Beta")).toBeInTheDocument();

    await view.user.keyboard("{Escape}");

    // The first press bought the collapse, not the selection.
    expect(onClearSelection).not.toHaveBeenCalled();
    // Expanded state drops synchronously; the panels then animate out.
    await waitFor(() =>
      expect(screen.queryByText("Details for Alpha")).not.toBeInTheDocument(),
    );

    await view.user.keyboard("{Escape}");

    expect(onClearSelection).toHaveBeenCalledTimes(1);
  });

  it("clears the selection on the first Escape when nothing is expanded", async () => {
    const onClearSelection = vi.fn();
    const view = render(
      <TestTable
        expandedContent={({ original }) => (
          <div>{`Details for ${original.name}`}</div>
        )}
        onClearSelection={onClearSelection}
      />,
    );

    await view.user.keyboard("{Escape}");

    expect(onClearSelection).toHaveBeenCalledTimes(1);
  });

  it("leaves Escape alone while a dialog owns it", async () => {
    const onClearSelection = vi.fn();
    const view = render(
      <>
        <div className="app-dialog-root" />
        <TestTable onClearSelection={onClearSelection} />
      </>,
    );

    await view.user.keyboard("{Escape}");

    expect(onClearSelection).not.toHaveBeenCalled();
  });

  it("suppresses only the word selection a double click would start", () => {
    // jsdom does not implement double-click word selection, so this asserts the
    // mechanism: the second mousedown is what starts it, and that is the one
    // default-prevented.
    const withGesture = render(<TestTable onRowDoubleClick={vi.fn()} />);
    const row = () => screen.getByText("Alpha").closest('[role="row"]')!;

    // fireEvent returns false once preventDefault has been called.
    expect(fireEvent.mouseDown(row(), { detail: 2 })).toBe(false);
    // The first press of a click still selects text, so drag-selecting an id
    // out of a cell keeps working.
    expect(fireEvent.mouseDown(row(), { detail: 1 })).toBe(true);

    withGesture.unmount();

    // A table with no double-click gesture never interferes.
    render(<TestTable />);
    expect(fireEvent.mouseDown(row(), { detail: 2 })).toBe(true);
  });

  it("leaves the word selection alone on a control inside the row", () => {
    const columnsWithAction: AppVirtualDataTableColumnDef<TableRow>[] = [
      tableColumns[0],
      {
        id: "action",
        header: "Action",
        cell: ({ row }) => (
          <button type="button">{`Restart ${row.original.name}`}</button>
        ),
      },
    ];
    render(
      <TestTable columns={columnsWithAction} onRowDoubleClick={vi.fn()} />,
    );

    const button = screen.getByRole("button", { name: "Restart Alpha" });

    expect(fireEvent.mouseDown(button, { detail: 2 })).toBe(true);
  });

  it("selects on the first checkbox press in a reorderable row", async () => {
    const view = render(<ReorderableSelectableTable />);
    const checkbox = screen.getByRole("checkbox", { name: "Select Alpha" });

    await view.user.click(checkbox);

    expect(screen.getByTestId("selected-count")).toHaveTextContent("1");
    expect(checkbox).toBeChecked();
    // The press belonged to the checkbox, so it neither armed the reorder hold
    // nor toggled the row.
    expect(screen.queryByText("Details for Alpha")).not.toBeInTheDocument();
    expect(screen.getByText("Alpha").closest('[role="row"]')).not.toHaveClass(
      "app-dt__row--reorder-pending",
    );
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
