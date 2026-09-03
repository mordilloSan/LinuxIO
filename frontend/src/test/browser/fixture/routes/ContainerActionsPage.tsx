import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState } from "react";

import {
  linuxio,
  type ContainerInfo,
  type ContainerInspectInfo,
  type DockerNetwork,
} from "@/api";
import ContainerResourceDetails from "@/components/docker/ContainerResourceDetails";
import AppButton from "@/components/ui/AppButton";

const container: ContainerInfo = {
  Created: 1_788_428_800,
  Id: "example-id",
  Image: "example:latest",
  Names: ["/example"],
  State: "running",
  Status: "Up 2 hours (healthy)",
  metrics: { status: "available", cpu_percent: 2.5 },
};
const inspect: ContainerInspectInfo = {
  command: ["serve", "--port", "80"],
  created: "2026-09-03T08:00:00Z",
  entrypoint: ["/entrypoint"],
  environment: [
    { name: "HOMEPAGE_ALLOWED_HOSTS", value: "home.example.test" },
    ...Array.from({ length: 12 }, (_, index) => ({
      name: `VARIABLE_${index}`,
      value: `value-${index}`,
    })),
  ],
  health: { failingStreak: 0, status: "healthy" },
  id: container.Id,
  image: container.Image,
  imageId: "sha256:image-id",
  labels: Object.fromEntries(
    Array.from({ length: 12 }, (_, index) => [
      `label.${index}`,
      `value-${index}`,
    ]),
  ),
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
  restartPolicy: { maximumRetryCount: 0, name: "unless-stopped" },
  state: {
    dead: false,
    error: "",
    exitCode: 0,
    finishedAt: "",
    oomKilled: false,
    paused: false,
    restarting: false,
    running: true,
    startedAt: "2026-09-03T08:00:01Z",
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

const queryClient = new QueryClient({
  defaultOptions: {
    mutations: { retry: false },
    queries: { retry: false, staleTime: Infinity },
  },
});
queryClient.setQueryData(linuxio.docker.list_containers.queryKey, [container]);
queryClient.setQueryData(linuxio.docker.list_networks.queryKey, networks);
queryClient.setQueryData(
  linuxio.docker.inspect_container({ containerId: container.Id }).queryKey,
  inspect,
);

export default function ContainerActionsPage() {
  const [selected, setSelected] = useState(false);

  return (
    <QueryClientProvider client={queryClient}>
      <main style={{ padding: "var(--app-space-24)" }}>
        <h1>Container lifecycle fixture</h1>
        {selected ? (
          <ContainerResourceDetails
            container={container}
            onClose={() => setSelected(false)}
          />
        ) : (
          <AppButton onClick={() => setSelected(true)} variant="contained">
            Open container details
          </AppButton>
        )}
      </main>
    </QueryClientProvider>
  );
}
