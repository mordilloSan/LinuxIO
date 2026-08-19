import type { RowData } from "@tanstack/react-table";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { UpdateHistoryRow } from "@/api";
import type { AppDataTableProps } from "@/components/tables/AppDataTable";
import { render, screen } from "@/test/render";

import UpdateHistory from "./UpdateHistory";

const mocks = vi.hoisted(() => ({
  nestedTableRender: vi.fn(),
  rows: [] as UpdateHistoryRow[],
}));

vi.mock("@/components/tables/AppDataTable", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/components/tables/AppDataTable")>();
  const OriginalAppDataTable = actual.default;
  const AppDataTable = <TData extends RowData>(
    props: AppDataTableProps<TData>,
  ) => {
    if (props.ariaLabel?.startsWith("Packages installed on ")) {
      mocks.nestedTableRender();
    }
    return <OriginalAppDataTable {...props} />;
  };

  return { ...actual, default: AppDataTable };
});

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useSuspenseQuery: () => ({ data: mocks.rows }),
  };
});

vi.mock("@tanstack/react-virtual", async () =>
  (await import("@/test/reactVirtualMock")).reactVirtualMock(),
);

describe("UpdateHistory", () => {
  beforeEach(() => {
    mocks.nestedTableRender.mockClear();
    mocks.rows = [
      { date: "2026-08-17", upgrades: [] },
      {
        date: "2026-08-18",
        upgrades: [{ package: "bash", version: "5.3" }],
      },
    ];
  });

  it("only offers expansion for history rows with packages", async () => {
    const { user } = render(<UpdateHistory />);
    const expandButtons = screen.getAllByRole("button", {
      name: "Expand row",
    });

    expect(expandButtons).toHaveLength(1);

    await user.click(expandButtons[0]);
    expect(screen.getByText("bash")).toBeInTheDocument();
    expect(screen.queryByText("No packages recorded.")).not.toBeInTheDocument();
  });

  it("does not rerender an unchanged expanded package table", async () => {
    const view = render(<UpdateHistory />);

    await view.user.click(
      screen.getByRole("button", {
        name: "Expand row",
      }),
    );
    expect(screen.getByText("bash")).toBeInTheDocument();
    expect(mocks.nestedTableRender).toHaveBeenCalledTimes(1);

    view.rerender(<UpdateHistory />);

    expect(mocks.nestedTableRender).toHaveBeenCalledTimes(1);
  });
});
