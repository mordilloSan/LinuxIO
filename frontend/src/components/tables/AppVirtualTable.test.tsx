import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import { render, screen } from "@/test/render";
import { TABLE_ROW_MIN_HEIGHT } from "@/theme/constants";

const virtualizerSpies = vi.hoisted(() => ({
  isScrolling: false,
  measure: vi.fn(),
  options: undefined as
    | {
        estimateSize: (index: number) => number;
        getItemKey: (index: number) => string | number;
        useAnimationFrameWithResizeObserver?: boolean;
      }
    | undefined,
}));

vi.mock("@tanstack/react-virtual", async () => {
  const { useRef } = await vi.importActual<typeof import("react")>("react");

  return {
    useVirtualizer: ({
      count,
      estimateSize,
      getItemKey,
      useAnimationFrameWithResizeObserver,
    }: {
      count: number;
      estimateSize: (index: number) => number;
      getItemKey: (index: number) => string | number;
      useAnimationFrameWithResizeObserver?: boolean;
    }) => {
      const optionsRef = useRef({ count, estimateSize, getItemKey });
      optionsRef.current = { count, estimateSize, getItemKey };
      virtualizerSpies.options = {
        estimateSize,
        getItemKey,
        useAnimationFrameWithResizeObserver,
      };

      const virtualizerRef = useRef<{
        getTotalSize: () => number;
        getVirtualItems: () => Array<{
          end: number;
          index: number;
          key: string | number;
          lane: number;
          size: number;
          start: number;
        }>;
        isScrolling: boolean;
        measure: typeof virtualizerSpies.measure;
        measureElement: ReturnType<typeof vi.fn>;
        resizeItem: ReturnType<typeof vi.fn>;
        scrollToIndex: ReturnType<typeof vi.fn>;
      } | null>(null);

      if (!virtualizerRef.current) {
        virtualizerRef.current = {
          getTotalSize: () => optionsRef.current.count * 48,
          getVirtualItems: () =>
            Array.from({ length: optionsRef.current.count }, (_, index) => ({
              end: (index + 1) * 48,
              index,
              key: optionsRef.current.getItemKey(index),
              lane: 0,
              size: 48,
              start: index * 48,
            })),
          get isScrolling() {
            return virtualizerSpies.isScrolling;
          },
          measure: virtualizerSpies.measure,
          measureElement: vi.fn(),
          resizeItem: vi.fn(),
          scrollToIndex: vi.fn(),
        };
      }

      return virtualizerRef.current;
    },
  };
});

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

const columns: AppVirtualTableColumnDef<TableRow>[] = [
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
  persistExpandedKey,
  selectedRowId,
  tableColumns = columns,
  estimateRowHeight,
}: {
  data?: TableRow[];
  expandedContent?: (row: { original: TableRow }) => ReactNode;
  persistExpandedKey?: string;
  selectedRowId?: string;
  tableColumns?: AppVirtualTableColumnDef<TableRow>[];
  estimateRowHeight?: number;
}) {
  return (
    <AppVirtualTable
      columns={tableColumns}
      data={data}
      estimateRowHeight={estimateRowHeight}
      fillAvailable={false}
      getRowId={(row) => row.id}
      height={200}
      persistExpandedKey={persistExpandedKey}
      renderExpandedContent={expandedContent}
      selectedRowId={selectedRowId}
    />
  );
}

