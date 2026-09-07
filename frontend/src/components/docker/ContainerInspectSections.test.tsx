import { describe, expect, it } from "vitest";

import type { ContainerInspectInfo, DockerNetwork } from "@/api";
import { render, screen } from "@/test/render";

import ContainerInspectSections from "./ContainerInspectSections";

const inspect: ContainerInspectInfo = {
  command: ["serve", "--port", "80"],
  created: "2026-09-03T10:00:00Z",
  entrypoint: ["/entrypoint"],
  environment: [
    { name: "HOMEPAGE_ALLOWED_HOSTS", value: "home.example.test" },
    { name: "SECRET_TOKEN", value: "very-secret" },
  ],
  health: { failingStreak: 0, status: "healthy" },
  id: "container-id",
  image: "example:latest",
  imageId: "sha256:image-id",
  labels: { purpose: "test" },
  mounts: [
    {
      Destination: "/var/run/docker.sock",
      Mode: "ro",
      RW: false,
      Source: "/var/run/docker.sock",
      Type: "bind",
    },
  ],
  name: "example",
  networks: {
    Teste_Default: {
      Gateway: "172.20.0.1",
      GlobalIPv6Address: "2001:db8::2",
      IPAddress: "172.20.0.2",
    },
  },
  ports: [
    {
      containerPort: 3000,
      hostIp: "0.0.0.0",
      hostPort: "3001",
      protocol: "tcp",
    },
    {
      containerPort: 3000,
      hostIp: "::",
      hostPort: "3001",
      protocol: "tcp",
    },
  ],
  restartCount: 0,
  restartPolicy: { maximumRetryCount: 3, name: "on-failure" },
  state: {
    dead: false,
    error: "",
    exitCode: 0,
    finishedAt: "",
    oomKilled: false,
    paused: false,
    restarting: false,
    running: true,
    startedAt: "2026-09-03T10:00:01Z",
    status: "running",
  },
  user: "1000:1000",
  workingDirectory: "/app",
};

const networks: DockerNetwork[] = [
  {
    Attachable: false,
    ConfigOnly: false,
    Driver: "bridge",
    Id: "network-id",
    Ingress: false,
    Name: "Teste_Default",
    Scope: "local",
  },
];

describe("ContainerInspectSections", () => {
  it("shows inspect sections while keeping environment values masked by default", async () => {
    const { user } = render(
      <ContainerInspectSections inspect={inspect} networks={networks} />,
    );

    expect(screen.getByText("Overview and health")).toBeInTheDocument();
    expect(screen.getByText("example:latest")).toBeInTheDocument();
    expect(screen.getByText("serve --port 80")).toBeInTheDocument();
    expect(screen.getAllByText("tcp")).toHaveLength(1);
    expect(screen.getByText("0.0.0.0:3001 → 3000")).toBeInTheDocument();
    expect(screen.queryByText(":::3001 → 3000")).not.toBeInTheDocument();
    expect(screen.getByText("purpose=test")).toBeInTheDocument();
    expect(screen.getByText("bind")).toBeInTheDocument();
    expect(
      screen.getByText(
        "/var/run/docker.sock → /var/run/docker.sock (read-only)",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByText("HOMEPAGE_ALLOWED_HOSTS").closest(".svc-detail-row"),
    ).toHaveStyle({
      display: "grid",
      gridTemplateColumns: "minmax(0, 1fr) minmax(0, 1fr)",
    });
    expect(screen.getByText("Custom network bridge")).toBeInTheDocument();
    expect(screen.getByText("Teste_Default")).toBeInTheDocument();
    expect(screen.getByText("IPv4")).toBeInTheDocument();
    expect(screen.getByText("172.20.0.2")).toBeInTheDocument();
    expect(screen.getByText("IPv6")).toBeInTheDocument();
    expect(screen.getByText("2001:db8::2")).toBeInTheDocument();
    expect(screen.queryByText("very-secret")).not.toBeInTheDocument();
    expect(screen.getAllByText("••••••••")).toHaveLength(2);

    await user.click(screen.getByRole("button", { name: "Show values" }));

    expect(screen.getByText("very-secret")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Hide values" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
  });

  it("distinguishes built-in networks and custom network drivers", () => {
    render(
      <ContainerInspectSections
        inspect={{
          ...inspect,
          networks: {
            bridge: { Gateway: "", IPAddress: "172.17.0.2" },
            host: { Gateway: "", IPAddress: "" },
            macvlan_network: { Gateway: "", IPAddress: "192.0.2.2" },
            none: { Gateway: "", IPAddress: "" },
          },
        }}
        networks={[
          ...networks,
          {
            Attachable: false,
            ConfigOnly: false,
            Driver: "macvlan",
            Id: "macvlan-id",
            Ingress: false,
            Name: "macvlan_network",
            Scope: "local",
          },
        ]}
        sections={["networks"]}
      />,
    );

    expect(screen.getByText("Default bridge")).toBeInTheDocument();
    expect(screen.getByText("Host network")).toBeInTheDocument();
    expect(screen.getByText("No network")).toBeInTheDocument();
    expect(screen.getByText("Custom network macvlan")).toBeInTheDocument();
  });
});
