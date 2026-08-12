import { beforeEach, describe, expect, it, vi } from "vitest";

import SetHostnameDialog from "@/routes/_authenticated/-dashboard/SetHostnameDialog";
import { render, screen } from "@/test/render";

const mocks = vi.hoisted(() => ({
  mutate: vi.fn(),
  useCallMutation: vi.fn(),
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    useCallMutation: mocks.useCallMutation,
  };
});

vi.mock("@/hooks/useScopedToast", () => ({
  useScopedToast: () => ({ success: vi.fn() }),
}));

describe("SetHostnameDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.useCallMutation.mockReturnValue({
      isPending: false,
      mutate: mocks.mutate,
    });
  });

  it("resets to the current hostname when reopened with a newer value", async () => {
    const { rerender, user } = render(
      <SetHostnameDialog current="old-host" onClose={vi.fn()} open />,
    );

    const input = screen.getByRole("textbox");
    await user.clear(input);
    await user.type(input, "edited-host");

    rerender(
      <SetHostnameDialog current="new-host" onClose={vi.fn()} open={false} />,
    );
    rerender(<SetHostnameDialog current="new-host" onClose={vi.fn()} open />);

    expect(screen.getByRole("textbox")).toHaveValue("new-host");
  });

  it("keeps the dialog busy and blocks duplicate save while pending", async () => {
    let pending = false;
    const onClose = vi.fn();
    mocks.useCallMutation.mockImplementation(() => ({
      isPending: pending,
      mutate: mocks.mutate,
    }));
    const { rerender, user } = render(
      <SetHostnameDialog current="nas" onClose={onClose} open />,
    );

    await user.click(screen.getByRole("button", { name: "Save" }));
    pending = true;
    rerender(<SetHostnameDialog current="nas" onClose={onClose} open />);

    expect(screen.getByRole("button", { name: "Saving…" })).toBeDisabled();
    expect(screen.getByRole("textbox")).toBeDisabled();
    expect(screen.getByRole("dialog")).toHaveAttribute("aria-busy", "true");
    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
    await user.keyboard("{Enter}{Escape}");
    await user.click(screen.getByRole("button", { name: "Saving…" }));
    expect(mocks.mutate).toHaveBeenCalledTimes(1);
    expect(onClose).not.toHaveBeenCalled();
  });
});
