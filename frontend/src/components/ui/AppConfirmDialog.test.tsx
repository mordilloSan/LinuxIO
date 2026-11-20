import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import AppConfirmDialog from "./AppConfirmDialog";

const renderDialog = (props: {
  closeWhilePending?: boolean;
  isPending?: boolean;
}) => {
  const onClose = vi.fn();
  const onConfirm = vi.fn();
  render(
    <AppConfirmDialog
      confirmText="Delete"
      message="Delete 3 items?"
      onClose={onClose}
      onConfirm={onConfirm}
      open
      title="Delete"
      {...props}
    />,
  );
  return { onClose, onConfirm, user: userEvent.setup() };
};

describe("AppConfirmDialog", () => {
  it("holds itself open while a short write is pending", async () => {
    const { onClose, onConfirm, user } = renderDialog({ isPending: true });

    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
    await user.keyboard("{Escape}");
    await user.keyboard("{Enter}");

    expect(onClose).not.toHaveBeenCalled();
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it("can be closed while a long job runs when the caller allows it", async () => {
    const { onClose, onConfirm, user } = renderDialog({
      closeWhilePending: true,
      isPending: true,
    });

    // The job cannot be started twice, and the first button now closes.
    expect(screen.getByRole("button", { name: "Deleting…" })).toBeDisabled();
    expect(
      screen.queryByRole("button", { name: "Cancel" }),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Close" }));
    expect(onClose).toHaveBeenCalledTimes(1);

    await user.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalledTimes(2);
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it("moves focus off the confirm button once its job runs, and Enter then closes", async () => {
    const onClose = vi.fn();
    const Confirm = ({ isPending }: { isPending: boolean }) => (
      <AppConfirmDialog
        closeWhilePending
        confirmText="Delete"
        isPending={isPending}
        message="Delete 3 items?"
        onClose={onClose}
        onConfirm={vi.fn()}
        open
        title="Delete"
      />
    );
    const { rerender } = render(<Confirm isPending={false} />);
    const user = userEvent.setup();
    screen.getByRole("button", { name: "Delete" }).focus();

    rerender(<Confirm isPending />);
    // The button that had focus is disabled now: the dialog takes it.
    expect(screen.getByRole("dialog")).toHaveFocus();

    await user.tab();
    expect(screen.getByRole("button", { name: "Close" })).toHaveFocus();
    await user.keyboard("{Enter}");
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("offers Cancel and confirms when nothing is pending", async () => {
    const { onClose, onConfirm, user } = renderDialog({
      closeWhilePending: true,
    });

    await user.click(screen.getByRole("button", { name: "Delete" }));
    expect(onConfirm).toHaveBeenCalledTimes(1);
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
