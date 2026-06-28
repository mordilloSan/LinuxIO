import { describe, expect, it } from "vitest";

import { getIconForType } from "@/components/filebrowser/FileIcon";

describe("FileIcon", () => {
  it("uses the YAML icon for yaml and yml files", () => {
    expect(getIconForType("compose.yaml")).toBe("mdi:file-cog-outline");
    expect(getIconForType("compose.yml")).toBe("mdi:file-cog-outline");
    expect(getIconForType("COMPOSE.YML")).toBe("mdi:file-cog-outline");
  });
});
