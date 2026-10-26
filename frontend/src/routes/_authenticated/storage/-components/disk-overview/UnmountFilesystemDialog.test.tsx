import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { render } from "@/test/render";

import UnmountFilesystemDialog from "./UnmountFilesystemDialog";

const renderDialog = (props: { isPending?: boolean; onConfirm?: () => void }) =>
  render(
    <UnmountFilesystemDialog
      isPending={props.isPending ?? false}
      mountpoint="/srv/media"
      onClose={vi.fn()}
      onConfirm={props.onConfirm ?? vi.fn()}
    />,
  );

describe("UnmountFilesystemDialog", () => {
  it.each([
    [false, ""],
    [true, "true"],
  ])(
    "requests removeFstab for a ticked=%s box",
    async (ticked, removeFstab) => {
      const user = userEvent.setup();
      const onConfirm = vi.fn();
      renderDialog({ onConfirm });

      const box = screen.getByLabelText("Also remove from /etc/fstab");
      expect(box).not.toBeChecked();
      if (ticked) await user.click(box);
      await user.click(screen.getByRole("button", { name: "Unmount" }));

      expect(onConfirm).toHaveBeenCalledWith({
        mountpoint: "/srv/media",
        removeFstab,
      });
    },
  );

  it("cannot be cancelled while the unmount runs", () => {
    renderDialog({ isPending: true });
    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "Unmounting..." }),
    ).toBeDisabled();
  });
});
