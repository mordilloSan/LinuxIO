import { useSuspenseQuery } from "@tanstack/react-query";
import { getRouteApi } from "@tanstack/react-router";
import { useCallback, useMemo, useState, type SyntheticEvent } from "react";

import { linuxio, type DockerNetwork, useCallMutation } from "@/api";
import NetworkCard from "@/components/cards/NetworkCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import BatchDeleteDialog from "@/components/docker/BatchDeleteDialog";
import DockerResourceDetailsLayout from "@/components/docker/DockerResourceDetailsLayout";
import ReorderableCardGrid from "@/components/reorder/ReorderableCardGrid";
import { RoutedTabSearch } from "@/components/tabbar";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import AppCollapse from "@/components/ui/AppCollapse";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppHeaderSearch from "@/components/ui/AppHeaderSearch";
import AppSelect from "@/components/ui/AppSelect";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import SectionHeader from "@/components/ui/SectionHeader";
import { useFocusedResourceParam } from "@/hooks/useFocusedResourceParam";
import { useRegisterCreateHandler } from "@/hooks/useRegisterCreateHandler";
import { useReorderableSurface } from "@/hooks/useReorderableSurface";
import { useReorderableTableDnd } from "@/hooks/useReorderableTableDnd";
import { useScopedToast } from "@/hooks/useScopedToast";
import { CARD_GRID_SIZE_STANDARD } from "@/theme/constants";
import {
  longTextStyles,
  responsiveTextStyles,
  wrappableChipStyle,
  wrappableChipLabelStyle,
} from "@/theme/tableStyles";
import { getContainerName } from "@/utils/dockerContainer";
import { parseDockerKeyValueLines } from "@/utils/dockerKeyValues";

import "./network-list.css";

const DOCKER_TOAST_META = { label: "Open Docker", to: "/docker" } as const;
const DOCKER_RESOURCE_NAME = /^[a-zA-Z0-9][a-zA-Z0-9_.-]*$/;

interface NetworkListProps {
  onMountCreateHandler?: (handler: () => void) => void;
  viewMode?: "table" | "card";
}

interface ConnectedContainerRow {
  endpointId: string;
  id: string;
  ipv4: string;
  ipv6: string;
  mac: string;
  name: string;
}

const connectedContainerColumns: AppVirtualTableColumnDef<ConnectedContainerRow>[] =
  [
    {
      accessorKey: "name",
      header: "Name",
      cell: ({ row }) => (
        <AppTypography style={responsiveTextStyles} variant="body2">
          {row.original.name}
        </AppTypography>
      ),
    },
    {
      accessorKey: "id",
      header: "Container ID",
      cell: ({ row }) => (
        <span
          style={{
            fontFamily: "var(--app-font-mono)",
            ...longTextStyles,
          }}
        >
          {row.original.id.slice(0, 12)}
        </span>
      ),
    },
    {
      accessorKey: "endpointId",
      header: "Endpoint ID",
      cell: ({ row }) => (
        <span
          style={{
            fontFamily: "var(--app-font-mono)",
            ...longTextStyles,
          }}
        >
          {row.original.endpointId.slice(0, 12) || "-"}
        </span>
      ),
    },
    {
      accessorKey: "ipv4",
      header: "IPv4",
      cell: ({ row }) => (
        <AppTypography
          style={{
            fontFamily: "var(--app-font-mono)",
            ...longTextStyles,
          }}
          variant="body2"
        >
          {row.original.ipv4}
        </AppTypography>
      ),
    },
    {
      accessorKey: "ipv6",
      header: "IPv6",
      cell: ({ row }) => (
        <AppTypography
          style={{
            fontFamily: "var(--app-font-mono)",
            ...longTextStyles,
          }}
          variant="body2"
        >
          {row.original.ipv6}
        </AppTypography>
      ),
    },
    {
      accessorKey: "mac",
      header: "MAC",
      cell: ({ row }) => (
        <span
          style={{
            fontFamily: "var(--app-font-mono)",
            ...longTextStyles,
          }}
        >
          {row.original.mac}
        </span>
      ),
    },
  ];

interface CreateNetworkDialogProps {
  existingNames: string[];
  onClose: () => void;
  open: boolean;
}