describe("AppVirtualTable", () => {
  it("exposes the canonical row floor and clamps low virtual estimates", () => {
    const view = render(<TestTable estimateRowHeight={40} />);
    const table = screen.getByRole("table");

    expect(table).toHaveStyle(
      `--app-dt-row-min-height: ${TABLE_ROW_MIN_HEIGHT}px`,
    );
    expect(virtualizerSpies.options?.estimateSize(0)).toBe(
      TABLE_ROW_MIN_HEIGHT,
    );
    expect(virtualizerSpies.options?.useAnimationFrameWithResizeObserver).toBe(
      true,
    );

    view.rerender(<TestTable estimateRowHeight={72} />);
    expect(virtualizerSpies.options?.estimateSize(0)).toBe(72);
  });

  it("paints the row rhythm on the body only while scrolling", () => {
    virtualizerSpies.isScrolling = true;
    const view = render(<TestTable />);
    const body = view.container.querySelector(".app-dt__body");
    expect(body).toHaveClass("app-dt__body--scrolling");

    virtualizerSpies.isScrolling = false;
    view.rerender(<TestTable selectedRowId="one" />);
    expect(body).not.toHaveClass("app-dt__body--scrolling");
  });

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
    const replacementColumns: AppVirtualTableColumnDef<TableRow>[] = [
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

  it("keeps virtualizer callbacks stable across unrelated table renders", () => {
    const view = render(<TestTable />);
    const firstOptions = virtualizerSpies.options;

    view.rerender(<TestTable selectedRowId="two" />);

    expect(virtualizerSpies.options?.estimateSize).toBe(
      firstOptions?.estimateSize,
    );
    expect(virtualizerSpies.options?.getItemKey).toBe(firstOptions?.getItemKey);
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

  it("does not restart detail animation for a new renderer identity", async () => {
    const rectSpy = vi
      .spyOn(HTMLElement.prototype, "getBoundingClientRect")
      .mockImplementation(() => ({ height: 100 }) as DOMRect);
    const frameSpy = vi
      .spyOn(window, "requestAnimationFrame")
      .mockImplementation(() => 1);

    try {
      const view = render(
        <TestTable
          expandedContent={({ original }) => (
            <div>{`Details for ${original.name}`}</div>
          )}
        />,
      );
      await view.user.click(screen.getByText("Alpha").closest('[role="row"]')!);
      const framesAfterExpand = frameSpy.mock.calls.length;

      view.rerender(
        <TestTable
          expandedContent={({ original }) => (
            <div>{`Updated details for ${original.name}`}</div>
          )}
        />,
      );

      expect(screen.getByText("Updated details for Alpha")).toBeInTheDocument();
      expect(frameSpy).toHaveBeenCalledTimes(framesAfterExpand);

      view.unmount();
    } finally {
      frameSpy.mockRestore();
      rectSpy.mockRestore();
    }
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

  it("restores persisted expansion on a fresh mount", async () => {
    const expandedContent = ({ original }: { original: TableRow }) => (
      <div>{`Details for ${original.name}`}</div>
    );
    const view = render(
      <TestTable
        expandedContent={expandedContent}
        persistExpandedKey="test-table"
      />,
    );

    await view.user.click(screen.getByText("Alpha").closest('[role="row"]')!);
    expect(screen.getByText("Details for Alpha")).toBeInTheDocument();
    view.unmount();

    render(
      <TestTable
        expandedContent={expandedContent}
        persistExpandedKey="test-table"
      />,
    );
    expect(screen.getByText("Details for Alpha")).toBeInTheDocument();
    expect(screen.getByText("Alpha").closest('[role="row"]')).toHaveAttribute(
      "aria-expanded",
      "true",
    );
    expect(screen.queryByText("Details for Beta")).not.toBeInTheDocument();
  });

  it("persists a collapse, not just an expand", async () => {
    const expandedContent = ({ original }: { original: TableRow }) => (
      <div>{`Details for ${original.name}`}</div>
    );
    const view = render(
      <TestTable
        expandedContent={expandedContent}
        persistExpandedKey="test-table"
      />,
    );
    const row = screen.getByText("Alpha").closest('[role="row"]')!;

    await view.user.click(row);
    await view.user.click(row);
    view.unmount();

    render(
      <TestTable
        expandedContent={expandedContent}
        persistExpandedKey="test-table"
      />,
    );
    expect(screen.getByText("Alpha").closest('[role="row"]')).toHaveAttribute(
      "aria-expanded",
      "false",
    );
  });

  // A Go handler that accumulates into a nil slice sends JSON null even where
  // the generated contract promises an array. Rendering the empty state keeps
  // the surrounding widget alive instead of tripping its error boundary.
  it.each([
    ["null", null],
    ["undefined", undefined],
  ])("renders the empty state for %s data", (_label, data) => {
    render(
      <AppVirtualTable
        columns={columns}
        data={data}
        emptyMessage="No rows reported."
        fillAvailable={false}
        getRowId={(row) => row.id}
        maxHeight={280}
      />,
    );

    expect(screen.getByText("No rows reported.")).toBeInTheDocument();
    expect(screen.queryByText("Alpha")).not.toBeInTheDocument();
  });
});
