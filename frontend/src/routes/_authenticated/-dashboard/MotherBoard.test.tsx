import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import MotherBoardInfo from "./MotherBoard";

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQuery: () => ({
      data: undefined,
      error: new Error("unable to read motherboard info"),
      isError: true,
    }),
  };
});

vi.mock("@/hooks/useCapabilities", () => ({
  useCapability: () => ({ isEnabled: true }),
}));

vi.mock("@/components/cards/DashboardCard", () => ({
  CardBadge: ({ text }: { text: string }) => <span>{text}</span>,
  default: ({
    headerExtras,
    stats,
  }: {
    headerExtras: ReactNode;
    stats: ReactNode;
  }) => (
    <section>
      {headerExtras}
      {stats}
    </section>
  ),
}));

vi.mock("./DashboardStatRows", () => ({
  default: ({ rows }: { rows: Array<{ label: string; value: string }> }) => (
    <dl>
      {rows.map((row) => (
        <div key={row.label}>
          <dt>{row.label}</dt>
          <dd>{row.value}</dd>
        </div>
      ))}
    </dl>
  ),
}));

describe("MotherBoardInfo", () => {
  it("renders N/A when an older backend rejects the motherboard query", () => {
    render(<MotherBoardInfo />);

    expect(screen.getAllByText("N/A")).toHaveLength(3);
    expect(screen.getByText("Board")).toBeInTheDocument();
    expect(screen.getByText("BIOS")).toBeInTheDocument();
  });
});
