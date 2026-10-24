import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import * as core from "@/api/linuxio-core";
import { fireEvent, render, screen } from "@/test/render";

import VirtualMachinesCard from "./VirtualMachines";

const vms = [
  { name: "web", state: "shutoff", vcpus: 4, memoryMB: 4096, diskGB: 40 },
  {
    name: "db",
    state: "running",
    vcpus: 2,
    memoryMB: 2048,
    diskGB: 20,
    nics: [{ ipAddresses: ["10.0.0.5"] }],
  },
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

vi.mock("@/routes/_authenticated/-dashboard/DashboardCard", () => ({
  default: ({ stats, stats2 }: { stats: ReactNode; stats2: ReactNode }) => (
    <section>
      {stats}
      {stats2}
    </section>
  ),
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn(), warning: vi.fn() },
}));

describe("dashboard VirtualMachines card", () => {
  it("sums running machines and lists running ones first", () => {
    render(<VirtualMachinesCard />);

    expect(screen.getByText("1/2")).toBeInTheDocument();
    expect(screen.getByText("2")).toBeInTheDocument();
    expect(screen.getByText("2 GB")).toBeInTheDocument();
    expect(screen.getByText("60 GB")).toBeInTheDocument();
    expect(
      screen
        .getAllByRole("listitem")
        .map((item) => item.getAttribute("aria-label")),
    ).toEqual(["db", "web"]);
  });

  it("offers state-aware power actions from a row's context menu", async () => {
    const request = vi
      .spyOn(core, "request")
      .mockReturnValue(new Promise(() => undefined));
    const { user } = render(<VirtualMachinesCard />);

    fireEvent.contextMenu(screen.getByRole("listitem", { name: "db" }));
    expect(
      screen.getAllByRole("menuitem").map((item) => item.textContent),
    ).toEqual(["Shutdown", "Reboot", "Suspend", "Force off"]);
    await user.keyboard("{Escape}");

    fireEvent.contextMenu(screen.getByRole("listitem", { name: "web" }));
    expect(
      screen.getAllByRole("menuitem").map((item) => item.textContent),
    ).toEqual(["Start"]);
    await user.click(screen.getByRole("menuitem", { name: "Start" }));

    expect(request.mock.calls[0]?.slice(0, 3)).toEqual([
      "virt",
      "start",
      { name: "web" },
    ]);
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(
      await screen.findByRole("progressbar", { name: "Starting web" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("listitem", { name: "web: Starting" }),
    ).toHaveAttribute("aria-busy", "true");
  });
});
