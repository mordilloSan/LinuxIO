import { beforeEach, describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import { UnitCardActions, UnitTableView } from "./UnitViews";

const mocks = vi.hoisted(() => ({
  isMobile: false,
  mutate: vi.fn(),
}));

vi.mock("@/hooks/useMediaQuery", () => ({
  useMediaQuery: () => mocks.isMobile,
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  const { mockMutationHooks } = await import("@/test/mutationHooks");
  const useCallMutation = () => ({ mutate: mocks.mutate, isPending: false });

  return {
    ...actual,
    useCallMutation,
    linuxio: mockMutationHooks(actual.linuxio, useCallMutation),
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
    renderMainRow.mockClear();
  });

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

describe("UnitCardActions", () => {
  it("offers every lifecycle action as its own named icon button", () => {
    render(
      <UnitCardActions
        activeState="active"
        info={undefined}
        unitFileState="enabled"
        unitName="nginx.service"
      />,
    );

    for (const label of [
      "Stop nginx.service",
      "Restart nginx.service",
      "Reload nginx.service",
      "Disable nginx.service",
      "Mask nginx.service",
    ]) {
      expect(screen.getByRole("button", { name: label })).toBeEnabled();
    }
  });

  it.each(["Stop", "Restart", "Disable", "Mask"])(
    "asks before %s on a LinuxIO unit and acts only once confirmed",
    async (verb) => {
      mocks.mutate.mockClear();
      const { user } = render(
        <UnitCardActions
          activeState="active"
          info={undefined}
          unitFileState="enabled"
          unitName="linuxio-bridge.service"
        />,
      );

      await user.click(
        screen.getByRole("button", { name: `${verb} linuxio-bridge.service` }),
      );

      expect(mocks.mutate).not.toHaveBeenCalled();
      expect(screen.getByText(/part of LinuxIO/)).toBeInTheDocument();
      expect(
        screen.getByText(/systemctl start linuxio\.target/),
      ).toBeInTheDocument();

      await user.click(screen.getByRole("button", { name: verb }));

      expect(mocks.mutate).toHaveBeenCalledWith({
        serviceName: "linuxio-bridge.service",
      });
    },
  );

  it("does not act when the warning is cancelled", async () => {
    mocks.mutate.mockClear();
    const { user } = render(
      <UnitCardActions
        activeState="active"
        info={undefined}
        unitFileState="enabled"
        unitName="linuxio.target"
      />,
    );

    await user.click(
      screen.getByRole("button", { name: "Stop linuxio.target" }),
    );
    await user.click(screen.getByRole("button", { name: "Cancel" }));

    expect(mocks.mutate).not.toHaveBeenCalled();
  });

  it("asks nothing for Start, Enable, Reload or Unmask on a LinuxIO unit", async () => {
    mocks.mutate.mockClear();
    const { user } = render(
      <UnitCardActions
        activeState="active"
        info={undefined}
        unitFileState="disabled"
        unitName="linuxio-auth.service"
      />,
    );

    await user.click(
      screen.getByRole("button", { name: "Reload linuxio-auth.service" }),
    );
    await user.click(
      screen.getByRole("button", { name: "Enable linuxio-auth.service" }),
    );

    expect(mocks.mutate).toHaveBeenCalledTimes(2);
    expect(screen.queryByText(/part of LinuxIO/)).not.toBeInTheDocument();
  });

  it("acts at once on a unit that is not part of LinuxIO", async () => {
    mocks.mutate.mockClear();
    const { user } = render(
      <UnitCardActions
        activeState="active"
        info={undefined}
        unitFileState="enabled"
        unitName="nginx.service"
      />,
    );

    await user.click(
      screen.getByRole("button", { name: "Stop nginx.service" }),
    );

    expect(mocks.mutate).toHaveBeenCalledWith({ serviceName: "nginx.service" });
    expect(screen.queryByText(/part of LinuxIO/)).not.toBeInTheDocument();
  });

  it("guards the running-only actions on an inactive unit", () => {
    render(
      <UnitCardActions
        activeState="inactive"
        info={undefined}
        unitFileState="disabled"
        unitName="nginx.service"
      />,
    );

    expect(
      screen.getByRole("button", { name: "Start nginx.service" }),
    ).toBeEnabled();
    expect(
      screen.getByRole("button", { name: "Restart nginx.service" }),
    ).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "Enable nginx.service" }),
    ).toBeEnabled();
  });
});
