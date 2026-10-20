import { describe, expect, it } from "vitest";

import { vmOSIcon } from "./vmShared";

describe("vmOSIcon", () => {
  it("maps OS ids to logos and falls back sensibly", () => {
    expect(vmOSIcon("ubuntu")).toBe("simple-icons:ubuntu");
    expect(vmOSIcon("homeassistant")).toBe("simple-icons:homeassistant");
    expect(vmOSIcon("win")).toBe("mdi:microsoft-windows");
    expect(vmOSIcon("silverblue")).toBe("simple-icons:linux");
    expect(vmOSIcon(undefined)).toBe("mdi:monitor");
  });
});