const CreateNetworkDialog = ({
  open,
  onClose,
  existingNames,
}: CreateNetworkDialogProps) => {
  const toast = useScopedToast(DOCKER_TOAST_META);
  const [networkName, setNetworkName] = useState("");
  const [driver, setDriver] = useState("bridge");
  const [internal, setInternal] = useState(false);
  const [attachable, setAttachable] = useState(false);
  const [enableIpv6, setEnableIpv6] = useState(false);
  const [subnet, setSubnet] = useState("");
  const [gateway, setGateway] = useState("");
  const [optionsText, setOptionsText] = useState("");
  const [advanced, setAdvanced] = useState(false);
  const [formError, setFormError] = useState<string>();
  const parsedOptions = parseDockerKeyValueLines(optionsText);
  const trimmedName = networkName.trim();

  const { mutate: createNetwork, isPending: isCreating } = useCallMutation(
    linuxio.docker.create_network,
    {
      success: () => {
        toast.success(`Network "${trimmedName}" created`);
        handleClose();
      },
      error: "Failed to create network",
      toast: DOCKER_TOAST_META,
    },
  );

  const nameTaken = existingNames.includes(trimmedName);
  const isValidName = DOCKER_RESOURCE_NAME.test(trimmedName);

  const handleCreate = (event: SyntheticEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!trimmedName || !driver.trim()) {
      setFormError("Name and driver are required.");
      return;
    }
    if (nameTaken) {
      setFormError("Choose a network name that is not already in use.");
      return;
    }
    if (!isValidName) {
      setFormError(
        "Name must start with an alphanumeric character and use only letters, numbers, underscores, periods, or hyphens.",
      );
      return;
    }
    if (gateway.trim() && !subnet.trim()) {
      setFormError("Enter a subnet when a gateway is set.");
      return;
    }
    if (parsedOptions.error) {
      setFormError(parsedOptions.error);
      return;
    }
    setFormError(undefined);
    createNetwork({
      name: trimmedName,
      driver: driver.trim(),
      internal,
      attachable,
      enableIpv6,
      subnet: subnet.trim() || undefined,
      gateway: gateway.trim() || undefined,
      options:
        Object.keys(parsedOptions.values).length > 0
          ? parsedOptions.values
          : undefined,
    });
  };

  const handleClose = () => {
    setNetworkName("");
    setDriver("bridge");
    setInternal(false);
    setAttachable(false);
    setEnableIpv6(false);
    setSubnet("");
    setGateway("");
    setOptionsText("");
    setAdvanced(false);
    setFormError(undefined);
    onClose();
  };

  return (
    <GeneralDialog
      aria-label="Create network"
      fullWidth
      maxWidth="xs"
      onClose={isCreating ? undefined : handleClose}
      open={open}
    >
      <form onSubmit={handleCreate}>
        <AppDialogTitle>Create Network</AppDialogTitle>
        <AppDialogContent>
          <div
            style={{
              display: "flex",
              flexDirection: "column",
              gap: "var(--app-space-12)",
              marginTop: "var(--app-space-8)",
            }}
          >
            <AppTextField
              autoFocus
              disabled={isCreating}
              error={nameTaken || (networkName.length > 0 && !isValidName)}
              fullWidth
              helperText={
                nameTaken
                  ? "This network name already exists."
                  : networkName.length > 0 && !isValidName
                    ? "Use letters, numbers, underscores, periods, or hyphens."
                    : undefined
              }
              label="Name"
              name="network-name"
              onChange={(event) => setNetworkName(event.target.value)}
              required
              value={networkName}
            />
            <AppSelect
              disabled={isCreating}
              fullWidth
              label="Driver"
              onChange={(event) => setDriver(event.target.value)}
              value={driver}
            >
              <option value="bridge">bridge</option>
              <option value="host">host</option>
              <option value="overlay">overlay</option>
              <option value="macvlan">macvlan</option>
              <option value="none">none</option>
            </AppSelect>
            <AppFormControlLabel
              control={
                <AppSwitch
                  checked={internal}
                  disabled={isCreating}
                  onChange={(event) => setInternal(event.target.checked)}
                />
              }
              label="Internal network (no external connectivity)"
            />
            <AppFormControlLabel
              control={
                <AppSwitch
                  checked={attachable}
                  disabled={isCreating}
                  onChange={(event) => setAttachable(event.target.checked)}
                />
              }
              label="Allow manual container attachment"
            />
            <AppFormControlLabel
              control={
                <AppSwitch
                  checked={enableIpv6}
                  disabled={isCreating}
                  onChange={(event) => setEnableIpv6(event.target.checked)}
                />
              }
              label="Enable IPv6"
            />
            <AppTextField
              disabled={isCreating}
              fullWidth
              label="Subnet"
              name="network-subnet"
              onChange={(event) => setSubnet(event.target.value)}
              placeholder={enableIpv6 ? "2001:db8::/64" : "172.20.0.0/16"}
              value={subnet}
            />
            <AppTextField
              disabled={isCreating}
              error={Boolean(gateway.trim() && !subnet.trim())}
              fullWidth
              helperText={
                gateway.trim() && !subnet.trim()
                  ? "Enter a subnet before setting a gateway."
                  : undefined
              }
              label="Gateway"
              name="network-gateway"
              onChange={(event) => setGateway(event.target.value)}
              placeholder={enableIpv6 ? "2001:db8::1" : "172.20.0.1"}
              value={gateway}
            />
            <section>
              <SectionHeader
                controlsId="network-driver-options"
                expanded={advanced}
                onToggle={() => setAdvanced((current) => !current)}
                title="Advanced"
              />
              <AppCollapse in={advanced} unmountOnExit>
                <div id="network-driver-options">
                  <AppTextField
                    disabled={isCreating}
                    error={Boolean(parsedOptions.error)}
                    fullWidth
                    helperText={
                      parsedOptions.error ??
                      "Optional, one key=value driver option per line."
                    }
                    label="Driver options"
                    multiline
                    name="network-driver-options"
                    onChange={(event) => setOptionsText(event.target.value)}
                    rows={4}
                    value={optionsText}
                  />
                </div>
              </AppCollapse>
            </section>
            {formError && (
              <AppTypography color="error" role="alert" variant="body2">
                {formError}
              </AppTypography>
            )}
          </div>
        </AppDialogContent>
        <AppDialogActions>
          <AppButton disabled={isCreating} onClick={handleClose} type="button">
            Cancel
          </AppButton>
          <AppButton disabled={isCreating} type="submit" variant="contained">
            {isCreating ? "Creating…" : "Create network"}
          </AppButton>
        </AppDialogActions>
      </form>
    </GeneralDialog>
  );
};

