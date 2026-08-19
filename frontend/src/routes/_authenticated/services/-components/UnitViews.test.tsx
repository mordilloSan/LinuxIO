import { beforeEach, describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import { UnitTableView } from "./UnitViews";

const mocks = vi.hoisted(() => ({
  isMobile: false,
  useSortable: vi.fn(() => ({
    attributes: {},
    isDragging: false,
    listeners: undefined,
    setNodeRef: () => undefined,
    transform: null,
    transition: undefined,
  })),
}));

vi.mock("@dnd-kit/sortable", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@dnd-kit/sortable")>();
  return { ...actual, useSortable: mocks.useSortable };
});

vi.mock("@/theme", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/theme")>();
  return {
    ...actual,
    useAppMediaQuery: () => mocks.isMobile,
  };
});

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
const renderMainRow = vi.fn((row: UnitRow) => [row.name, row.status]);
const initialRows: UnitRow[] = [
  { id: "one", name: "Alpha", status: "running" },
  { id: "two", name: "Beta", status: "stopped" },
];

function TestUnitTable({
  data,
  onSelect,
}: {
  data: UnitRow[];
  onSelect?: (key: string | number | null) => void;
}) {
  return (
    <UnitTableView
      data={data}
      desktopColumns={tableColumns}
      emptyMessage="No units"
      getRowKey={getRowKey}
      mobileColumns={tableColumns}
      onSelect={onSelect}
      renderMainRow={renderMainRow}
    />
  );
}

describe("UnitTableView", () => {
  beforeEach(() => {
    mocks.isMobile = false;
    mocks.useSortable.mockClear();
    renderMainRow.mockClear();
  });

  it("reuses unchanged formatted rows and refreshes changed or reordered rows", () => {
    const view = render(<TestUnitTable data={initialRows} />);

    expect(renderMainRow).toHaveBeenCalledTimes(2);
    expect(mocks.useSortable).toHaveBeenCalledTimes(2);

    view.rerender(<TestUnitTable data={[...initialRows]} />);
    expect(renderMainRow).toHaveBeenCalledTimes(2);
    expect(mocks.useSortable).toHaveBeenCalledTimes(2);

    const changedSecondRow = { ...initialRows[1], status: "running" };
    view.rerender(<TestUnitTable data={[initialRows[0], changedSecondRow]} />);

    expect(screen.getAllByText("running")).toHaveLength(2);
    expect(renderMainRow).toHaveBeenCalledTimes(3);
    expect(mocks.useSortable).toHaveBeenCalledTimes(3);

    view.rerender(<TestUnitTable data={[changedSecondRow, initialRows[0]]} />);

    expect(renderMainRow).toHaveBeenCalledTimes(5);
    expect(mocks.useSortable).toHaveBeenCalledTimes(5);
  });

  it("opens the focused unit layout from a mobile row click", async () => {
    mocks.isMobile = true;
    const onSelect = vi.fn();
    const { user } = render(
      <TestUnitTable data={initialRows} onSelect={onSelect} />,
    );

    await user.click(screen.getByText("Alpha"));

    expect(onSelect).toHaveBeenCalledWith("one");
  });
});
