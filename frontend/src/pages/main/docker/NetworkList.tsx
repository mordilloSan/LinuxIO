import { Icon } from "@iconify/react";
import { useQueryClient } from "@tanstack/react-query";
import React, { useCallback, useEffect, useMemo, useState } from "react";

import { linuxio } from "@/api";
import NetworkCard from "@/components/cards/NetworkCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import UnifiedCollapsibleTable, {
  UnifiedTableColumn,
} from "@/components/tables/UnifiedCollapsibleTable";
import AppButton from "@/components/ui/AppButton";
import AppCheckbox from "@/components/ui/AppCheckbox";
import Chip from "@/components/ui/AppChip";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppGrid from "@/components/ui/AppGrid";
import AppSearchField from "@/components/ui/AppSearchField";
import AppSelect from "@/components/ui/AppSelect";
import AppSwitch from "@/components/ui/AppSwitch";
import {
  AppTable,
  AppTableBody,
  AppTableCell,
  AppTableHead,
  AppTableRow,
} from "@/components/ui/AppTable";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useAppTheme } from "@/theme";
import {
  longTextStyles,
  responsiveTextStyles,
  wrappableChipStyles,
} from "@/theme/tableStyles";
import { alpha } from "@/utils/color";
import { getMutationErrorMessage } from "@/utils/mutations";

interface NetworkListProps {
  onMountCreateHandler?: (handler: () => void) => void;
  viewMode?: "table" | "card";
}

interface CreateNetworkDialogProps {
  existingNames: string[];
  onClose: () => void;
  open: boolean;
}