const getNetworkId = (network: { Id: string }) => network.Id;

const dockerRouteApi = getRouteApi("/_authenticated/docker/networks");

const NetworkDetailsContent = ({
  connectDisabled,
  network,
  onConnect,
  onDisconnect,
}: {
  connectDisabled: boolean;
  network: DockerNetwork;
  onConnect: () => void;
  onDisconnect: (container: ConnectedContainerRow) => void;
}) => {
  const columns: AppVirtualTableColumnDef<ConnectedContainerRow>[] = [
    ...connectedContainerColumns,
    {
      id: "actions",
      header: "Actions",
      enableSorting: false,
      cell: ({ row }) => (
        <AppActionIconButton
          ariaLabel={`Disconnect container ${row.original.name}`}
          color="var(--app-palette-error-main)"
          icon="mdi:lan-disconnect"
          iconSize={18}
          label="Disconnect container"
          onClick={() => onDisconnect(row.original)}
        />
      ),
      meta: { align: "right", width: "80px" },
    },
  ];
  return (
    <div className="expand-panel">
      <div>
        <AppTypography gutterBottom variant="subtitle2">
          <b>Full Network ID:</b>
        </AppTypography>
        <AppTypography
          className="expand-panel__mono"
          style={longTextStyles}
          variant="body2"
        >
          {network.Id}
        </AppTypography>
      </div>
      <div>
        <AppTypography gutterBottom variant="subtitle2">
          <b>Subnet(s):</b>
        </AppTypography>
        <div className="expand-panel__chips">
          {network.IPAM?.Config?.length ? (
            network.IPAM.Config.map((ipam, index) => (
              <Chip
                key={index}
                label={`${ipam.Subnet} / Gateway: ${ipam.Gateway}`}
                size="small"
                style={wrappableChipStyle}
                labelStyle={wrappableChipLabelStyle}
                variant="soft"
              />
            ))
          ) : (
            <AppTypography color="text.secondary" variant="body2">
              (no IPAM config)
            </AppTypography>
          )}
        </div>
      </div>
      <div>
        <AppTypography gutterBottom variant="subtitle2">
          <b>Network Details:</b>
        </AppTypography>
        <div className="expand-panel__chips">
          {network.Created && (
            <Chip
              label={`Created: ${new Date(network.Created).toLocaleString()}`}
              size="small"
              variant="soft"
            />
          )}
          <Chip
            label={`Driver: ${network.Driver}`}
            size="small"
            variant="soft"
          />
          <Chip label={`Scope: ${network.Scope}`} size="small" variant="soft" />
          <Chip
            label={`Internal: ${network.Internal ? "Yes" : "No"}`}
            size="small"
            variant="soft"
          />
          <Chip
            label={`Attachable: ${network.Attachable ? "Yes" : "No"}`}
            size="small"
            variant="soft"
          />
          <Chip
            label={`Ingress: ${network.Ingress ? "Yes" : "No"}`}
            size="small"
            variant="soft"
          />
          <Chip
            label={`Config only: ${network.ConfigOnly ? "Yes" : "No"}`}
            size="small"
            variant="soft"
          />
          <Chip
            label={`IPv4: ${network.EnableIPv4 !== false ? "Yes" : "No"}`}
            size="small"
            variant="soft"
          />
          <Chip
            label={`IPv6: ${network.EnableIPv6 ? "Yes" : "No"}`}
            size="small"
            variant="soft"
          />
        </div>
        {network.Protected && (
          <AppTypography
            color="text.secondary"
            style={{ marginTop: "var(--app-space-4)" }}
            variant="body2"
          >
            Docker default networks cannot be deleted.
          </AppTypography>
        )}
      </div>
      <div>
        <AppTypography gutterBottom variant="subtitle2">
          <b>Options:</b>
        </AppTypography>
        <div className="expand-panel__chips">
          {Object.entries(network.Options ?? {}).length ? (
            Object.entries(network.Options ?? {}).map(([key, value]) => (
              <Chip
                key={key}
                label={`${key}: ${value}`}
                size="small"
                style={wrappableChipStyle}
                labelStyle={wrappableChipLabelStyle}
                variant="soft"
              />
            ))
          ) : (
            <AppTypography color="text.secondary" variant="body2">
              (no options)
            </AppTypography>
          )}
        </div>
      </div>
      {(network.IPAM?.Driver ||
        Object.keys(network.IPAM?.Options ?? {}).length > 0) && (
        <div>
          <AppTypography gutterBottom variant="subtitle2">
            <b>IPAM:</b>
          </AppTypography>
          <div className="expand-panel__chips">
            {network.IPAM?.Driver && (
              <Chip
                label={`Driver: ${network.IPAM.Driver}`}
                size="small"
                variant="soft"
              />
            )}
            {Object.entries(network.IPAM?.Options ?? {}).map(([key, value]) => (
              <Chip
                key={key}
                label={`${key}: ${value}`}
                size="small"
                style={wrappableChipStyle}
                labelStyle={wrappableChipLabelStyle}
                variant="soft"
              />
            ))}
          </div>
        </div>
      )}
      {network.IPAM?.Config?.some(
        (config) =>
          config.IPRange ||
          Object.keys(config.AuxiliaryAddresses ?? {}).length > 0,
      ) && (
        <div>
          <AppTypography gutterBottom variant="subtitle2">
            <b>IPAM Ranges:</b>
          </AppTypography>
          <div className="expand-panel__chips">
            {network.IPAM.Config.flatMap((config, index) => [
              ...(config.IPRange
                ? [
                    <Chip
                      key={`${index}-range`}
                      label={`Range: ${config.IPRange}`}
                      size="small"
                      variant="soft"
                    />,
                  ]
                : []),
              ...Object.entries(config.AuxiliaryAddresses ?? {}).map(
                ([key, value]) => (
                  <Chip
                    key={`${index}-${key}`}
                    label={`${key}: ${value}`}
                    size="small"
                    style={wrappableChipStyle}
                    labelStyle={wrappableChipLabelStyle}
                    variant="soft"
                  />
                ),
              ),
            ])}
          </div>
        </div>
      )}
      <div>
        <AppTypography gutterBottom variant="subtitle2">
          <b>Labels:</b>
        </AppTypography>
        <div
          style={{
            alignItems: "flex-start",
            display: "flex",
            flexDirection: "column",
            gap: "var(--app-space-4)",
          }}
        >
          {Object.entries(network.Labels ?? {}).length ? (
            Object.entries(network.Labels ?? {}).map(([key, value]) => (
              <Chip
                key={key}
                label={`${key}: ${value}`}
                size="small"
                style={wrappableChipStyle}
                labelStyle={wrappableChipLabelStyle}
                variant="soft"
              />
            ))
          ) : (
            <AppTypography color="text.secondary" variant="body2">
              (no labels)
            </AppTypography>
          )}
        </div>
      </div>
      <div>
        <div
          style={{
            alignItems: "center",
            display: "flex",
            gap: "var(--app-space-8)",
            justifyContent: "space-between",
          }}
        >
          <AppTypography gutterBottom variant="subtitle2">
            <b>Connected Containers:</b>
          </AppTypography>
          <AppButton
            disabled={connectDisabled}
            onClick={onConnect}
            size="small"
            variant="outlined"
          >
            Connect container
          </AppButton>
        </div>
        {Object.entries(network.Containers ?? {}).length ? (
          <AppVirtualTable
            ariaLabel="Connected containers"
            className="network-connected-table"
            columns={columns}
            data={Object.entries(network.Containers ?? {}).map(
              ([id, info]) => ({
                endpointId: info.EndpointID || "",
                id,
                ipv4: info.IPv4Address?.replace(/\/.*/, "") || "-",
                ipv6: info.IPv6Address?.replace(/\/.*/, "") || "-",
                mac: info.MacAddress || "-",
                name: info.Name || "-",
              }),
            )}
            density="compact"
            fillAvailable={false}
            getRowId={(container) => container.id}
            maxHeight={240}
            variant="embedded"
          />
        ) : (
          <AppTypography color="text.secondary" variant="body2">
            (no containers)
          </AppTypography>
        )}
      </div>
    </div>
  );
};

