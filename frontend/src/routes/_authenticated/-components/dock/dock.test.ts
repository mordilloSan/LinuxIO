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
  it("shows labels only for keyboard-visible focus", () => {
    // :focus-visible is necessary but not sufficient — the browser starts
    // matching it on the click-focused link as soon as any key is pressed, so
    // the pointer-focus mark has to be excluded too or the label sticks.
    expect(stylesheet).toContain(
      ".app-dock-link:focus-visible:not([data-pointer-focus]) .app-dock__label",
    );
    expect(stylesheet).toContain(
      ":root[data-pointer-active] .app-dock-link:hover .app-dock__label",
    );
    expect(stylesheet).not.toContain("\n.app-dock-link:hover .app-dock__label");
    expect(stylesheet).not.toContain(":focus-within");
  });
});

describe("dock magnification rasterization", () => {
  it("lays out the tile layer at its 64px magnification peak", () => {
    const tileRule = stylesheet.match(
      /\.app-dock__tile \{(?<body>[\s\S]*?)\n\}/,
    )?.groups?.body;

    expect(tileRule).toContain("width: 64px");
    expect(tileRule).toContain("height: 64px");
    expect(tileRule).toContain("flex: 0 0 64px");
    expect(tileRule).toContain("will-change: transform");
  });
});
