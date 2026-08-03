import fs from "node:fs";
import path from "node:path";

import { describe, expect, it, vi } from "vitest";

import AppIconButton from "@/components/ui/AppIconButton";
import { render, screen } from "@/test/render";

const stylesheet = fs.readFileSync(
  path.resolve(process.cwd(), "src/components/ui/app-icon-button.css"),
  "utf8",
);

describe("AppIconButton", () => {
  it("exposes its accessible name and disabled state", async () => {
    const onClick = vi.fn();
    const { user } = render(
      <AppIconButton aria-label="Close" disabled onClick={onClick}>
        <span aria-hidden="true">×</span>
      </AppIconButton>,
    );
    const button = screen.getByRole("button", { name: "Close" });

    expect(button).toBeDisabled();
    await user.click(button);
    expect(onClick).not.toHaveBeenCalled();
  });

  it("keeps Iconify children out of pointer hit testing", () => {
    expect(stylesheet).toMatch(
      /\.app-icon-btn\s*>\s*\.iconify\s*\{[^}]*pointer-events:\s*none;/,
    );
  });
});
