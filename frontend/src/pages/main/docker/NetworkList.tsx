import { Icon } from "@iconify/react";
import {
  Grid,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  TextField,
  Checkbox,
  Button,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  DialogContentText,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  FormControlLabel,
  Switch,
  useTheme,
} from "@mui/material";
import { useQueryClient } from "@tanstack/react-query";
import React, { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";

import { linuxio } from "@/api";
import FrostedCard from "@/components/cards/RootCard";
import UnifiedCollapsibleTable, {
  UnifiedTableColumn,
} from "@/components/tables/UnifiedCollapsibleTable";
import Chip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";
import {
  responsiveTextStyles,
  longTextStyles,
  wrappableChipStyles,
} from "@/theme/tableStyles";
import { alpha } from "@/utils/color";
import { getMutationErrorMessage } from "@/utils/mutations";

interface NetworkListProps {
  onMountCreateHandler?: (handler: () => void) => void;
  viewMode?: "table" | "card";
}

interface CreateNetworkDialogProps {
  open: boolean;
  onClose: () => void;
  existingNames: string[];
}

const CreateNetworkDialog: React.FC<CreateNetworkDialogProps> = ({
  open,
  onClose,
  existingNames,
}) => {
  const queryClient = useQueryClient();
  const theme = useTheme();
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
    createNetwork([networkName]);
  };

  const handleClose = () => {
    setNetworkName("");
    setDriver("bridge");
    setInternal(false);
    onClose();
  };

  return (
    <Dialog open={open} onClose={handleClose} fullWidth maxWidth="xs">
      <DialogTitle>Create Network</DialogTitle>
      <DialogContent>
        <div style={{ marginTop: theme.spacing(2) }}>
          <TextField
            label="Network Name"
            value={networkName}
            onChange={(e) => setNetworkName(e.target.value)}
            fullWidth
            margin="normal"
            error={!!nameTaken || (networkName.length > 0 && !isValidName)}
            helperText={
              nameTaken
                ? "This network name already exists."
                : networkName.length > 0 && !isValidName
                  ? "Name must start with alphanumeric and contain only alphanumeric, _, ., or -"
                  : ""
            }
            disabled={isCreating}
            autoFocus
          />
          <FormControl fullWidth margin="normal">
            <InputLabel id="driver-select-label">Driver</InputLabel>
            <Select
              labelId="driver-select-label"
              value={driver}
              onChange={(e) => setDriver(e.target.value)}
              label="Driver"
              disabled={isCreating}
            >
              <MenuItem value="bridge">bridge</MenuItem>
              <MenuItem value="host">host</MenuItem>
              <MenuItem value="overlay">overlay</MenuItem>
              <MenuItem value="macvlan">macvlan</MenuItem>
              <MenuItem value="none">none</MenuItem>
            </Select>
          </FormControl>
          <FormControlLabel
            control={
              <Switch
                checked={internal}
                onChange={(e) => setInternal(e.target.checked)}
                disabled={isCreating}
              />
            }
            label="Internal network (no external connectivity)"
            sx={{ mt: 1 }}
          />
        </div>
      </DialogContent>
      <DialogActions>
        <Button onClick={handleClose} color="secondary" disabled={isCreating}>
          Cancel
        </Button>
        <Button
          onClick={handleCreate}
          variant="contained"
          disabled={!networkName || !!nameTaken || !isValidName || isCreating}
        >
          {isCreating ? "Creating..." : "Create"}
        </Button>
      </DialogActions>
    </Dialog>
  );
};

interface DeleteNetworkDialogProps {
  open: boolean;
  onClose: () => void;
  networkNames: string[];
  networkIds: string[];
  onSuccess: () => void;
}

