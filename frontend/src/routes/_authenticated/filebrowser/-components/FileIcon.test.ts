import { describe, expect, it } from "vitest";

import { getIconColor, getIconForType } from "./fileIconUtils";

describe("FileIcon", () => {
  it("uses the YAML icon for yaml and yml files", () => {
    expect(getIconForType("compose.yaml")).toBe("mdi:file-cog-outline");
    expect(getIconForType("compose.yml")).toBe("mdi:file-cog-outline");
    expect(getIconForType("COMPOSE.YML")).toBe("mdi:file-cog-outline");
  });

  it("derives icon and color from the same extension entry", () => {
    expect(getIconForType("run.sh")).toBe("mdi:file-code");
    expect(getIconColor("run.sh")).toBe(getIconColor("main.go"));
    expect(getIconForType("notes.TXT")).toBe("mdi:file-document-outline");
    expect(getIconColor("notes.txt")).toBe(getIconColor("report.docx"));
  });

  it("falls back to the default icon and no color", () => {
    for (const name of [undefined, "Makefile", "trailing.", "data.unknown"]) {
      expect(getIconForType(name)).toBe("mdi:file");
      expect(getIconColor(name)).toBeUndefined();
    }
  });
});
