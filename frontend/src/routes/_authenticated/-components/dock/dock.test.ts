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
  it("gates hover labels on live dock pointer activity", () => {
    // Chromium revives :hover on window return without a pointermove, so bare
    // :hover would show a label under a resting tile. The dock-owned attribute
    // (useDockPointerLiveness) supplies the missing evidence.
    expect(stylesheet).toContain(
      ".app-dock[data-dock-pointer] .app-dock-link:hover .app-dock__label",
    );
    expect(stylesheet).toContain(
      ".app-dock-link:focus-visible .app-dock__label",
    );
    expect(stylesheet).not.toContain("\n.app-dock-link:hover .app-dock__label");
    expect(stylesheet).not.toContain(":focus-within");
  });

  it("replaces the user-agent anchor outline with a tile-local focus ring", () => {
    // The default outline traces the transformed tile and the absolutely
    // positioned label as one stepped contour around unrelated boxes.
    expect(stylesheet).toContain(
      ".app-dock-link:focus-visible {\n  outline: none;",
    );
    expect(stylesheet).toContain(
      ".app-dock-link:focus-visible .app-dock__tile",
    );
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