const CreateNetworkDialog: React.FC<CreateNetworkDialogProps> = ({
  open,
  onClose,
  existingNames,
}) => {
  const queryClient = useQueryClient();
  const theme = useAppTheme();
  const toast = useScopedToast({ href: "/docker", label: "Open Docker" });
  const [networkName, setNetworkName] = useState("");
  const [driver, setDriver] = useState("bridge");
  const [internal, setInternal] = useState(false);

  const { mutate: createNetwork, isPending: isCreating } =
    linuxio.docker.create_network.useMutation({
      onSuccess: () => {
        toast.success(`Network "${networkName}" created successfully`);
        queryClient.invalidateQueries({
          queryKey: linuxio.docker.list_networks.queryKey(),
        });
        handleClose();
      },
      onError: (error: Error) => {
        toast.error(getMutationErrorMessage(error, "Failed to create network"));
      },
    });

  const nameTaken = networkName && existingNames.includes(networkName);
  const isValidName = /^[a-zA-Z0-9][a-zA-Z0-9_.-]*$/.test(networkName);

  const handleCreate = () => {
    if (!networkName || nameTaken || !isValidName) return;
    createNetwork({ name: networkName });
  };

  const handleClose = () => {
    setNetworkName("");
    setDriver("bridge");
    setInternal(false);
    onClose();
  };

  return (
    <GeneralDialog fullWidth maxWidth="xs" onClose={handleClose} open={open}>
      <AppDialogTitle>Create Network</AppDialogTitle>
      <AppDialogContent>
        <div style={{ marginTop: theme.spacing(2) }}>
          <AppTextField
            autoFocus
            disabled={isCreating}
            error={!!nameTaken || (networkName.length > 0 && !isValidName)}
            fullWidth
            helperText={
              nameTaken
                ? "This network name already exists."
                : networkName.length > 0 && !isValidName
                  ? "Name must start with alphanumeric and contain only alphanumeric, _, ., or -"
                  : ""
            }
            label="Network Name"
            onChange={(e) => setNetworkName(e.target.value)}
            value={networkName}
          />
          <AppSelect
            disabled={isCreating}
            fullWidth
            label="Driver"
            onChange={(e) => setDriver(e.target.value)}
            style={{ marginBlock: 8 }}
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
                onChange={(e) => setInternal(e.target.checked)}
              />
            }
            label="Internal network (no external connectivity)"
            style={{ marginTop: 4 }}
          />
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton
          color="secondary"
          disabled={isCreating}
          onClick={handleClose}
        >
          Cancel
        </AppButton>
        <AppButton
          disabled={!networkName || !!nameTaken || !isValidName || isCreating}
          onClick={handleCreate}
          variant="contained"
        >
          {isCreating ? "Creating..." : "Create"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

interface DeleteNetworkDialogProps {
  networkIds: string[];
  networkNames: string[];
  onClose: () => void;
  onSuccess: () => void;
  open: boolean;
}

const DeleteNetworkDialog: React.FC<DeleteNetworkDialogProps> = ({
  open,
  onClose,
  networkNames,
  networkIds,
  onSuccess,
}) => {
  const queryClient = useQueryClient();
  const theme = useAppTheme();
  const toast = useScopedToast({ href: "/docker", label: "Open Docker" });

  const { mutateAsync: deleteNetwork, isPending: isDeleting } =
    linuxio.docker.delete_network.useMutation({
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to delete network(s)"),
        );
      },
    });

  const handleDelete = async () => {
    // Delete networks sequentially
    for (const id of networkIds) {
      await deleteNetwork({ id });
    }
    const successMessage =
      networkNames.length === 1
        ? `Network "${networkNames[0]}" deleted successfully`
        : `${networkNames.length} networks deleted successfully`;
    toast.success(successMessage);
    queryClient.invalidateQueries({
      queryKey: linuxio.docker.list_networks.queryKey(),
    });
    onSuccess();
    handleClose();
  };

  const handleClose = () => {
    onClose();
  };

  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={handleClose} open={open}>
      <AppDialogTitle>
        Delete Network{networkNames.length > 1 ? "s" : ""}
      </AppDialogTitle>
      <AppDialogContent>
        <AppDialogContentText>
          Are you sure you want to delete the following network
          {networkNames.length > 1 ? "s" : ""}?
        </AppDialogContentText>
        <div
          style={{
            display: "flex",
            flexWrap: "wrap",
            marginTop: theme.spacing(2),
            marginBottom: theme.spacing(1),
          }}
        >
          {networkNames.map((name) => (
            <Chip
              key={name}
              label={name}
              size="small"
              style={{ marginRight: 4, marginBottom: 4 }}
              variant="soft"
            />
          ))}
        </div>
        <AppDialogContentText
          style={{ marginTop: 8, color: "var(--mui-palette-warning-main)" }}
        >
          This action cannot be undone. Networks with connected containers
          cannot be deleted.
        </AppDialogContentText>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isDeleting} onClick={handleClose}>
          Cancel
        </AppButton>
        <AppButton
          color="error"
          disabled={isDeleting}
          onClick={handleDelete}
          variant="contained"
        >
          {isDeleting ? "Deleting..." : "Delete"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

const NetworkList: React.FC<NetworkListProps> = ({
  onMountCreateHandler,
  viewMode = "table",
}) => {
  const theme = useAppTheme();
  const { data: rawNetworks } = linuxio.docker.list_networks.useQuery({
    refetchInterval: 10000,
  });
  const networks = rawNetworks ?? [];

  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);

  const filtered = networks.filter((net) =>
    net.Name.toLowerCase().includes(search.toLowerCase()),
  );

  // Compute effective selection - only include items that are in the filtered list
  const effectiveSelected = useMemo(() => {
    const filteredIds = new Set(filtered.map((n) => n.Id));
    const result = new Set<string>();
    selected.forEach((id) => {
      if (filteredIds.has(id)) {
        result.add(id);
      }
    });
    return result;
  }, [selected, filtered]);

  // Create network handler
  const handleCreateNetwork = useCallback(() => {
    setCreateDialogOpen(true);
  }, []);

  // Mount handler to parent
  useEffect(() => {
    if (onMountCreateHandler) {
      onMountCreateHandler(handleCreateNetwork);
    }
  }, [onMountCreateHandler, handleCreateNetwork]);

  const handleSelectAll = (checked: boolean) => {
    if (checked) {
      setSelected(new Set(filtered.map((n) => n.Id)));
    } else {
      setSelected(new Set());
    }
  };

  const handleSelectOne = (id: string, checked: boolean) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (checked) {
        next.add(id);
      } else {
        next.delete(id);
      }
      return next;
    });
  };

  const handleDeleteSuccess = () => {
    setSelected(new Set());
  };

  const selectedNetworks = filtered.filter((n) => effectiveSelected.has(n.Id));
  const allSelected =
    filtered.length > 0 && effectiveSelected.size === filtered.length;
  const someSelected =
    effectiveSelected.size > 0 && effectiveSelected.size < filtered.length;

  const columns: UnifiedTableColumn[] = [
    { field: "name", headerName: "Network Name", align: "left" },
    { field: "driver", headerName: "Driver", align: "left", width: "120px" },
    {
      field: "scope",
      headerName: "Scope",
      align: "left",
      width: "100px",
      className: "app-table-hide-below-md",
    },
    {
      field: "internal",
      headerName: "Internal",
      align: "left",
      width: "100px",
      className: "app-table-hide-below-md",
    },
    {
      field: "ipv4",
      headerName: "IPv4",
      align: "left",
      width: "100px",
      className: "app-table-hide-below-lg",
    },
    {
      field: "ipv6",
      headerName: "IPv6",
      align: "left",
      width: "100px",
      className: "app-table-hide-below-lg",
    },
    {
      field: "id",
      headerName: "Network ID",
      align: "left",
      width: "140px",
      className: "app-table-hide-below-md",
    },
  ];

  return (
    <div>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: theme.spacing(2),
          flexWrap: "wrap",
          marginBottom: theme.spacing(2),
        }}
      >
        <AppSearchField
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search networks…"
          style={{ width: 320 }}
          value={search}
        />
        <AppTypography fontWeight={700}>{filtered.length} shown</AppTypography>
        {effectiveSelected.size > 0 && (
          <AppButton
            color="error"
            onClick={() => setDeleteDialogOpen(true)}
            size="small"
            startIcon={<Icon height={20} icon="mdi:delete" width={20} />}
            variant="contained"
          >
            Delete ({effectiveSelected.size})
          </AppButton>
        )}
      </div>
      {viewMode === "card" ? (
        filtered.length > 0 ? (
          <AppGrid container spacing={2}>
            {filtered.map((network) => (
              <AppGrid key={network.Id} size={{ xs: 12, sm: 6, md: 4, lg: 3 }}>
                <NetworkCard
                  network={network}
                  onSelect={(checked) => handleSelectOne(network.Id, checked)}
                  selected={effectiveSelected.has(network.Id)}
                />
              </AppGrid>
            ))}
          </AppGrid>
        ) : (
          <div
            style={{
              textAlign: "center",
              paddingTop: theme.spacing(4),
              paddingBottom: theme.spacing(4),
            }}
          >
            <AppTypography color="text.secondary" variant="body2">
              No networks found.
            </AppTypography>
          </div>
        )
      ) : (
        <UnifiedCollapsibleTable
          columns={columns}
          data={filtered}
          emptyMessage="No networks found."
          getRowKey={(network) => network.Id}
          renderExpandedContent={(network) => (
            <>
              <AppTypography gutterBottom variant="subtitle2">
                <b>Full Network ID:</b>
              </AppTypography>
              <AppTypography
                style={{
                  fontFamily: "monospace",
                  fontSize: "0.85rem",
                  marginBottom: 8,
                  ...longTextStyles,
                }}
                variant="body2"
              >
                {network.Id}
              </AppTypography>

              <AppTypography gutterBottom variant="subtitle2">
                <b>Subnet(s):</b>
              </AppTypography>
              <div
                style={{
                  display: "flex",
                  flexWrap: "wrap",
                  marginBottom: theme.spacing(2),
                }}
              >
                {network.IPAM?.Config && network.IPAM.Config.length > 0 ? (
                  network.IPAM.Config.map((ipam, i) => (
                    <Chip
                      key={i}
                      label={`${ipam.Subnet} / Gateway: ${ipam.Gateway}`}
                      size="small"
                      sx={{ mr: 1, mb: 1, ...wrappableChipStyles }}
                      variant="soft"
                    />
                  ))
                ) : (
                  <AppTypography color="text.secondary" variant="body2">
                    (no IPAM config)
                  </AppTypography>
                )}
              </div>

              <AppTypography gutterBottom variant="subtitle2">
                <b>Options:</b>
              </AppTypography>
              <div
                style={{
                  display: "flex",
                  flexWrap: "wrap",
                  marginBottom: theme.spacing(2),
                }}
              >
                {network.Options && Object.keys(network.Options).length > 0 ? (
                  Object.entries(network.Options).map(([key, val]) => (
                    <Chip
                      key={key}
                      label={`${key}: ${val}`}
                      size="small"
                      sx={{ mr: 1, mb: 1, ...wrappableChipStyles }}
                      variant="soft"
                    />
                  ))
                ) : (
                  <AppTypography color="text.secondary" variant="body2">
                    (no options)
                  </AppTypography>
                )}
              </div>

              <AppTypography gutterBottom variant="subtitle2">
                <b>Labels:</b>
              </AppTypography>
              <div
                style={{
                  display: "flex",
                  flexWrap: "wrap",
                  marginBottom: theme.spacing(2),
                }}
              >
                {network.Labels && Object.keys(network.Labels).length > 0 ? (
                  Object.entries(network.Labels).map(([key, val]) => (
                    <Chip
                      key={key}
                      label={`${key}: ${val}`}
                      size="small"
                      sx={{ mr: 1, mb: 1, ...wrappableChipStyles }}
                      variant="soft"
                    />
                  ))
                ) : (
                  <AppTypography color="text.secondary" variant="body2">
                    (no labels)
                  </AppTypography>
                )}
              </div>

              <AppTypography gutterBottom variant="subtitle2">
                <b>Connected Containers:</b>
              </AppTypography>
              <div>
                {network.Containers &&
                Object.keys(network.Containers).length > 0 ? (
                  <AppTable
                    style={{
                      backgroundColor: alpha(
                        theme.palette.text.primary,
                        theme.palette.mode === "dark" ? 0.2 : 0.08,
                      ),
                      overflowX: "auto",
                      display: "block",
                    }}
                  >
                    <AppTableHead>
                      <AppTableRow>
                        <AppTableCell>
                          <b>Name</b>
                        </AppTableCell>
                        <AppTableCell>
                          <b>Container ID</b>
                        </AppTableCell>
                        <AppTableCell>
                          <b>IPv4</b>
                        </AppTableCell>
                        <AppTableCell>
                          <b>IPv6</b>
                        </AppTableCell>
                        <AppTableCell>
                          <b>MAC</b>
                        </AppTableCell>
                      </AppTableRow>
                    </AppTableHead>
                    <AppTableBody>
                      {Object.entries(network.Containers).map(
                        ([id, info]: [string, any]) => (
                          <AppTableRow key={id}>
                            <AppTableCell>
                              <AppTypography
                                style={responsiveTextStyles}
                                variant="body2"
                              >
                                {info.Name || "-"}
                              </AppTypography>
                            </AppTableCell>
                            <AppTableCell
                              style={{
                                fontFamily: "monospace",
                                fontSize: "0.85rem",
                                ...longTextStyles,
                              }}
                            >
                              {id.slice(0, 12)}
                            </AppTableCell>
                            <AppTableCell>
                              <AppTypography
                                style={{
                                  fontFamily: "monospace",
                                  fontSize: "0.85rem",
                                  ...longTextStyles,
                                }}
                                variant="body2"
                              >
                                {info.IPv4Address?.replace(/\/.*/, "") || "-"}
                              </AppTypography>
                            </AppTableCell>
                            <AppTableCell>
                              <AppTypography
                                style={{
                                  fontFamily: "monospace",
                                  fontSize: "0.85rem",
                                  ...longTextStyles,
                                }}
                                variant="body2"
                              >
                                {info.IPv6Address?.replace(/\/.*/, "") || "-"}
                              </AppTypography>
                            </AppTableCell>
                            <AppTableCell
                              style={{
                                fontFamily: "monospace",
                                fontSize: "0.85rem",
                                ...longTextStyles,
                              }}
                            >
                              {info.MacAddress || "-"}
                            </AppTableCell>
                          </AppTableRow>
                        ),
                      )}
                    </AppTableBody>
                  </AppTable>
                ) : (
                  <AppTypography color="text.secondary" variant="body2">
                    (no containers)
                  </AppTypography>
                )}
              </div>
            </>
          )}
          renderFirstCell={(network) => (
            <AppCheckbox
              checked={effectiveSelected.has(network.Id)}
              onChange={(e) => handleSelectOne(network.Id, e.target.checked)}
              onClick={(e) => e.stopPropagation()}
              size="small"
            />
          )}
          renderHeaderFirstCell={() => (
            <AppCheckbox
              checked={allSelected}
              indeterminate={someSelected}
              onChange={(e) => handleSelectAll(e.target.checked)}
              size="small"
            />
          )}
          renderMainRow={(network) => (
            <>
              <AppTableCell>
                <AppTypography
                  fontWeight={500}
                  style={responsiveTextStyles}
                  variant="body2"
                >
                  {network.Name}
                </AppTypography>
              </AppTableCell>
              <AppTableCell>
                <Chip
                  label={network.Driver}
                  size="small"
                  style={{ fontSize: "0.75rem" }}
                  variant="soft"
                />
              </AppTableCell>
              <AppTableCell className="app-table-hide-below-md">
                <AppTypography style={responsiveTextStyles} variant="body2">
                  {network.Scope}
                </AppTypography>
              </AppTableCell>
              <AppTableCell className="app-table-hide-below-md">
                <Chip
                  color={network.Internal ? "warning" : "default"}
                  label={network.Internal ? "Yes" : "No"}
                  size="small"
                  variant="soft"
                />
              </AppTableCell>
              <AppTableCell className="app-table-hide-below-lg">
                <Chip
                  color={network.EnableIPv4 !== false ? "success" : "default"}
                  label={network.EnableIPv4 !== false ? "Yes" : "No"}
                  size="small"
                  variant="soft"
                />
              </AppTableCell>
              <AppTableCell className="app-table-hide-below-lg">
                <Chip
                  color={network.EnableIPv6 ? "success" : "default"}
                  label={network.EnableIPv6 ? "Yes" : "No"}
                  size="small"
                  variant="soft"
                />
              </AppTableCell>
              <AppTableCell className="app-table-hide-below-md">
                <AppTypography
                  style={{
                    fontFamily: "monospace",
                    fontSize: "0.85rem",
                    ...responsiveTextStyles,
                  }}
                  variant="body2"
                >
                  {network.Id?.slice(0, 12)}
                </AppTypography>
              </AppTableCell>
            </>
          )}
        />
      )}

      <CreateNetworkDialog
        existingNames={networks.map((n) => n.Name)}
        onClose={() => setCreateDialogOpen(false)}
        open={createDialogOpen}
      />

      <DeleteNetworkDialog
        networkIds={selectedNetworks.map((n) => n.Id)}
        networkNames={selectedNetworks.map((n) => n.Name)}
        onClose={() => setDeleteDialogOpen(false)}
        onSuccess={handleDeleteSuccess}
        open={deleteDialogOpen}
      />
    </div>
  );
};

export default NetworkList;
