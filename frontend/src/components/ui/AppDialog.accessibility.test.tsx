import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { AppDialog, AppDialogTitle } from "@/components/ui/AppDialog";
import AppFullscreenDialog from "@/components/ui/AppFullscreenDialog";
import { render } from "@/test/render";

describe("dialog accessible names", () => {
  it("associates a dialog with its title automatically", () => {
    render(
      <AppDialog open>
        <AppDialogTitle>Settings</AppDialogTitle>
      </AppDialog>,
    );

    const dialog = screen.getByRole("dialog", { name: "Settings" });
    const title = screen.getByRole("heading", { name: "Settings", level: 2 });
    expect(title).toHaveAttribute("id");
    expect(dialog).toHaveAttribute("aria-labelledby", title.id);
  });

  it("preserves explicit accessible name props", () => {
    render(
      <AppDialog aria-label="Command palette" open>
        <AppDialogTitle>Ignored title</AppDialogTitle>
      </AppDialog>,
    );

    expect(
      screen.getByRole("dialog", { name: "Command palette" }),
    ).toHaveAttribute("aria-label", "Command palette");
    expect(
      screen.getByRole("dialog", { name: "Command palette" }),
    ).not.toHaveAttribute("aria-labelledby");
  });

  it("uses an explicit labelledby over the generated title id", () => {
    render(
      <AppDialog aria-label="Fallback" aria-labelledby="custom-title" open>
        <span id="custom-title">Explicit title</span>
        <AppDialogTitle>Generated title</AppDialogTitle>
      </AppDialog>,
    );

    expect(
      screen.getByRole("dialog", { name: "Explicit title" }),
    ).toHaveAttribute("aria-labelledby", "custom-title");
  });

  it("keeps nested dialog title associations separate", () => {
    render(
      <AppDialog open>
        <AppDialogTitle>Outer</AppDialogTitle>
        <AppDialog open>
          <AppDialogTitle>Inner</AppDialogTitle>
        </AppDialog>
      </AppDialog>,
    );

    const outerTitle = screen.getByText("Outer");
    const innerTitle = screen.getByText("Inner");
    expect(screen.getByRole("dialog", { name: "Outer" })).toHaveAttribute(
      "aria-labelledby",
      outerTitle.id,
    );
    expect(screen.getByRole("dialog", { name: "Inner" })).toHaveAttribute(
      "aria-labelledby",
      innerTitle.id,
    );
    expect(outerTitle.id).not.toBe(innerTitle.id);
  });

  it("shares title association with fullscreen dialogs", () => {
    render(
      <AppFullscreenDialog open>
        <AppDialogTitle>Compose editor</AppDialogTitle>
      </AppFullscreenDialog>,
    );

    const dialog = screen.getByRole("dialog", { name: "Compose editor" });
    const title = screen.getByText("Compose editor");
    expect(dialog).toHaveAttribute("aria-labelledby", title.id);
  });
});