const DeleteNetworkDialog: React.FC<DeleteNetworkDialogProps> = ({
  open,
  onClose,
  networkNames,
  networkIds,
  onSuccess,
}) => {
  const queryClient = useQueryClient();
  const theme = useTheme();

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
      await deleteNetwork([id]);
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
    <Dialog open={open} onClose={handleClose} maxWidth="sm" fullWidth>
      <DialogTitle>
        Delete Network{networkNames.length > 1 ? "s" : ""}
      </DialogTitle>
      <DialogContent>
        <DialogContentText>
          Are you sure you want to delete the following network
          {networkNames.length > 1 ? "s" : ""}?
        </DialogContentText>
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
              variant="soft"
              sx={{ mr: 1, mb: 1 }}
            />
          ))}
        </div>
        <DialogContentText sx={{ mt: 2, color: "warning.main" }}>
          This action cannot be undone. Networks with connected containers
          cannot be deleted.
        </DialogContentText>
      </DialogContent>
      <DialogActions>
        <Button onClick={handleClose} disabled={isDeleting}>
          Cancel
        </Button>
        <Button
          onClick={handleDelete}
          variant="contained"
          color="error"
          disabled={isDeleting}
        >
          {isDeleting ? "Deleting..." : "Delete"}
        </Button>
      </DialogActions>
    </Dialog>
  );
};