const NetworkList = ({
  onMountCreateHandler,
  viewMode = "table",
}: NetworkListProps) => {
  const navigate = dockerRouteApi.useNavigate();
  const searchParams = dockerRouteApi.useSearch();
  const focusedNetworkId =
    typeof searchParams.network === "string" ? searchParams.network : undefined;
  const { data: rawNetworks } = useSuspenseQuery({
    ...linuxio.docker.list_networks,
    ...{
      refetchInterval: 10000,
    },
  });
  const networks = rawNetworks;
  const { data: containers } = useSuspenseQuery({
    ...linuxio.docker.list_containers,
    refetchInterval: 10000,
  });
  const toast = useScopedToast(DOCKER_TOAST_META);

  const [search, setSearch] = useState("");
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [connectDialogOpen, setConnectDialogOpen] = useState(false);
  const [connectContainerId, setConnectContainerId] = useState("");
  const [aliases, setAliases] = useState("");
  const [disconnectTarget, setDisconnectTarget] =
    useState<ConnectedContainerRow | null>(null);

  const updateFocusedNetwork = useCallback(
    (networkId: string | null) => {
      void navigate({
        to: "/docker/networks",
        search: (previous) => ({
          ...previous,
          network: networkId ?? undefined,
        }),
      });
    },
    [navigate],
  );

  const surface = useReorderableSurface({
    getId: getNetworkId,
    items: networks,
    surface: "docker.networks",
  });
  const tableDnd = useReorderableTableDnd<
    (typeof networks)[number],
    (typeof networks)[number]
  >({ handleAriaLabel: "Reorder network", surface });
  const filtered = surface.items.filter((net) =>
    net.Name.toLowerCase().includes(search.toLowerCase()),
  );
  const focusedNetwork = useFocusedResourceParam({
    focusedId: focusedNetworkId,
    getId: getNetworkId,
    items: surface.items,
    onClear: () => updateFocusedNetwork(null),
  });
  const connectedContainerIds = new Set(
    Object.keys(focusedNetwork?.Containers ?? {}),
  );
  const unattachedContainers = containers.filter(
    (container) => !connectedContainerIds.has(container.Id),
  );

  // Create network handler
  const handleCreateNetwork = useCallback(() => {
    setCreateDialogOpen(true);
  }, []);

  useRegisterCreateHandler(onMountCreateHandler, handleCreateNetwork);

  // Configless: this is a batch flow — the dialog owns aggregation and toasts.
  const { mutateAsync: deleteNetwork } = useCallMutation(
    linuxio.docker.delete_network,
  );
  const { mutate: connectNetwork, isPending: isConnecting } = useCallMutation(
    linuxio.docker.connect_network,
    {
      success: () => {
        toast.success("Container connected to network");
        setConnectDialogOpen(false);
        setAliases("");
      },
      error: "Failed to connect container",
      toast: DOCKER_TOAST_META,
    },
  );
  const { mutate: disconnectNetwork, isPending: isDisconnecting } =
    useCallMutation(linuxio.docker.disconnect_network, {
      success: () => {
        toast.success("Container disconnected from network");
        setDisconnectTarget(null);
      },
      error: "Failed to disconnect container",
      toast: DOCKER_TOAST_META,
    });
  const handleDeleteSuccess = () => {
    updateFocusedNetwork(null);
  };

  const handleNetworkRowClick = useCallback(
    ({ original: network }: { original: { Id: string } }) =>
      updateFocusedNetwork(network.Id),
    [updateFocusedNetwork],
  );
  const openConnectDialog = () => {
    setConnectContainerId(unattachedContainers[0]?.Id ?? "");
    setAliases("");
    setConnectDialogOpen(true);
  };
  const handleConnect = (event: SyntheticEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!focusedNetwork || !connectContainerId) return;
    connectNetwork({
      networkId: focusedNetwork.Id,
      containerId: connectContainerId,
      aliases: aliases
        .split(",")
        .map((alias) => alias.trim())
        .filter(Boolean),
    });
  };

  // Stable column defs — see docs/table-row-gestures.md: a rebuilt array
  // remounts every cell subtree on the press that arms the reorder hold.
  const columns = useMemo<
    AppVirtualTableColumnDef<(typeof filtered)[number]>[]
  >(
    () => [
      {
        accessorKey: "Name",
        header: "Network Name",
        cell: ({ row }) => (
          <AppTypography
            fontWeight={500}
            style={responsiveTextStyles}
            variant="body2"
          >
            {row.original.Name}
          </AppTypography>
        ),
        meta: { align: "left" },
      },
      {
        accessorKey: "Driver",
        header: "Driver",
        cell: ({ row }) => (
          <Chip
            label={
              <AppTypography component="span" variant="caption">
                {row.original.Driver}
              </AppTypography>
            }
            size="small"
            variant="soft"
          />
        ),
        meta: {
          align: "left",
          width: "120px",
        },
      },
      {
        accessorKey: "Scope",
        header: "Scope",
        cell: ({ row }) => (
          <AppTypography style={responsiveTextStyles} variant="body2">
            {row.original.Scope}
          </AppTypography>
        ),
        meta: {
          align: "left",
          hideBelow: "md",
          width: "100px",
        },
      },
      {
        accessorKey: "Internal",
        header: "Internal",
        cell: ({ row }) => (
          <Chip
            color={row.original.Internal ? "warning" : "default"}
            label={row.original.Internal ? "Yes" : "No"}
            size="small"
            variant="soft"
          />
        ),
        meta: {
          align: "left",
          hideBelow: "md",
          width: "100px",
        },
      },
      {
        id: "features",
        header: "Features",
        cell: ({ row }) => {
          const features = [
            row.original.Attachable && "Attachable",
            row.original.Ingress && "Ingress",
            row.original.ConfigOnly && "Config only",
          ].filter(Boolean);
          return (
            <AppTypography style={responsiveTextStyles} variant="body2">
              {features.length > 0 ? features.join(", ") : "-"}
            </AppTypography>
          );
        },
        meta: {
          align: "left",
          hideBelow: "lg",
          width: "150px",
        },
      },
      {
        accessorKey: "Created",
        header: "Created",
        cell: ({ row }) => (
          <AppTypography style={responsiveTextStyles} variant="body2">
            {row.original.Created
              ? new Date(row.original.Created).toLocaleDateString()
              : "-"}
          </AppTypography>
        ),
        meta: {
          align: "left",
          hideBelow: "lg",
          width: "120px",
        },
      },
      {
        accessorKey: "EnableIPv4",
        header: "IPv4",
        cell: ({ row }) => (
          <Chip
            color={row.original.EnableIPv4 !== false ? "success" : "default"}
            label={row.original.EnableIPv4 !== false ? "Yes" : "No"}
            size="small"
            variant="soft"
          />
        ),
        meta: {
          align: "left",
          hideBelow: "lg",
          width: "100px",
        },
      },
      {
        accessorKey: "EnableIPv6",
        header: "IPv6",
        cell: ({ row }) => (
          <Chip
            color={row.original.EnableIPv6 ? "success" : "default"}
            label={row.original.EnableIPv6 ? "Yes" : "No"}
            size="small"
            variant="soft"
          />
        ),
        meta: {
          align: "left",
          hideBelow: "lg",
          width: "100px",
        },
      },
      {
        accessorKey: "Id",
        header: "Network ID",
        cell: ({ row }) => (
          <AppTypography
            style={{
              fontFamily: "var(--app-font-mono)",
              ...responsiveTextStyles,
            }}
            variant="body2"
          >
            {row.original.Id?.slice(0, 12)}
          </AppTypography>
        ),
        meta: {
          align: "left",
          hideBelow: "md",
          width: "140px",
        },
      },
    ],
    [],
  );

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        height: "100%",
        minHeight: 0,
      }}
    >
      {!focusedNetwork && (
        <RoutedTabSearch active={search !== ""}>
          <AppHeaderSearch
            clearOnDocumentEscape
            onChange={setSearch}
            placeholder="Search networks…"
            value={search}
          />
        </RoutedTabSearch>
      )}
      {focusedNetwork ? (
        <DockerResourceDetailsLayout
          onClose={() => updateFocusedNetwork(null)}
          resourceLabel="network"
          subtitle={`${focusedNetwork.Driver} · ${focusedNetwork.Scope}`}
          summary={
            <NetworkCard
              actions={
                focusedNetwork.Protected ? undefined : (
                  <AppActionIconButton
                    ariaLabel={`Delete network ${focusedNetwork.Name}`}
                    color="var(--app-palette-error-main)"
                    icon="mdi:delete"
                    iconSize={18}
                    label="Delete network"
                    onClick={() => setDeleteDialogOpen(true)}
                  />
                )
              }
              network={focusedNetwork}
              selected
            />
          }
          title={focusedNetwork.Name}
        >
          <NetworkDetailsContent
            connectDisabled={unattachedContainers.length === 0}
            network={focusedNetwork}
            onConnect={openConnectDialog}
            onDisconnect={setDisconnectTarget}
          />
        </DockerResourceDetailsLayout>
      ) : viewMode === "card" ? (
        filtered.length > 0 ? (
          <ReorderableCardGrid
            fillAvailable
            getId={getNetworkId}
            items={filtered}
            renderItem={(network) => (
              <NetworkCard
                network={network}
                onOpen={
                  surface.editMode
                    ? undefined
                    : () => updateFocusedNetwork(network.Id)
                }
              />
            )}
            size={CARD_GRID_SIZE_STANDARD}
            surface={surface}
          />
        ) : (
          <div
            style={{
              textAlign: "center",
              paddingTop: "var(--app-space-16)",
              paddingBottom: "var(--app-space-16)",
            }}
          >
            <AppTypography color="text.secondary" variant="body2">
              No networks found.
            </AppTypography>
          </div>
        )
      ) : (
        <AppVirtualTable
          ariaLabel="Docker networks"
          columns={columns}
          data={filtered}
          dnd={tableDnd}
          emptyMessage="No networks found."
          fillAvailable
          getRowId={getNetworkId}
          onRowClick={surface.editMode ? undefined : handleNetworkRowClick}
          selectedRowId={focusedNetworkId}
        />
      )}

      <CreateNetworkDialog
        existingNames={networks.map((n) => n.Name)}
        onClose={() => setCreateDialogOpen(false)}
        open={createDialogOpen}
      />

      <GeneralDialog
        aria-label={
          focusedNetwork
            ? `Connect container to ${focusedNetwork.Name}`
            : "Connect container to network"
        }
        fullWidth
        maxWidth="xs"
        onClose={
          isConnecting
            ? undefined
            : () => {
                setConnectDialogOpen(false);
                setAliases("");
              }
        }
        open={connectDialogOpen && Boolean(focusedNetwork)}
      >
        <form onSubmit={handleConnect}>
          <AppDialogTitle>
            Connect Container to {focusedNetwork?.Name}
          </AppDialogTitle>
          <AppDialogContent>
            <div
              style={{
                display: "flex",
                flexDirection: "column",
                gap: "var(--app-space-12)",
                marginTop: "var(--app-space-8)",
              }}
            >
              <AppSelect
                disabled={isConnecting}
                fullWidth
                label="Container"
                onChange={(event) => setConnectContainerId(event.target.value)}
                value={connectContainerId}
              >
                {unattachedContainers.map((container) => (
                  <option key={container.Id} value={container.Id}>
                    {getContainerName(container)} ({container.State})
                  </option>
                ))}
              </AppSelect>
              <AppTextField
                disabled={isConnecting}
                fullWidth
                helperText="Optional, separated with commas."
                label="Aliases"
                name="network-aliases"
                onChange={(event) => setAliases(event.target.value)}
                placeholder="api, web"
                value={aliases}
              />
            </div>
          </AppDialogContent>
          <AppDialogActions>
            <AppButton
              disabled={isConnecting}
              onClick={() => {
                setConnectDialogOpen(false);
                setAliases("");
              }}
              type="button"
            >
              Cancel
            </AppButton>
            <AppButton
              disabled={isConnecting || !connectContainerId}
              type="submit"
              variant="contained"
            >
              {isConnecting ? "Connecting…" : "Connect container"}
            </AppButton>
          </AppDialogActions>
        </form>
      </GeneralDialog>

      <GeneralDialog
        aria-label={
          disconnectTarget
            ? `Disconnect ${disconnectTarget.name} from ${focusedNetwork?.Name}?`
            : "Disconnect container from network?"
        }
        fullWidth
        maxWidth="xs"
        onClose={isDisconnecting ? undefined : () => setDisconnectTarget(null)}
        open={Boolean(disconnectTarget && focusedNetwork)}
      >
        <AppDialogTitle>
          Disconnect {disconnectTarget?.name} from {focusedNetwork?.Name}?
        </AppDialogTitle>
        <AppDialogContent>
          <AppDialogContentText>
            The container will immediately lose access to this network and its
            connected services.
          </AppDialogContentText>
        </AppDialogContent>
        <AppDialogActions>
          <AppButton
            disabled={isDisconnecting}
            onClick={() => setDisconnectTarget(null)}
          >
            Cancel
          </AppButton>
          <AppButton
            color="error"
            disabled={isDisconnecting}
            onClick={() => {
              if (focusedNetwork && disconnectTarget) {
                disconnectNetwork({
                  networkId: focusedNetwork.Id,
                  containerId: disconnectTarget.id,
                });
              }
            }}
            variant="contained"
          >
            {isDisconnecting ? "Disconnecting…" : "Disconnect container"}
          </AppButton>
        </AppDialogActions>
      </GeneralDialog>

      <BatchDeleteDialog
        items={
          focusedNetwork
            ? [{ key: focusedNetwork.Id, label: focusedNetwork.Name }]
            : []
        }
        noun="network"
        onClose={() => setDeleteDialogOpen(false)}
        onDeleteOne={(item) => deleteNetwork({ id: item.key })}
        onSuccess={handleDeleteSuccess}
        open={deleteDialogOpen}
        warning="Networks with connected containers cannot be deleted."
      />
    </div>
  );
};

export default NetworkList;
