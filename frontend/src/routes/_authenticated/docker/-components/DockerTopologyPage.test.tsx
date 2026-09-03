import { beforeEach, describe, expect, it, vi } from "vitest";

import type { ContainerInfo, DockerNetwork } from "@/api";
import { render, screen, within } from "@/test/render";

import DockerTopologyPage from "./DockerTopologyPage";

const mocks = vi.hoisted(() => ({
  containers: [] as ContainerInfo[],
  networks: [] as DockerNetwork[],
}));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useSuspenseQuery: (options: { queryKey?: unknown[] }) => ({
      data: options.queryKey?.includes("list_networks")
        ? mocks.networks
        : mocks.containers,
    }),
  };
});

describe("DockerTopologyPage", () => {
  beforeEach(() => {
    mocks.containers = [
      {
        Created: 0,
        Id: "app-id",
        Image: "example/app:latest",
        Names: ["/app"],
        Ports: [
          {
            IP: "0.0.0.0",
            PrivatePort: 80,
            PublicPort: 8080,
            Type: "tcp",
          },
          {
            IP: "::",
            PrivatePort: 80,
            PublicPort: 8080,
            Type: "tcp",
          },
          { PrivatePort: 443, Type: "tcp" },
        ],
        State: "running",
        Status: "Up 10 minutes",
      },
      {
        Created: 0,
        Id: "orphan-id",
        Image: "example/orphan:latest",
        Names: ["/orphan"],
        State: "exited",
        Status: "Exited (0)",
      },
    ];
    mocks.networks = [
      {
        Attachable: false,
        ConfigOnly: false,
        Containers: {
          "app-id": {
            Name: "app",
            IPv4Address: "172.20.0.2/16",
          },
        },
        Driver: "bridge",
        Id: "network-id",
        Ingress: false,
        IPAM: {
          Config: [{ Gateway: "172.20.0.1", Subnet: "172.20.0.0/16" }],
        },
        Name: "example-network",
        Scope: "local",
      },
    ];
  });

  it("shows network attachments and collapses duplicate wildcard port bindings", () => {
    render(<DockerTopologyPage />);

    const topology = screen.getByRole("list", {
      name: "Docker network topology",
    });
    expect(within(topology).getByText("example-network")).toBeInTheDocument();
    expect(within(topology).getByText("172.20.0.2/16")).toBeInTheDocument();
    expect(within(topology).getByText("No listed network")).toBeInTheDocument();
    expect(within(topology).getByText("orphan")).toBeInTheDocument();

    const ports = screen.getByRole("list", { name: "Host port bindings" });
    expect(within(ports).getAllByRole("listitem")).toHaveLength(1);
    expect(within(ports).getByText("*:8080/tcp")).toBeInTheDocument();
    expect(within(ports).getByText("80/tcp")).toBeInTheDocument();
  });
});
