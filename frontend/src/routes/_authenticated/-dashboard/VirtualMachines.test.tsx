import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import VirtualMachinesCard from "./VirtualMachines";

const vms = [
  { name: "web", state: "shutoff", vcpus: 4, memoryMB: 4096, diskGB: 40 },
  { name: "db", state: "running", vcpus: 2, memoryMB: 2048, diskGB: 20 },
];

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useSuspenseQuery: (options: { select?: (vms: unknown) => unknown }) => ({
      data: options.select ? options.select(vms) : vms,
    }),
  };
});

vi.mock("@/components/cards/DashboardCard", () => ({
  default: ({ stats, stats2 }: { stats: ReactNode; stats2: ReactNode }) => (
    <section>
      {stats}
      {stats2}
    </section>
  ),
}));

describe("dashboard VirtualMachines card", () => {
  it("sums running machines and lists running ones first", () => {
    render(<VirtualMachinesCard />);

    expect(screen.getByText("1/2")).toBeInTheDocument();
    expect(screen.getByText("2")).toBeInTheDocument();
    expect(screen.getByText("2 GB")).toBeInTheDocument();
    expect(screen.getByText("60 GB")).toBeInTheDocument();
    expect(
      screen.getAllByRole("listitem").map((item) => item.textContent),
    ).toEqual(["db", "web"]);
  });
});
