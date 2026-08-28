import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import AppFullscreenDialog from "@/components/ui/AppFullscreenDialog";
import { render } from "@/test/render";

describe("AppFullscreenDialog motion", () => {
  it("keeps content mounted until its exit completes", async () => {
    const onEntered = vi.fn();
    const onExited = vi.fn();
    const view = render(
      <AppFullscreenDialog
        open
        slotProps={{ transition: { onEntered, onExited } }}
      >
        Editor
      </AppFullscreenDialog>,
    );

    await waitFor(() => expect(onEntered).toHaveBeenCalledOnce());

    view.rerender(
      <AppFullscreenDialog
        open={false}
        slotProps={{ transition: { onEntered, onExited } }}
      >
        Editor
      </AppFullscreenDialog>,
    );

    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(onExited).not.toHaveBeenCalled();
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(onExited).toHaveBeenCalledOnce();
  });
});
