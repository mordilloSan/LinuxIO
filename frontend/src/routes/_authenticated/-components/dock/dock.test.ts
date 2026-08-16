import fs from "node:fs";
import path from "node:path";

import { describe, expect, it } from "vitest";

const stylesheet = fs.readFileSync(
  path.resolve(
    process.cwd(),
    "src/routes/_authenticated/-components/dock/dock.css",
  ),
  "utf8",
);

describe("dock labels", () => {
  it("shows action labels only for keyboard-visible focus", () => {
    expect(stylesheet).toContain(
      ".app-dock__action:has(:focus-visible) .app-dock__label",
    );
    expect(stylesheet).not.toContain(
      ".app-dock__action:focus-within .app-dock__label",
    );
  });
});
