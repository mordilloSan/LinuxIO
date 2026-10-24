import { describe, expect, it } from "vitest";

import type { ContainerConfiguration, DockerNetwork } from "@/api";

import {
  changedSections,
  emptyConfiguration,
  normalizedConfiguration,
  validateConfiguration,
} from "./containerConfiguration";

const configuration = (
  overrides: Partial<ContainerConfiguration> = {},
): ContainerConfiguration => ({
  ...emptyConfiguration([]),
  name: "web",
  image: "nginx",
  ...overrides,
});

describe("emptyConfiguration", () => {
  it("attaches to the default bridge network when it exists", () => {
    const networks = [{ Name: "bridge" }] as DockerNetwork[];
    expect(emptyConfiguration(networks).networks).toEqual([
      { name: "bridge", aliases: [] },
    ]);
    expect(emptyConfiguration([]).networks).toEqual([]);
  });
});

describe("normalizedConfiguration", () => {
  it("trims text and drops blank environment and network rows", () => {
    const normalized = normalizedConfiguration(
      configuration({
        name: " web ",
        environment: [
          { name: "", value: "" },
          { name: " A ", value: "1" },
        ],
        networks: [
          { name: "", aliases: [] },
          { name: " app ", aliases: [" api ", ""] },
        ],
      }),
    );
    expect(normalized.name).toBe("web");
    expect(normalized.environment).toEqual([{ name: "A", value: "1" }]);
    expect(normalized.networks).toEqual([{ name: "app", aliases: ["api"] }]);
  });
});

describe("validateConfiguration", () => {
  it("accepts a minimal configuration", () => {
    expect(validateConfiguration(configuration())).toBeUndefined();
  });

  it("requires a name and an image", () => {
    expect(validateConfiguration(configuration({ image: "" }))).toBe(
      "Name and image are required.",
    );
  });

  it("requires absolute container paths for mounts", () => {
    expect(
      validateConfiguration(
        configuration({
          mounts: [
            {
              type: "bind",
              source: "/srv",
              destination: "data",
              readOnly: false,
            },
          ],
        }),
      ),
    ).toBe("Each mount needs a source and an absolute container path.");
  });
});

describe("changedSections", () => {
  it("lists only the sections that differ", () => {
    const before = configuration();
    expect(changedSections(before, before)).toEqual([]);
    expect(
      changedSections(
        before,
        configuration({ image: "nginx:1.25", user: "1" }),
      ),
    ).toEqual(["Basics", "Runtime"]);
  });
});
