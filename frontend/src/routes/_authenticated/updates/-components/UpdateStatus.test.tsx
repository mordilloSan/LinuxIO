import { describe, expect, it, vi } from "vitest";

import type { Update } from "@/api";
import { render, screen } from "@/test/render";

import UpdateStatus from "./UpdateStatus";

const baseProps = {
  onUpdateOne: vi.fn(),
  progress: 0,
  updates: [] as Update[],
};

describe("UpdateStatus", () => {
  it("shows no progress panel while recovery is still scanning", () => {
    render(
      <UpdateStatus
        {...baseProps}
        onCancel={vi.fn()}
        recoveryPending
        updatingPackage={null}
      />,
    );

    // recoveryPending is true on every entry into the section; reporting
    // "Preparing…" before the scan finds a job invents an update.
    expect(screen.queryByText("Preparing...")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Cancel update" }),
    ).not.toBeInTheDocument();
  });

  it("keeps cancel available while a recovered update is resuming", async () => {
    const onCancel = vi.fn();
    const { user } = render(
      <UpdateStatus
        {...baseProps}
        onCancel={onCancel}
        recoveryPending
        status="Resuming update transaction"
        updatingPackage="nginx"
      />,
    );

    expect(
      screen.getByText("Resuming update transaction: nginx"),
    ).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Cancel update" }));
    expect(onCancel).toHaveBeenCalledTimes(1);
  });
});
