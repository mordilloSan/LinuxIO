import fs from "node:fs";
import path from "node:path";

import { describe, expect, it } from "vitest";

const stylesheet = fs.readFileSync(
  path.resolve(process.cwd(), "src/components/ui/app-icon-button.css"),
  "utf8",
);

describe("AppIconButton", () => {
  it("keeps Iconify children out of pointer hit testing", () => {
    expect(stylesheet).toMatch(
      /\.app-icon-btn\s*>\s*\.iconify\s*\{[^}]*pointer-events:\s*none;/,
    );
  });
});
