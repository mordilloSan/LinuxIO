import { describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import ContextMenu from "./ContextMenu";

describe("ContextMenu", () => {
  it("uses the new labels and shows keyboard shortcut hints", () => {
    render(
      <ContextMenu
        anchorPosition={{ left: 10, top: 10 }}
        canExtract
        hasClipboard
        hasSelection
        onChangePermissions={vi.fn()}
        onClose={vi.fn()}
        onCopy={vi.fn()}
        onCreateFile={vi.fn()}
        onCreateFolder={vi.fn()}
        onCut={vi.fn()}
        onDelete={vi.fn()}
        onDeletePermanently={vi.fn()}
        onDownload={vi.fn()}
        onPaste={vi.fn()}
        onRename={vi.fn()}
        onUpload={vi.fn()}
      />,
    );

    expect(
      screen.getByRole("menuitem", { name: /New folder/ }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("menuitem", { name: /Extract to new folder/ }),
    ).toBeInTheDocument();
    for (const [name, keys] of [
      ["Copy", "Ctrl+C"],
      ["Cut", "Ctrl+X"],
      ["Paste", "Ctrl+V"],
      ["Rename", "F2"],
      ["Delete", "Del"],
      ["Delete permanently", "Shift+Del"],
    ]) {
      expect(
        screen.getByRole("menuitem", {
          name: new RegExp(`^${name}\\s*${keys.replace("+", "\\+")}$`),
        }),
      ).toBeInTheDocument();
    }
  });
});