const NetworkList: React.FC<NetworkListProps> = ({
  onMountCreateHandler,
  viewMode = "table",
}) => {
  const theme = useTheme();
  const { data: networks = [] } = linuxio.docker.list_networks.useQuery({
    refetchInterval: 10000,
  });

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
      sx: { display: { xs: "none", md: "table-cell" } },
    },
    {
      field: "internal",
      headerName: "Internal",
      align: "left",
      width: "100px",
      sx: { display: { xs: "none", md: "table-cell" } },
    },
    {
      field: "ipv4",
      headerName: "IPv4",
      align: "left",
      width: "100px",
      sx: { display: { xs: "none", lg: "table-cell" } },
    },
    {
      field: "ipv6",
      headerName: "IPv6",
      align: "left",
      width: "100px",
      sx: { display: { xs: "none", lg: "table-cell" } },
    },
    {
      field: "id",
      headerName: "Network ID",
      align: "left",
      width: "140px",
      sx: { display: { xs: "none", md: "table-cell" } },
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
        <TextField
          variant="outlined"
          size="small"
          placeholder="Search networks…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          sx={{
            width: 320,
            "@media (max-width: 600px)": {
              width: "100%",
            },
          }}
        />
        <AppTypography fontWeight={700}>{filtered.length} shown</AppTypography>
        {effectiveSelected.size > 0 && (
          <Button
            variant="contained"
            color="error"
            size="small"
            startIcon={<Icon icon="mdi:delete" width={20} height={20} />}
            onClick={() => setDeleteDialogOpen(true)}
          >
            Delete ({effectiveSelected.size})
          </Button>
        )}
      </div>
      {viewMode === "card" ? (
        filtered.length > 0 ? (
          <Grid container spacing={2}>
            {filtered.map((network) => (
              <Grid key={network.Id} size={{ xs: 12, sm: 6, md: 4, lg: 3 }}>
                <FrostedCard style={{ padding: 8 }}>
                  <div
                    style={{
                      display: "flex",
                      alignItems: "center",
                      justifyContent: "space-between",
                      gap: theme.spacing(1),
                      marginBottom: theme.spacing(1),
                    }}
                  >
                    <div
                      style={{
                        display: "flex",
                        alignItems: "center",
                        gap: theme.spacing(1),
                      }}
                    >
                      <Checkbox
                        size="small"
                        checked={effectiveSelected.has(network.Id)}
                        onChange={(e) =>
                          handleSelectOne(network.Id, e.target.checked)
                        }
                      />
                      <AppTypography variant="body2" fontWeight={700} noWrap>
                        {network.Name}
                      </AppTypography>
                    </div>
                    <Chip
                      label={network.Driver}
                      size="small"
                      color="primary"
                      variant="soft"
                      sx={{ fontSize: "0.75rem" }}
                    />
                  </div>

                  <div
                    style={{
                      display: "flex",
                      flexWrap: "wrap",
                      gap: theme.spacing(0.75),
                    }}
                  >
                    <Chip
                      label={`Scope: ${network.Scope}`}
                      size="small"
                      variant="soft"
                    />
                    <Chip
                      label={`Internal: ${network.Internal ? "Yes" : "No"}`}
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

                  <AppTypography
                    variant="body2"
                    style={{
                      marginTop: 4,
                      marginBottom: 4,
                      fontFamily: "monospace",
                      fontSize: "0.78rem",
                      ...longTextStyles,
                    }}
                  >
                    ID: {network.Id}
                  </AppTypography>

                  <div
                    style={{
                      display: "flex",
                      flexWrap: "wrap",
                      gap: theme.spacing(0.75),
                    }}
                  >
                    {network.IPAM?.Config && network.IPAM.Config.length > 0 ? (
                      network.IPAM.Config.slice(0, 2).map((ipam, i) => (
                        <Chip
                          key={`${network.Id}-ipam-${i}`}
                          label={ipam.Subnet}
                          size="small"
                          variant="outlined"
                          sx={wrappableChipStyles}
                        />
                      ))
                    ) : (
                      <AppTypography variant="caption" color="text.secondary">
                        No IPAM config
                      </AppTypography>
                    )}
                  </div>
                </FrostedCard>
              </Grid>
            ))}
          </Grid>
        ) : (
          <div
            style={{
              textAlign: "center",
              paddingTop: theme.spacing(4),
              paddingBottom: theme.spacing(4),
            }}
          >
            <AppTypography variant="body2" color="text.secondary">
              No networks found.
            </AppTypography>
          </div>
        )
      ) : (
        <UnifiedCollapsibleTable
          data={filtered}
          columns={columns}
          getRowKey={(network) => network.Id}
          renderFirstCell={(network) => (
            <Checkbox
              size="small"
              checked={effectiveSelected.has(network.Id)}
              onChange={(e) => handleSelectOne(network.Id, e.target.checked)}
              onClick={(e) => e.stopPropagation()}
            />
          )}
          renderHeaderFirstCell={() => (
            <Checkbox
              size="small"
              checked={allSelected}
              indeterminate={someSelected}
              onChange={(e) => handleSelectAll(e.target.checked)}
            />
          )}
          renderMainRow={(network) => (
            <>
              <TableCell>
                <AppTypography
                  variant="body2"
                  fontWeight={500}
                  style={responsiveTextStyles}
                >
                  {network.Name}
                </AppTypography>
              </TableCell>
              <TableCell>
                <Chip
                  label={network.Driver}
                  size="small"
                  variant="soft"
                  sx={{ fontSize: "0.75rem" }}
                />
              </TableCell>
              <TableCell sx={{ display: { xs: "none", md: "table-cell" } }}>
                <AppTypography variant="body2" style={responsiveTextStyles}>
                  {network.Scope}
                </AppTypography>
              </TableCell>
              <TableCell sx={{ display: { xs: "none", md: "table-cell" } }}>
                <Chip
                  label={network.Internal ? "Yes" : "No"}
                  size="small"
                  variant="soft"
                  color={network.Internal ? "warning" : "default"}
                />
              </TableCell>
              <TableCell sx={{ display: { xs: "none", lg: "table-cell" } }}>
                <Chip
                  label={network.EnableIPv4 !== false ? "Yes" : "No"}
                  size="small"
                  variant="soft"
                  color={network.EnableIPv4 !== false ? "success" : "default"}
                />
              </TableCell>
              <TableCell sx={{ display: { xs: "none", lg: "table-cell" } }}>
                <Chip
                  label={network.EnableIPv6 ? "Yes" : "No"}
                  size="small"
                  variant="soft"
                  color={network.EnableIPv6 ? "success" : "default"}
                />
              </TableCell>
              <TableCell sx={{ display: { xs: "none", md: "table-cell" } }}>
                <AppTypography
                  variant="body2"
                  style={{
                    fontFamily: "monospace",
                    fontSize: "0.85rem",
                    ...responsiveTextStyles,
                  }}
                >
                  {network.Id?.slice(0, 12)}
                </AppTypography>
              </TableCell>
            </>
          )}
          renderExpandedContent={(network) => (
            <>
              <AppTypography variant="subtitle2" gutterBottom>
                <b>Full Network ID:</b>
              </AppTypography>
              <AppTypography
                variant="body2"
                style={{
                  fontFamily: "monospace",
                  fontSize: "0.85rem",
                  marginBottom: 8,
                  ...longTextStyles,
                }}
              >
                {network.Id}
              </AppTypography>

              <AppTypography variant="subtitle2" gutterBottom>
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
                      variant="soft"
                      sx={{ mr: 1, mb: 1, ...wrappableChipStyles }}
                    />
                  ))
                ) : (
                  <AppTypography variant="body2" color="text.secondary">
                    (no IPAM config)
                  </AppTypography>
                )}
              </div>

              <AppTypography variant="subtitle2" gutterBottom>
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
                      variant="soft"
                      sx={{ mr: 1, mb: 1, ...wrappableChipStyles }}
                    />
                  ))
                ) : (
                  <AppTypography variant="body2" color="text.secondary">
                    (no options)
                  </AppTypography>
                )}
              </div>

              <AppTypography variant="subtitle2" gutterBottom>
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
                      variant="soft"
                      sx={{ mr: 1, mb: 1, ...wrappableChipStyles }}
                    />
                  ))
                ) : (
                  <AppTypography variant="body2" color="text.secondary">
                    (no labels)
                  </AppTypography>
                )}
              </div>

              <AppTypography variant="subtitle2" gutterBottom>
                <b>Connected Containers:</b>
              </AppTypography>
              <div>
                {network.Containers &&
                Object.keys(network.Containers).length > 0 ? (
                  <Table
                    size="small"
                    sx={{
                      bgcolor: (theme) =>
                        alpha(
                          theme.palette.text.primary,
                          theme.palette.mode === "dark" ? 0.2 : 0.08,
                        ),
                      overflowX: "auto",
                      display: "block",
                    }}
                  >
                    <TableHead>
                      <TableRow>
                        <TableCell>
                          <b>Name</b>
                        </TableCell>
                        <TableCell>
                          <b>Container ID</b>
                        </TableCell>
                        <TableCell>
                          <b>IPv4</b>
                        </TableCell>
                        <TableCell>
                          <b>IPv6</b>
                        </TableCell>
                        <TableCell>
                          <b>MAC</b>
                        </TableCell>
                      </TableRow>
                    </TableHead>
                    <TableBody>
                      {Object.entries(network.Containers).map(
                        ([id, info]: [string, any]) => (
                          <TableRow key={id}>
                            <TableCell>
                              <AppTypography
                                variant="body2"
                                style={responsiveTextStyles}
                              >
                                {info.Name || "-"}
                              </AppTypography>
                            </TableCell>
                            <TableCell
                              sx={{
                                fontFamily: "monospace",
                                fontSize: "0.85rem",
                                ...longTextStyles,
                              }}
                            >
                              {id.slice(0, 12)}
                            </TableCell>
                            <TableCell>
                              <AppTypography
                                variant="body2"
                                style={{
                                  fontFamily: "monospace",
                                  fontSize: "0.85rem",
                                  ...longTextStyles,
                                }}
                              >
                                {info.IPv4Address?.replace(/\/.*/, "") || "-"}
                              </AppTypography>
                            </TableCell>
                            <TableCell>
                              <AppTypography
                                variant="body2"
                                style={{
                                  fontFamily: "monospace",
                                  fontSize: "0.85rem",
                                  ...longTextStyles,
                                }}
                              >
                                {info.IPv6Address?.replace(/\/.*/, "") || "-"}
                              </AppTypography>
                            </TableCell>
                            <TableCell
                              sx={{
                                fontFamily: "monospace",
                                fontSize: "0.85rem",
                                ...longTextStyles,
                              }}
                            >
                              {info.MacAddress || "-"}
                            </TableCell>
                          </TableRow>
                        ),
                      )}
                    </TableBody>
                  </Table>
                ) : (
                  <AppTypography variant="body2" color="text.secondary">
                    (no containers)
                  </AppTypography>
                )}
              </div>
            </>
          )}
          emptyMessage="No networks found."
        />
      )}

      <CreateNetworkDialog
        open={createDialogOpen}
        onClose={() => setCreateDialogOpen(false)}
        existingNames={networks.map((n) => n.Name)}
      />

      <DeleteNetworkDialog
        open={deleteDialogOpen}
        onClose={() => setDeleteDialogOpen(false)}
        networkNames={selectedNetworks.map((n) => n.Name)}
        networkIds={selectedNetworks.map((n) => n.Id)}
        onSuccess={handleDeleteSuccess}
      />
    </div>
  );
};

export default NetworkList;
