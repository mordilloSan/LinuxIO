import { useState, type ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import AppDataTable from "@/components/tables/AppDataTable";
import type { AppDataTableColumnDef } from "@/components/tables/AppDataTable";
import AppCheckbox from "@/components/ui/AppCheckbox";
import { useReorderableSurface } from "@/hooks/useReorderableSurface";
import { useReorderableTableDnd } from "@/hooks/useReorderableTableDnd";
import { act, fireEvent, render, screen, waitFor } from "@/test/render";

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
  onClearSelection,
  onRowClick,
  onRowDoubleClick,
  onSelectAll,
  selectedRowId,
  selectedRowIds,
}: {
  columns?: AppDataTableColumnDef<TableRow>[];
  data?: TableRow[];
  expandedContent?: (row: { original: TableRow }) => ReactNode;
  onClearSelection?: () => void;
  onRowClick?: () => void;
  onRowDoubleClick?: () => void;
  onSelectAll?: (rowIds: string[]) => void;
  selectedRowId?: string;
  selectedRowIds?: ReadonlySet<string>;
}) {
  return (
    <AppDataTable
      columns={columns}
      data={data}
      getRowAttributes={getRowAttributes}
      getRowId={getTableRowId}
      onClearSelection={onClearSelection}
      onRowClick={onRowClick}
      onRowDoubleClick={onRowDoubleClick}
      onSelectAll={onSelectAll}
      renderExpandedContent={expandedContent}
      selectedRowId={selectedRowId}
      selectedRowIds={selectedRowIds}
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
  const columns: AppDataTableColumnDef<TableRow>[] = [
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
      <AppDataTable
        columns={columns}
        data={tableRows}
        dnd={dnd}
        getRowId={getTableRowId}
        renderExpandedContent={({ original }) => (
          <div>{`Details for ${original.name}`}</div>
        )}
      />
    </>
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
      "app-dt__row--selected",
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

    expect(row).toHaveClass("app-dt__row--interactive");
    expect(row).toHaveAttribute("aria-expanded", "false");

    await view.user.click(row);
    expect(screen.getByText("Details for Alpha")).toBeInTheDocument();
    expect(row).toHaveAttribute("aria-expanded", "true");

    await view.user.click(row);
    // The row reports itself closed the moment the click lands...
    expect(row).toHaveAttribute("aria-expanded", "false");
    // ...but the panel has to stay mounted while it animates shut, so
    // AppCollapse's unmountOnExit only drops it a slow transition later
    // (TRANSITION_DURATION_SLOW_MS). Asserting its absence outright raced that
    // timer and failed whenever the run was quick enough to beat it.
    await expect
      .poll(() => screen.queryByText("Details for Alpha"), { timeout: 2000 })
      .toBeNull();
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

  it("double-clicking a row selects it and leaves the panel closed", async () => {
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
    expect(screen.queryByText("Details for Beta")).not.toBeInTheDocument();

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
    const columnsWithAction: AppDataTableColumnDef<TableRow>[] = [
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

  it("selects every visible row on Ctrl-A and on Cmd-A", async () => {
    const onSelectAll = vi.fn();
    const view = render(<TestTable onSelectAll={onSelectAll} />);

    await view.user.keyboard("{Control>}a{/Control}");
    expect(onSelectAll).toHaveBeenLastCalledWith(["one", "two"]);

    await view.user.keyboard("{Meta>}a{/Meta}");
    expect(onSelectAll).toHaveBeenCalledTimes(2);

    // Combos that mean something else stay inert.
    await view.user.keyboard("{Control>}{Shift>}a{/Shift}{/Control}");
    await view.user.keyboard("a");
    expect(onSelectAll).toHaveBeenCalledTimes(2);
  });

  it("names only the rows the current data leaves visible", async () => {
    const onSelectAll = vi.fn();
    const view = render(
      <TestTable data={[tableRows[1]]} onSelectAll={onSelectAll} />,
    );

    await view.user.keyboard("{Control>}a{/Control}");

    expect(onSelectAll).toHaveBeenCalledWith(["two"]);
  });

  it("leaves Ctrl-A to the field while the user is typing", async () => {
    const onSelectAll = vi.fn();
    const view = render(
      <>
        <input aria-label="Search" />
        <TestTable onSelectAll={onSelectAll} />
      </>,
    );

    await view.user.click(screen.getByRole("textbox", { name: "Search" }));
    await view.user.keyboard("{Control>}a{/Control}");

    expect(onSelectAll).not.toHaveBeenCalled();
  });

  it("tints every row named by selectedRowIds", () => {
    const view = render(<TestTable selectedRowIds={new Set(["one"])} />);
    const alphaRow = () => screen.getByText("Alpha").closest('[role="row"]');
    const betaRow = () => screen.getByText("Beta").closest('[role="row"]');

    expect(alphaRow()).toHaveClass("app-dt__row--selected");
    expect(betaRow()).not.toHaveClass("app-dt__row--selected");

    view.rerender(<TestTable selectedRowIds={new Set(["one", "two"])} />);

    expect(alphaRow()).toHaveClass("app-dt__row--selected");
    expect(betaRow()).toHaveClass("app-dt__row--selected");

    view.rerender(<TestTable selectedRowIds={new Set()} />);

    expect(alphaRow()).not.toHaveClass("app-dt__row--selected");
    expect(betaRow()).not.toHaveClass("app-dt__row--selected");
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
