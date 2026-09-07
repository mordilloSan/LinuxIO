import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import { useId, useState, type ReactNode, type SyntheticEvent } from "react";

import {
  type ContainerConfiguration,
  type ContainerInspectInfo,
  type DockerImage,
  type DockerNetwork,
  type DockerVolume,
  linuxio,
  useCallMutation,
} from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppButton from "@/components/ui/AppButton";
import AppCheckbox from "@/components/ui/AppCheckbox";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppCollapse from "@/components/ui/AppCollapse";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppIconButton from "@/components/ui/AppIconButton";
import AppSelect from "@/components/ui/AppSelect";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import SectionHeader from "@/components/ui/SectionHeader";

import "./container-form-dialog.css";

interface ContainerFormDialogProps {
  containerId?: string;
  mode: "create" | "edit";
  onClose: () => void;
  open: boolean;
}

interface FormSectionProps {
  children: ReactNode;
  expanded: boolean;
  id: string;
  onToggle: () => void;
  title: string;
}

type SectionKey =
  | "command"
  | "environment"
  | "ports"
  | "mounts"
  | "networks"
  | "runtime";

const DOCKER_TOAST_META = { label: "Open Docker", to: "/docker" } as const;

const FormSection = ({
  children,
  expanded,
  id,
  onToggle,
  title,
}: FormSectionProps) => (
  <section className="container-form-dialog__section">
    <SectionHeader
      controlsId={id}
      expanded={expanded}
      onToggle={onToggle}
      title={title}
    />
    <AppCollapse in={expanded} unmountOnExit>
      <div className="container-form-dialog__section-content" id={id}>
        {children}
      </div>
    </AppCollapse>
  </section>
);

