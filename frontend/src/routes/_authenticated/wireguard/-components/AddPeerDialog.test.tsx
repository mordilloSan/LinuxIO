import { describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import AddPeerDialog from "./AddPeerDialog";

describe("AddPeerDialog", () => {
  it("submits the typed name", async () => {
    const onCreate = vi.fn();
    const view = render(
      <AddPeerDialog
        interfaceName="wg0"
        loading={false}
        onClose={vi.fn()}
        onCreate={onCreate}
        open
      />,
    );

    await view.user.type(screen.getByLabelText("Name"), "Alice");
    await view.user.click(screen.getByRole("button", { name: "Add peer" }));
    expect(onCreate).toHaveBeenCalledWith("Alice");
  });

  it("allows an empty name so the backend assigns the id", async () => {
    const onCreate = vi.fn();
    const view = render(
      <AddPeerDialog
        interfaceName="wg0"
        loading={false}
        onClose={vi.fn()}
        onCreate={onCreate}
        open
      />,
    );

    await view.user.click(screen.getByRole("button", { name: "Add peer" }));
    expect(onCreate).toHaveBeenCalledWith("");
  });
});
