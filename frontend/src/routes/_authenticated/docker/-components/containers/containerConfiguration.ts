import type {
  ContainerConfiguration,
  ContainerInspectInfo,
  DockerNetwork,
} from "@/api";

export const emptyConfiguration = (
  networks: DockerNetwork[],
): ContainerConfiguration => ({
  name: "",
  image: "",
  command: [],
  entrypoint: [],
  environment: [],
  ports: [],
  mounts: [],
  networks: networks.some((network) => network.Name === "bridge")
    ? [{ name: "bridge", aliases: [] }]
    : [],
  restartPolicy: { name: "no", maximumRetryCount: 0 },
  user: "",
  workingDirectory: "",
});

export const configurationFromInspect = (
  inspect: ContainerInspectInfo,
): ContainerConfiguration => ({
  name: inspect.name,
  image: inspect.image,
  command: inspect.command ?? [],
  entrypoint: inspect.entrypoint ?? [],
  environment: inspect.environment ?? [],
  ports: inspect.ports ?? [],
  mounts: (inspect.mounts ?? [])
    .filter((item) => item.Type === "bind" || item.Type === "volume")
    .map((item) => ({
      type: item.Type,
      source: item.Type === "volume" ? (item.Name ?? item.Source) : item.Source,
      destination: item.Destination,
      readOnly: !item.RW,
    })),
  networks: Object.entries(inspect.networks ?? {}).map(([name, endpoint]) => ({
    name,
    aliases: endpoint.Aliases ?? [],
  })),
  restartPolicy: inspect.restartPolicy,
  user: inspect.user,
  workingDirectory: inspect.workingDirectory,
});

export const normalizedConfiguration = (
  configuration: ContainerConfiguration,
): ContainerConfiguration => ({
  ...configuration,
  name: configuration.name.trim(),
  image: configuration.image.trim(),
  environment: configuration.environment
    .filter((item) => item.name !== "" || item.value !== "")
    .map((item) => ({ ...item, name: item.name.trim() })),
  ports: configuration.ports.map((item) => ({
    ...item,
    hostIp: item.hostIp.trim(),
    hostPort: item.hostPort.trim(),
    protocol: item.protocol.toLowerCase(),
  })),
  mounts: configuration.mounts.map((item) => ({
    ...item,
    source: item.source.trim(),
    destination: item.destination.trim(),
  })),
  networks: configuration.networks
    .filter((item) => item.name !== "" || item.aliases.length > 0)
    .map((item) => ({
      name: item.name.trim(),
      aliases: item.aliases.map((alias) => alias.trim()).filter(Boolean),
    })),
  user: configuration.user.trim(),
  workingDirectory: configuration.workingDirectory.trim(),
});

export const validateConfiguration = (
  configuration: ContainerConfiguration,
): string | undefined => {
  if (!configuration.name || !configuration.image) {
    return "Name and image are required.";
  }
  if (configuration.environment.some((item) => !item.name)) {
    return "Each environment variable needs a name.";
  }
  if (
    configuration.ports.some(
      (item) => item.containerPort < 1 || item.containerPort > 65535,
    )
  ) {
    return "Container ports must be between 1 and 65535.";
  }
  if (
    configuration.mounts.some(
      (item) => !item.source || !item.destination.startsWith("/"),
    )
  ) {
    return "Each mount needs a source and an absolute container path.";
  }
  if (configuration.networks.some((item) => !item.name)) {
    return "Each network attachment needs a network.";
  }
  if (
    configuration.workingDirectory &&
    !configuration.workingDirectory.startsWith("/")
  ) {
    return "Working directory must be an absolute container path.";
  }
  return undefined;
};

export const changedSections = (
  before: ContainerConfiguration,
  after: ContainerConfiguration,
): string[] => {
  const changed: string[] = [];
  if (before.name !== after.name || before.image !== after.image) {
    changed.push("Basics");
  }
  if (
    JSON.stringify(before.command) !== JSON.stringify(after.command) ||
    JSON.stringify(before.entrypoint) !== JSON.stringify(after.entrypoint)
  ) {
    changed.push("Command and entrypoint");
  }
  if (
    JSON.stringify(before.environment) !== JSON.stringify(after.environment)
  ) {
    changed.push("Environment variables");
  }
  if (JSON.stringify(before.ports) !== JSON.stringify(after.ports)) {
    changed.push("Published ports");
  }
  if (JSON.stringify(before.mounts) !== JSON.stringify(after.mounts)) {
    changed.push("Mounts");
  }
  if (JSON.stringify(before.networks) !== JSON.stringify(after.networks)) {
    changed.push("Networks and aliases");
  }
  if (
    JSON.stringify(before.restartPolicy) !==
      JSON.stringify(after.restartPolicy) ||
    before.user !== after.user ||
    before.workingDirectory !== after.workingDirectory
  ) {
    changed.push("Runtime");
  }
  return changed;
};

export const errorMessage = (error: unknown) =>
  error instanceof Error ? error.message : "The Docker operation failed.";
