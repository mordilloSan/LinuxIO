import { describe, expect, it, vi } from "vitest";

import { render, screen, waitFor } from "@/test/render";

import BatchDeleteDialog from "./BatchDeleteDialog";

const mocks = vi.hoisted(() => ({ error: vi.fn(), success: vi.fn() }));

vi.mock("@/hooks/useScopedToast", () => ({
  useScopedToast: () => mocks,
}));

describe("BatchDeleteDialog", () => {
  it("reports Docker's reason when one resource cannot be deleted", async () => {
    const { user } = render(
      <BatchDeleteDialog
        items={[{ key: "data", label: "data" }]}
        noun="volume"
        onClose={vi.fn()}
        onDeleteOne={() => Promise.reject(new Error("volume is in use"))}
        onSuccess={vi.fn()}
        open
      />,
    );

    await user.click(screen.getByRole("button", { name: "Delete" }));

    await waitFor(() =>
      expect(mocks.error).toHaveBeenCalledWith("volume is in use"),
    );
  });
});
