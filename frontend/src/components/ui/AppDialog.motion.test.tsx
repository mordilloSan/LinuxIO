import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { AppDialog } from "@/components/ui/AppDialog";
import { render } from "@/test/render";

describe("AppDialog motion", () => {
  it("animates a dialog that mounts already open", async () => {
    const onEntered = vi.fn();
    render(
      <AppDialog open slotProps={{ transition: { onEntered } }}>
        Dialog
      </AppDialog>,
    );

    await waitFor(() => expect(onEntered).toHaveBeenCalledOnce());
  });

  it("keeps the dialog mounted through its exit and fires lifecycle callbacks", async () => {
    const onEntered = vi.fn();
    const onExited = vi.fn();
    const view = render(
      <AppDialog
        open={false}
        slotProps={{ transition: { onEntered, onExited } }}
      >
        Dialog
      </AppDialog>,
    );

    view.rerender(
      <AppDialog open slotProps={{ transition: { onEntered, onExited } }}>
        Dialog
      </AppDialog>,
    );
    await waitFor(() => expect(onEntered).toHaveBeenCalledOnce());

    view.rerender(
      <AppDialog
        open={false}
        slotProps={{ transition: { onEntered, onExited } }}
      >
        Dialog
      </AppDialog>,
    );

    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(onExited).not.toHaveBeenCalled();
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(onExited).toHaveBeenCalledOnce();
  });
});
