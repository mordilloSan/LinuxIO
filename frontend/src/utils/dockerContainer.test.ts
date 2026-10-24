import { describe, expect, it } from "vitest";

import type { ContainerInfo } from "@/api";
import {
  formatContainerPort,
  formatContainerUptime,
  getContainerDisplayState,
  getContainerMounts,
  getContainerName,
  getContainerServiceName,
  getDedupedPorts,
} from "@/utils/dockerContainer";

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

describe("formatContainerPort", () => {
  it("shows the host binding only for published ports", () => {
    expect(
      formatContainerPort({ PrivatePort: 80, PublicPort: 8080, Type: "tcp" }),
    ).toBe("8080:80/tcp");
    expect(formatContainerPort({ PrivatePort: 53, Type: "udp" })).toBe(
      "53/udp",
    );
  });
});

describe("getContainerMounts", () => {
  it("keeps only bind and volume mounts", () => {
    const mounts = getContainerMounts(
      container({
        Mounts: [
          { Type: "bind" },
          { Type: "tmpfs" },
          { Type: "volume" },
        ] as ContainerInfo["Mounts"],
      }),
    );
    expect(mounts.map((mount) => mount.Type)).toEqual(["bind", "volume"]);
  });
});

describe("getContainerServiceName", () => {
  it("reads the compose service label", () => {
    expect(
      getContainerServiceName(
        container({ Labels: { "com.docker.compose.service": "web" } }),
      ),
    ).toBe("web");
    expect(getContainerServiceName(container({}))).toBe("-");
  });
});

describe("formatContainerUptime", () => {
  const ago = (seconds: number) => Math.floor(Date.now() / 1000) - seconds;

  it.each([
    [30, "30s"],
    [125, "2m"],
    [3 * 3600 + 5 * 60, "3h 5m"],
    [2 * 86400 + 4 * 3600, "2d 4h"],
  ])("%is → %s", (seconds, expected) => {
    expect(formatContainerUptime(ago(seconds))).toBe(expected);
  });

  it("returns a dash for future timestamps", () => {
    expect(formatContainerUptime(ago(-3600))).toBe("-");
  });
});
