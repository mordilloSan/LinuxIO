import { describe, expect, it, vi } from "vitest";

import { render, screen, waitFor } from "@/test/render";

import BatchDeleteDialog from "./BatchDeleteDialog";

const mocks = vi.hoisted(() => ({ error: vi.fn(), success: vi.fn() }));

vi.mock("@/hooks/useScopedToast", () => ({
  useScopedToast: () => mocks,
}));

describe("BatchDeleteDialog", () => {
  it("reports the backend's reason when one item cannot be deleted", async () => {
    const { user } = render(
      <BatchDeleteDialog
        items={[{ key: "data", label: "data" }]}
        noun="volume"
        onClose={vi.fn()}
        onDeleteOne={() => Promise.reject(new Error("volume is in use"))}
        onSuccess={vi.fn()}
        open
        toastMeta={{ label: "Open Docker", to: "/docker" }}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Delete" }));

    await waitFor(() =>
      expect(mocks.error).toHaveBeenCalledWith("volume is in use"),
    );
  });

  it("deletes sequentially and summarises partial failures", async () => {
    mocks.error.mockClear();
    const onSuccess = vi.fn();
    const onDeleteOne = vi.fn((item: { key: string }) =>
      item.key === "b" ? Promise.reject(new Error("busy")) : Promise.resolve(),
    );
    const { user } = render(
      <BatchDeleteDialog
        items={[
          { key: "a", label: "a" },
          { key: "b", label: "b" },
        ]}
        noun="group"
        onClose={vi.fn()}
        onDeleteOne={onDeleteOne}
        onSuccess={onSuccess}
        open
        toastMeta={{ label: "Open accounts", to: "/accounts" }}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Delete" }));

    await waitFor(() =>
      expect(mocks.error).toHaveBeenCalledWith(
        "Failed to delete 1 of 2 groups",
      ),
    );
    expect(onDeleteOne).toHaveBeenCalledTimes(2);
    expect(onSuccess).toHaveBeenCalledTimes(1);
  });
});
