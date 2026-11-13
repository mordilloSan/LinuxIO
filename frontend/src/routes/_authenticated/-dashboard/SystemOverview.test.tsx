import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import SystemOverview from "./SystemOverview";

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  const hostInfo = {
    hostname: "nas",
    platform: "debian",
    platformVersion: "13",
  };
  return {
    ...actual,
    useSuspenseQueries: () => [
      { data: hostInfo },
      { data: { uptime_seconds: 3600 } },
      { data: "2026-11-13T10:00:00Z" },
    ],
    useSuspenseQuery: () => ({ data: "debian" }),
  };
});

vi.mock("@/routes/_authenticated/-dashboard/DashboardCard", () => ({
  default: ({ stats }: { stats: ReactNode }) => <section>{stats}</section>,
}));

vi.mock("./DashboardStatRows", () => ({
  default: ({
    rows,
  }: {
    rows: Array<{ label: string; onEdit?: () => void; value: string }>;
  }) => (
    <dl>
      {rows.map((row) => (
        <div key={row.label}>
          <dt>{row.label}</dt>
          <dd>{row.value}</dd>
          {row.onEdit ? (
            <button onClick={row.onEdit} type="button">
              Edit {row.label}
            </button>
          ) : null}
        </div>
      ))}
    </dl>
  ),
}));

vi.mock("./SetHostnameDialog", () => ({ default: () => null }));
vi.mock("./SetDateTimeDialog", () => ({ default: () => null }));

describe("SystemOverview", () => {
  it("lets a session with sudo edit the hostname and the clock", () => {
    render(<SystemOverview />, { auth: { privileged: true } });

    expect(
      screen.getByRole("button", { name: "Edit Hostname" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Edit Date / Time" }),
    ).toBeInTheDocument();
  });

  it("shows the hostname and the clock read-only without sudo", () => {
    render(<SystemOverview />, { auth: { privileged: false } });

    expect(screen.getByText("nas")).toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });
});
