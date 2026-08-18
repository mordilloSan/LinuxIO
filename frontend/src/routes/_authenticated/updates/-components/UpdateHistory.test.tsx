import { beforeEach, describe, expect, it, vi } from "vitest";

import type { UpdateHistoryRow } from "@/api";
import { render, screen } from "@/test/render";

import UpdateHistory from "./UpdateHistory";

const mocks = vi.hoisted(() => ({
  rows: [] as UpdateHistoryRow[],
}));

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
});
