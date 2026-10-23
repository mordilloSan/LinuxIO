import { useSuspenseQuery } from "@tanstack/react-query";
import { getRouteApi } from "@tanstack/react-router";
import { useCallback, useMemo, useState, type SyntheticEvent } from "react";

import { linuxio, useCallMutation } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import ReorderableCardGrid from "@/components/reorder/ReorderableCardGrid";
import { useReorderableSurface } from "@/components/reorder/useReorderableSurface";
import { RoutedTabSearch } from "@/components/tabbar";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import { useReorderableTableDnd } from "@/components/tables/useReorderableTableDnd";
import { RESPONSIVE_TEXT_CLASS } from "@/components/tables/wrappingText";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppHeaderSearch from "@/components/ui/AppHeaderSearch";
import AppSelect from "@/components/ui/AppSelect";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import { useRegisterCreateHandler } from "@/hooks/useRegisterCreateHandler";
import { useScopedToast } from "@/hooks/useScopedToast";
import { CARD_GRID_SIZE_STANDARD } from "@/theme/constants";
import { getContainerName } from "@/utils/dockerContainer";

import { CreateNetworkDialog } from "./CreateNetworkDialog";
import {
  NetworkDetailsContent,
  type ConnectedContainerRow,
} from "./NetworkDetailsContent";
import { useFocusedResourceParam } from "../../-hooks/useFocusedResourceParam";
import DockerNetworkCard from "../cards/DockerNetworkCard";
import BatchDeleteDialog from "../shared/BatchDeleteDialog";
import DockerResourceDetailsLayout from "../shared/DockerResourceDetailsLayout";
import { DOCKER_TOAST_META } from "../shared/dockerToast";

interface NetworkListProps {
  onMountCreateHandler?: (handler: () => void) => void;
  viewMode?: "table" | "card";
}

const getNetworkId = (network: { Id: string }) => network.Id;

const dockerRouteApi = getRouteApi("/_authenticated/docker/networks");

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
            className={RESPONSIVE_TEXT_CLASS}
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
          <AppTypography className={RESPONSIVE_TEXT_CLASS} variant="body2">
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
            <AppTypography className={RESPONSIVE_TEXT_CLASS} variant="body2">
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
          <AppTypography className={RESPONSIVE_TEXT_CLASS} variant="body2">
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
            className={RESPONSIVE_TEXT_CLASS}
            style={{ fontFamily: "var(--app-font-mono)" }}
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
            <DockerNetworkCard
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
              <DockerNetworkCard
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
        <form className="app-dialog-form" onSubmit={handleConnect}>
          <AppDialogTitle>
            Connect Container to {focusedNetwork?.Name}
          </AppDialogTitle>
          <AppDialogContent>
            <div className="app-dialog-fields">
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