const emptyConfiguration = (
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

const configurationFromInspect = (
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

const normalizedConfiguration = (
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

const validateConfiguration = (
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

const changedSections = (
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

const errorMessage = (error: unknown) =>
  error instanceof Error ? error.message : "The Docker operation failed.";

interface ContainerConfigurationFormProps {
  images: DockerImage[];
  initialConfiguration: ContainerConfiguration;
  inspect?: ContainerInspectInfo;
  mode: "create" | "edit";
  networks: DockerNetwork[];
  onClose: () => void;
  volumes: DockerVolume[];
}

const ContainerConfigurationForm = ({
  images,
  initialConfiguration,
  inspect,
  mode,
  networks,
  onClose,
  volumes,
}: ContainerConfigurationFormProps) => {
  const [configuration, setConfiguration] = useState(initialConfiguration);
  const [start, setStart] = useState(false);
  const [error, setError] = useState<string>();
  const [reviewConfiguration, setReviewConfiguration] =
    useState<ContainerConfiguration>();
  const [sections, setSections] = useState<Record<SectionKey, boolean>>(() => ({
    command:
      initialConfiguration.command.length > 0 ||
      initialConfiguration.entrypoint.length > 0,
    environment: initialConfiguration.environment.length > 0,
    ports: initialConfiguration.ports.length > 0,
    mounts: initialConfiguration.mounts.length > 0,
    networks: initialConfiguration.networks.length > 0,
    runtime:
      initialConfiguration.restartPolicy.name !== "no" ||
      initialConfiguration.restartPolicy.maximumRetryCount > 0 ||
      Boolean(initialConfiguration.user) ||
      Boolean(initialConfiguration.workingDirectory),
  }));
  const imageListId = useId();
  const volumeListId = useId();
  const createMutation = useCallMutation(linuxio.docker.create_container, {
    success: "Container created",
    error: "Failed to create container",
    toast: DOCKER_TOAST_META,
  });
  const editMutation = useCallMutation(linuxio.docker.edit_container, {
    success: "Container recreated",
    error: "Failed to recreate container",
    toast: DOCKER_TOAST_META,
  });
  const pending = createMutation.isPending || editMutation.isPending;
  const changes = reviewConfiguration
    ? changedSections(initialConfiguration, reviewConfiguration)
    : [];
  const imageTags = Array.from(
    new Set(images.flatMap((image) => image.RepoTags ?? [])),
  ).sort();
  const toggleSection = (section: SectionKey) => {
    setSections((current) => ({ ...current, [section]: !current[section] }));
  };

  const submitConfiguration = async (
    event: SyntheticEvent<HTMLFormElement>,
  ) => {
    event.preventDefault();
    const normalized = normalizedConfiguration(configuration);
    const validationError = validateConfiguration(normalized);
    if (validationError) {
      setError(validationError);
      return;
    }
    setError(undefined);
    if (mode === "edit") {
      if (changedSections(initialConfiguration, normalized).length === 0) {
        setError("Change at least one field before recreating the container.");
        return;
      }
      setReviewConfiguration(normalized);
      return;
    }
    try {
      await createMutation.mutateAsync({ configuration: normalized, start });
      onClose();
    } catch (mutationError) {
      setError(errorMessage(mutationError));
    }
  };

  const confirmEdit = async () => {
    if (!inspect || !reviewConfiguration) return;
    setError(undefined);
    try {
      await editMutation.mutateAsync({
        containerId: inspect.id,
        configuration: reviewConfiguration,
      });
      onClose();
    } catch (mutationError) {
      setError(errorMessage(mutationError));
    }
  };

  if (reviewConfiguration) {
    return (
      <GeneralDialog
        aria-busy={pending || undefined}
        aria-label={`Review changes to ${initialConfiguration.name}`}
        disableEscapeKeyDown={pending}
        fullWidth
        maxWidth="sm"
        onClose={pending ? undefined : onClose}
        open
      >
        <AppDialogTitle>Recreate {initialConfiguration.name}?</AppDialogTitle>
        <AppDialogContent>
          <AppDialogContentText>
            {inspect?.state.running
              ? "The container will be briefly unavailable while Docker creates and verifies its replacement."
              : "Docker will recreate the container and keep the replacement stopped."}
          </AppDialogContentText>
          <AppTypography color="text.secondary" variant="body2">
            The original container is restored automatically if replacement
            verification fails.
          </AppTypography>
          <ul className="container-form-dialog__changes">
            {changes.map((change) => (
              <li key={change}>{change}</li>
            ))}
          </ul>
          {error && (
            <AppTypography color="error" role="alert" variant="body2">
              {error}
            </AppTypography>
          )}
        </AppDialogContent>
        <AppDialogActions>
          <AppButton
            disabled={pending}
            onClick={() => setReviewConfiguration(undefined)}
          >
            Back
          </AppButton>
          <AppButton disabled={pending} onClick={onClose}>
            Cancel
          </AppButton>
          <AppButton
            autoFocus
            disabled={pending}
            onClick={() => void confirmEdit()}
            variant="contained"
          >
            {pending ? "Recreating…" : "Recreate container"}
          </AppButton>
        </AppDialogActions>
      </GeneralDialog>
    );
  }

  return (
    <GeneralDialog
      aria-busy={pending || undefined}
      aria-label={
        mode === "create" ? "Create container" : `Edit ${inspect?.name}`
      }
      className="container-form-dialog"
      disableEscapeKeyDown={pending}
      fullWidth
      maxWidth="md"
      onClose={pending ? undefined : onClose}
      open
    >
      <form
        className="container-form-dialog__form"
        onSubmit={submitConfiguration}
      >
        <AppDialogTitle>
          {mode === "create" ? "Create container" : `Edit ${inspect?.name}`}
        </AppDialogTitle>
        <AppDialogContent className="container-form-dialog__content">
          <section aria-labelledby="container-basics-title">
            <AppTypography
              fontWeight={700}
              id="container-basics-title"
              variant="subtitle1"
            >
              Basics
            </AppTypography>
            <div className="container-form-dialog__grid">
              <AppTextField
                autoFocus
                disabled={pending}
                fullWidth
                label="Name"
                onChange={(event) =>
                  setConfiguration((current) => ({
                    ...current,
                    name: event.target.value,
                  }))
                }
                required
                value={configuration.name}
              />
              <AppTextField
                disabled={pending}
                fullWidth
                label="Image"
                list={imageListId}
                onChange={(event) =>
                  setConfiguration((current) => ({
                    ...current,
                    image: event.target.value,
                  }))
                }
                placeholder="nginx:latest"
                required
                value={configuration.image}
              />
              <datalist id={imageListId}>
                {imageTags.map((tag) => (
                  <option key={tag} value={tag} />
                ))}
              </datalist>
            </div>
          </section>

          <FormSection
            expanded={sections.command}
            id="container-command-section"
            onToggle={() => toggleSection("command")}
            title="Command and entrypoint"
          >
            <div className="container-form-dialog__grid">
              <AppTextField
                disabled={pending}
                fullWidth
                helperText="One argument per line"
                label="Command"
                multiline
                onChange={(event) =>
                  setConfiguration((current) => ({
                    ...current,
                    command:
                      event.target.value === ""
                        ? []
                        : event.target.value.split("\n"),
                  }))
                }
                rows={3}
                value={configuration.command.join("\n")}
              />
              <AppTextField
                disabled={pending}
                fullWidth
                helperText="One argument per line"
                label="Entrypoint"
                multiline
                onChange={(event) =>
                  setConfiguration((current) => ({
                    ...current,
                    entrypoint:
                      event.target.value === ""
                        ? []
                        : event.target.value.split("\n"),
                  }))
                }
                rows={3}
                value={configuration.entrypoint.join("\n")}
              />
            </div>
          </FormSection>

          <FormSection
            expanded={sections.environment}
            id="container-environment-section"
            onToggle={() => toggleSection("environment")}
            title="Environment variables"
          >
            <div className="container-form-dialog__rows">
              {configuration.environment.map((variable, index) => (
                <div
                  className="container-form-dialog__environment-row"
                  key={index}
                >
                  <AppTextField
                    disabled={pending}
                    fullWidth
                    label="Variable"
                    onChange={(event) =>
                      setConfiguration((current) => ({
                        ...current,
                        environment: current.environment.map(
                          (item, itemIndex) =>
                            itemIndex === index
                              ? { ...item, name: event.target.value }
                              : item,
                        ),
                      }))
                    }
                    value={variable.name}
                  />
                  <AppTextField
                    disabled={pending}
                    fullWidth
                    label="Value"
                    onChange={(event) =>
                      setConfiguration((current) => ({
                        ...current,
                        environment: current.environment.map(
                          (item, itemIndex) =>
                            itemIndex === index
                              ? { ...item, value: event.target.value }
                              : item,
                        ),
                      }))
                    }
                    value={variable.value}
                  />
                  <AppIconButton
                    aria-label={`Remove environment variable ${index + 1}`}
                    disabled={pending}
                    onClick={() =>
                      setConfiguration((current) => ({
                        ...current,
                        environment: current.environment.filter(
                          (_, itemIndex) => itemIndex !== index,
                        ),
                      }))
                    }
                    size="small"
                  >
                    <Icon icon="mdi:delete-outline" width={18} />
                  </AppIconButton>
                </div>
              ))}
            </div>
            <AppButton
              disabled={pending}
              onClick={() =>
                setConfiguration((current) => ({
                  ...current,
                  environment: [
                    ...current.environment,
                    { name: "", value: "" },
                  ],
                }))
              }
              size="small"
              startIcon={<Icon icon="mdi:plus" width={18} />}
            >
              Add variable
            </AppButton>
          </FormSection>

          <FormSection
            expanded={sections.ports}
            id="container-ports-section"
            onToggle={() => toggleSection("ports")}
            title="Published ports"
          >
            <div className="container-form-dialog__rows">
              {configuration.ports.map((port, index) => (
                <div className="container-form-dialog__port-row" key={index}>
                  <AppTextField
                    disabled={pending}
                    fullWidth
                    label="Host IP"
                    onChange={(event) =>
                      setConfiguration((current) => ({
                        ...current,
                        ports: current.ports.map((item, itemIndex) =>
                          itemIndex === index
                            ? { ...item, hostIp: event.target.value }
                            : item,
                        ),
                      }))
                    }
                    placeholder="All interfaces"
                    value={port.hostIp}
                  />
                  <AppTextField
                    disabled={pending}
                    fullWidth
                    label="Host port"
                    onChange={(event) =>
                      setConfiguration((current) => ({
                        ...current,
                        ports: current.ports.map((item, itemIndex) =>
                          itemIndex === index
                            ? { ...item, hostPort: event.target.value }
                            : item,
                        ),
                      }))
                    }
                    type="number"
                    value={port.hostPort}
                  />
                  <AppTextField
                    disabled={pending}
                    fullWidth
                    label="Container port"
                    onChange={(event) =>
                      setConfiguration((current) => ({
                        ...current,
                        ports: current.ports.map((item, itemIndex) =>
                          itemIndex === index
                            ? {
                                ...item,
                                containerPort: Number(event.target.value),
                              }
                            : item,
                        ),
                      }))
                    }
                    required
                    type="number"
                    value={port.containerPort || ""}
                  />
                  <AppSelect
                    disabled={pending}
                    fullWidth
                    label="Protocol"
                    onChange={(event) =>
                      setConfiguration((current) => ({
                        ...current,
                        ports: current.ports.map((item, itemIndex) =>
                          itemIndex === index
                            ? { ...item, protocol: event.target.value }
                            : item,
                        ),
                      }))
                    }
                    value={port.protocol}
                  >
                    <option value="tcp">TCP</option>
                    <option value="udp">UDP</option>
                    <option value="sctp">SCTP</option>
                  </AppSelect>
                  <AppIconButton
                    aria-label={`Remove published port ${index + 1}`}
                    disabled={pending}
                    onClick={() =>
                      setConfiguration((current) => ({
                        ...current,
                        ports: current.ports.filter(
                          (_, itemIndex) => itemIndex !== index,
                        ),
                      }))
                    }
                    size="small"
                  >
                    <Icon icon="mdi:delete-outline" width={18} />
                  </AppIconButton>
                </div>
              ))}
            </div>
            <AppButton
              disabled={pending}
              onClick={() =>
                setConfiguration((current) => ({
                  ...current,
                  ports: [
                    ...current.ports,
                    {
                      containerPort: 0,
                      hostIp: "",
                      hostPort: "",
                      protocol: "tcp",
                    },
                  ],
                }))
              }
              size="small"
              startIcon={<Icon icon="mdi:plus" width={18} />}
            >
              Add port
            </AppButton>
          </FormSection>

          <FormSection
            expanded={sections.mounts}
            id="container-mounts-section"
            onToggle={() => toggleSection("mounts")}
            title="Mounts"
          >
            <datalist id={volumeListId}>
              {volumes.map((volume) => (
                <option key={volume.Name} value={volume.Name} />
              ))}
            </datalist>
            <div className="container-form-dialog__rows">
              {configuration.mounts.map((item, index) => (
                <div className="container-form-dialog__mount-row" key={index}>
                  <AppSelect
                    disabled={pending}
                    fullWidth
                    label="Type"
                    onChange={(event) =>
                      setConfiguration((current) => ({
                        ...current,
                        mounts: current.mounts.map((mountItem, itemIndex) =>
                          itemIndex === index
                            ? { ...mountItem, type: event.target.value }
                            : mountItem,
                        ),
                      }))
                    }
                    value={item.type}
                  >
                    <option value="bind">Bind</option>
                    <option value="volume">Volume</option>
                  </AppSelect>
                  <AppTextField
                    disabled={pending}
                    fullWidth
                    label="Source"
                    list={item.type === "volume" ? volumeListId : undefined}
                    onChange={(event) =>
                      setConfiguration((current) => ({
                        ...current,
                        mounts: current.mounts.map((mountItem, itemIndex) =>
                          itemIndex === index
                            ? { ...mountItem, source: event.target.value }
                            : mountItem,
                        ),
                      }))
                    }
                    value={item.source}
                  />
                  <AppTextField
                    disabled={pending}
                    fullWidth
                    label="Container path"
                    onChange={(event) =>
                      setConfiguration((current) => ({
                        ...current,
                        mounts: current.mounts.map((mountItem, itemIndex) =>
                          itemIndex === index
                            ? { ...mountItem, destination: event.target.value }
                            : mountItem,
                        ),
                      }))
                    }
                    placeholder="/data"
                    value={item.destination}
                  />
                  <label className="container-form-dialog__checkbox">
                    <AppCheckbox
                      checked={item.readOnly}
                      disabled={pending}
                      onChange={(_, checked) =>
                        setConfiguration((current) => ({
                          ...current,
                          mounts: current.mounts.map((mountItem, itemIndex) =>
                            itemIndex === index
                              ? { ...mountItem, readOnly: checked }
                              : mountItem,
                          ),
                        }))
                      }
                    />
                    <AppTypography variant="body2">Read-only</AppTypography>
                  </label>
                  <AppIconButton
                    aria-label={`Remove mount ${index + 1}`}
                    disabled={pending}
                    onClick={() =>
                      setConfiguration((current) => ({
                        ...current,
                        mounts: current.mounts.filter(
                          (_, itemIndex) => itemIndex !== index,
                        ),
                      }))
                    }
                    size="small"
                  >
                    <Icon icon="mdi:delete-outline" width={18} />
                  </AppIconButton>
                </div>
              ))}
            </div>
            <AppButton
              disabled={pending}
              onClick={() =>
                setConfiguration((current) => ({
                  ...current,
                  mounts: [
                    ...current.mounts,
                    {
                      type: "bind",
                      source: "",
                      destination: "",
                      readOnly: false,
                    },
                  ],
                }))
              }
              size="small"
              startIcon={<Icon icon="mdi:plus" width={18} />}
            >
              Add mount
            </AppButton>
          </FormSection>

          <FormSection
            expanded={sections.networks}
            id="container-networks-section"
            onToggle={() => toggleSection("networks")}
            title="Networks and aliases"
          >
            <div className="container-form-dialog__rows">
              {configuration.networks.map((item, index) => (
                <div className="container-form-dialog__network-row" key={index}>
                  <AppSelect
                    disabled={pending}
                    fullWidth
                    label="Network"
                    onChange={(event) =>
                      setConfiguration((current) => ({
                        ...current,
                        networks: current.networks.map(
                          (networkItem, itemIndex) =>
                            itemIndex === index
                              ? { ...networkItem, name: event.target.value }
                              : networkItem,
                        ),
                      }))
                    }
                    value={item.name}
                  >
                    {!networks.some(
                      (network) => network.Name === item.name,
                    ) && <option value={item.name}>{item.name}</option>}
                    {networks.map((network) => (
                      <option
                        key={network.Id || network.Name}
                        value={network.Name}
                      >
                        {network.Name} ({network.Driver})
                      </option>
                    ))}
                  </AppSelect>
                  <AppTextField
                    disabled={pending}
                    fullWidth
                    helperText="Comma-separated"
                    label="Aliases"
                    onChange={(event) =>
                      setConfiguration((current) => ({
                        ...current,
                        networks: current.networks.map(
                          (networkItem, itemIndex) =>
                            itemIndex === index
                              ? {
                                  ...networkItem,
                                  aliases: event.target.value.split(","),
                                }
                              : networkItem,
                        ),
                      }))
                    }
                    value={item.aliases.join(", ")}
                  />
                  <AppIconButton
                    aria-label={`Remove network ${index + 1}`}
                    disabled={pending}
                    onClick={() =>
                      setConfiguration((current) => ({
                        ...current,
                        networks: current.networks.filter(
                          (_, itemIndex) => itemIndex !== index,
                        ),
                      }))
                    }
                    size="small"
                  >
                    <Icon icon="mdi:delete-outline" width={18} />
                  </AppIconButton>
                </div>
              ))}
            </div>
            <AppButton
              disabled={pending || networks.length === 0}
              onClick={() =>
                setConfiguration((current) => ({
                  ...current,
                  networks: [
                    ...current.networks,
                    { name: networks[0]?.Name ?? "", aliases: [] },
                  ],
                }))
              }
              size="small"
              startIcon={<Icon icon="mdi:plus" width={18} />}
            >
              Add network
            </AppButton>
          </FormSection>

          <FormSection
            expanded={sections.runtime}
            id="container-runtime-section"
            onToggle={() => toggleSection("runtime")}
            title="Runtime"
          >
            <div className="container-form-dialog__runtime-grid">
              <AppSelect
                disabled={pending}
                fullWidth
                label="Restart policy"
                onChange={(event) =>
                  setConfiguration((current) => ({
                    ...current,
                    restartPolicy: {
                      ...current.restartPolicy,
                      name: event.target.value,
                    },
                  }))
                }
                value={configuration.restartPolicy.name}
              >
                <option value="no">No</option>
                <option value="always">Always</option>
                <option value="unless-stopped">Unless stopped</option>
                <option value="on-failure">On failure</option>
              </AppSelect>
              {configuration.restartPolicy.name === "on-failure" && (
                <AppTextField
                  disabled={pending}
                  fullWidth
                  label="Maximum retries"
                  onChange={(event) =>
                    setConfiguration((current) => ({
                      ...current,
                      restartPolicy: {
                        ...current.restartPolicy,
                        maximumRetryCount: Number(event.target.value),
                      },
                    }))
                  }
                  type="number"
                  value={configuration.restartPolicy.maximumRetryCount}
                />
              )}
              <AppTextField
                disabled={pending}
                fullWidth
                label="User"
                onChange={(event) =>
                  setConfiguration((current) => ({
                    ...current,
                    user: event.target.value,
                  }))
                }
                placeholder="1000:1000"
                value={configuration.user}
              />
              <AppTextField
                disabled={pending}
                fullWidth
                label="Working directory"
                onChange={(event) =>
                  setConfiguration((current) => ({
                    ...current,
                    workingDirectory: event.target.value,
                  }))
                }
                placeholder="/app"
                value={configuration.workingDirectory}
              />
            </div>
          </FormSection>

          {mode === "create" && (
            <label className="container-form-dialog__checkbox">
              <AppCheckbox
                checked={start}
                disabled={pending}
                onChange={(_, checked) => setStart(checked)}
              />
              <AppTypography variant="body2">
                Start container after creation
              </AppTypography>
            </label>
          )}
          {error && (
            <AppTypography color="error" role="alert" variant="body2">
              {error}
            </AppTypography>
          )}
        </AppDialogContent>
        <AppDialogActions>
          <AppButton disabled={pending} onClick={onClose}>
            Cancel
          </AppButton>
          <AppButton disabled={pending} type="submit" variant="contained">
            {pending
              ? mode === "create"
                ? "Creating…"
                : "Preparing…"
              : mode === "create"
                ? "Create container"
                : "Review changes"}
          </AppButton>
        </AppDialogActions>
      </form>
    </GeneralDialog>
  );
};

const ContainerFormDialog = ({
  containerId,
  mode,
  onClose,
  open,
}: ContainerFormDialogProps) => {
  const inspectQuery = useQuery({
    ...linuxio.docker.inspect_container({ containerId: containerId ?? "" }),
    enabled: open && mode === "edit" && Boolean(containerId),
  });
  const imagesQuery = useQuery({
    ...linuxio.docker.list_images,
    enabled: open,
  });
  const networksQuery = useQuery({
    ...linuxio.docker.list_networks,
    enabled: open,
  });
  const volumesQuery = useQuery({
    ...linuxio.docker.list_volumes,
    enabled: open,
  });

  if (!open) return null;

  const loading =
    imagesQuery.isPending ||
    networksQuery.isPending ||
    volumesQuery.isPending ||
    (mode === "edit" && inspectQuery.isPending);
  if (loading) {
    return (
      <GeneralDialog
        aria-label={mode === "create" ? "Create container" : "Edit container"}
        fullWidth
        maxWidth="sm"
        onClose={onClose}
        open
      >
        <AppDialogTitle>
          {mode === "create" ? "Create container" : "Edit container"}
        </AppDialogTitle>
        <AppDialogContent className="container-form-dialog__loading">
          <AppCircularProgress />
          <AppTypography color="text.secondary" variant="body2">
            Loading container options…
          </AppTypography>
        </AppDialogContent>
      </GeneralDialog>
    );
  }

  const loadingError =
    imagesQuery.error ??
    networksQuery.error ??
    volumesQuery.error ??
    (mode === "edit" ? inspectQuery.error : undefined);
  if (loadingError) {
    return (
      <GeneralDialog
        aria-label="Unable to load container form"
        fullWidth
        maxWidth="sm"
        onClose={onClose}
        open
      >
        <AppDialogTitle>Unable to load container form</AppDialogTitle>
        <AppDialogContent>
          <AppTypography color="error" role="alert" variant="body2">
            {errorMessage(loadingError)}
          </AppTypography>
        </AppDialogContent>
        <AppDialogActions>
          <AppButton onClick={onClose}>Close</AppButton>
          <AppButton
            onClick={() => {
              void imagesQuery.refetch();
              void networksQuery.refetch();
              void volumesQuery.refetch();
              if (mode === "edit") void inspectQuery.refetch();
            }}
            variant="contained"
          >
            Retry
          </AppButton>
        </AppDialogActions>
      </GeneralDialog>
    );
  }

  if (mode === "edit" && (!containerId || !inspectQuery.data)) {
    return (
      <GeneralDialog
        aria-label="Unable to edit container"
        fullWidth
        maxWidth="sm"
        onClose={onClose}
        open
      >
        <AppDialogTitle>Unable to edit container</AppDialogTitle>
        <AppDialogContent>
          <AppTypography color="error" role="alert" variant="body2">
            {errorMessage(inspectQuery.error)}
          </AppTypography>
        </AppDialogContent>
        <AppDialogActions>
          <AppButton onClick={onClose}>Close</AppButton>
          <AppButton
            onClick={() => void inspectQuery.refetch()}
            variant="contained"
          >
            Retry
          </AppButton>
        </AppDialogActions>
      </GeneralDialog>
    );
  }

  const availableNetworks = networksQuery.data ?? [];
  const initialConfiguration = inspectQuery.data
    ? configurationFromInspect(inspectQuery.data)
    : emptyConfiguration(availableNetworks);
  return (
    <ContainerConfigurationForm
      images={imagesQuery.data ?? []}
      initialConfiguration={initialConfiguration}
      inspect={inspectQuery.data}
      key={`${mode}:${inspectQuery.data?.id ?? "new"}`}
      mode={mode}
      networks={availableNetworks}
      onClose={onClose}
      volumes={volumesQuery.data ?? []}
    />
  );
};

export default ContainerFormDialog;
