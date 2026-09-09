import { screen } from "@testing-library/react";
import type { KeyboardEvent } from "react";
import { expect, it, vi } from "vitest";

import { AppDialog } from "@/components/ui/AppDialog";
import { render } from "@/test/render";

it("keeps dialog typing out of parent shortcuts while preserving Escape close", async () => {
  const onClose = vi.fn();
  const onParentKeyDown = vi.fn((event: KeyboardEvent) => {
    if (event.key !== "Escape") event.preventDefault();
  });
  const { user } = render(
    <div onKeyDown={onParentKeyDown}>
      <AppDialog onClose={onClose} open>
        <input aria-label="Command" />
      </AppDialog>
    </div>,
  );

  await user.type(screen.getByRole("textbox"), "echo hello  world ");
  await user.keyboard("{Enter}");
  expect(screen.getByRole("textbox")).toHaveValue("echo hello  world ");
  expect(onParentKeyDown).not.toHaveBeenCalled();

  await user.keyboard("{Escape}");
  expect(onClose).toHaveBeenCalledOnce();
  expect(onClose).toHaveBeenCalledWith(expect.anything(), "escapeKeyDown");
});
