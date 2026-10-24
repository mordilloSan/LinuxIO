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
    // "Preparing…" before the scan finds a task invents an update.
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

  it("keeps progress visible without cancel after cancellation is requested", () => {
    render(
      <UpdateStatus
        {...baseProps}
        canCancel={false}
        isUpdating
        onCancel={vi.fn()}
        progress={50}
        status="Installing"
        updatingPackage="nginx"
      />,
    );

    expect(screen.getByText("Installing: nginx")).toBeInTheDocument();
    expect(screen.getByRole("progressbar")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Cancel update" }),
    ).not.toBeInTheDocument();
  });

  it("keeps the package manager output behind a toggle, also after a failure", async () => {
    const output = [
      { id: 0, stream: "stdout" as const, text: "Unpacking gh (2.103.2) ..." },
      { id: 1, stream: "stdout" as const, text: "dpkg: error processing gh" },
    ];
    const { user } = render(
      <UpdateStatus
        {...baseProps}
        error="Failed to update gh: PackageKit error 1"
        output={output}
        updatingPackage={null}
      />,
    );

    expect(
      screen.getByText("Failed to update gh: PackageKit error 1"),
    ).toBeInTheDocument();
    await user.click(
      screen.getByRole("button", { name: "Show update output" }),
    );
    expect(screen.getByText("dpkg: error processing gh")).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Hide update output" }),
    ).toHaveAttribute("aria-expanded", "true");
  });

  it("shows no output toggle before the package manager has written anything", () => {
    render(
      <UpdateStatus
        {...baseProps}
        isUpdating
        output={[]}
        status="Download"
        updatingPackage="nginx"
      />,
    );

    expect(
      screen.queryByRole("button", { name: "Show update output" }),
    ).not.toBeInTheDocument();
  });
});
