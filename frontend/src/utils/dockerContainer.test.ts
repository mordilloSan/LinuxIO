import { describe, expect, it } from "vitest";

import type { ContainerInfo } from "@/api";

import {
  getContainerDisplayState,
  getContainerName,
  getDedupedPorts,
} from "./dockerContainer";

const container = (overrides: Partial<ContainerInfo>): ContainerInfo =>
  ({
    Id: "abcdef0123456789",
    Names: [],
    State: "",
    Status: "",
    ...overrides,
  }) as ContainerInfo;

describe("getContainerName", () => {
  it("strips only the leading slash", () => {
    expect(getContainerName(container({ Names: ["/a/b"] }))).toBe("a/b");
  });
  it("falls back to the short ID", () => {
    expect(getContainerName(container({ Names: undefined }))).toBe(
      "abcdef012345",
    );
  });
});

describe("getContainerDisplayState", () => {
  it.each([
    [{ State: "running", Status: "Up 2h (unhealthy)" }, "Unhealthy"],
    [{ State: "running", Status: "Up 2h (healthy)" }, "Healthy"],
    [{ State: "running", Status: "Up 2h" }, "Running"],
    [{ State: "exited", Status: "Exited (0)" }, "Stopped"],
    [{ State: "dead", Status: "Dead" }, "Dead"],
    [{ State: "created", Status: "Created" }, "created"],
    [{ State: "", Status: "" }, "Unknown"],
  ])("%o → %s", (fields, expected) => {
    expect(getContainerDisplayState(container(fields))).toBe(expected);
  });
});

describe("getDedupedPorts", () => {
  it("collapses duplicate bindings and sorts by private port", () => {
    const ports = getDedupedPorts(
      container({
        Ports: [
          { IP: "::", PrivatePort: 443, PublicPort: 8443, Type: "tcp" },
          { IP: "0.0.0.0", PrivatePort: 443, PublicPort: 8443, Type: "tcp" },
          { PrivatePort: 53, Type: "udp" },
          { PrivatePort: 53, Type: "tcp" },
        ] as ContainerInfo["Ports"],
      }),
    );
    expect(ports.map((p) => `${p.PrivatePort}/${p.Type}`)).toEqual([
      "53/tcp",
      "53/udp",
      "443/tcp",
    ]);
  });
});
