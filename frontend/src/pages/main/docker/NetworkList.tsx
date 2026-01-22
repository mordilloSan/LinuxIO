import DeleteIcon from "@mui/icons-material/Delete";
import {
  Box,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  TextField,
  Chip,
  Typography,
  Checkbox,
  Button,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  DialogContentText,
  Alert,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  FormControlLabel,
  Switch,
} from "@mui/material";
import React, { useCallback, useEffect, useState } from "react";

import linuxio from "@/api/react-query";
import UnifiedCollapsibleTable, {
  UnifiedTableColumn,
} from "@/components/tables/UnifiedCollapsibleTable";
import {
  responsiveTextStyles,
  longTextStyles,
  wrappableChipStyles,
} from "@/theme/tableStyles";

interface NetworkListProps {
  onMountCreateHandler?: (handler: () => void) => void;
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
  const [networkName, setNetworkName] = useState("");
  const [driver, setDriver] = useState("bridge");
  const [internal, setInternal] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const createNetworkMutation = linuxio.docker.create_network.useMutation();

  const nameTaken = networkName && existingNames.includes(networkName);
  const isValidName = /^[a-zA-Z0-9][a-zA-Z0-9_.-]*$/.test(networkName);

  const handleCreate = async () => {
    if (!networkName || nameTaken || !isValidName) return;

    setError(null);
    try {
      await createNetworkMutation.mutateAsync([networkName]);
      handleClose();
    } catch (err: any) {
      setError(err?.message || "Failed to create network");
    }
  };

  const handleClose = () => {
    setNetworkName("");
    setDriver("bridge");
    setInternal(false);
    setError(null);
    onClose();
  };

  return (
    <Dialog open={open} onClose={handleClose} fullWidth maxWidth="xs">
      <DialogTitle>Create Network</DialogTitle>
      <DialogContent>
        <Box mt={2}>
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
            disabled={createNetworkMutation.isPending}
            autoFocus
          />
          <FormControl fullWidth margin="normal">
            <InputLabel id="driver-select-label">Driver</InputLabel>
            <Select
              labelId="driver-select-label"
              value={driver}
              onChange={(e) => setDriver(e.target.value)}
              label="Driver"
              disabled={createNetworkMutation.isPending}
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
                disabled={createNetworkMutation.isPending}
              />
            }
            label="Internal network (no external connectivity)"
            sx={{ mt: 1 }}
          />
          {error && (
            <Alert severity="error" sx={{ mt: 2 }}>
              {error}
            </Alert>
          )}
        </Box>
      </DialogContent>
      <DialogActions>
        <Button
          onClick={handleClose}
          color="secondary"
          disabled={createNetworkMutation.isPending}
        >
          Cancel
        </Button>
        <Button
          onClick={handleCreate}
          variant="contained"
          disabled={
            !networkName ||
            !!nameTaken ||
            !isValidName ||
            createNetworkMutation.isPending
          }
        >
          {createNetworkMutation.isPending ? "Creating..." : "Create"}
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
  const [error, setError] = useState<string | null>(null);
  const [isDeleting, setIsDeleting] = useState(false);

  const deleteNetworkMutation = linuxio.docker.delete_network.useMutation();

  const handleDelete = async () => {
    setError(null);
    setIsDeleting(true);

    try {
      // Delete networks sequentially
      for (const id of networkIds) {
        await deleteNetworkMutation.mutateAsync([id]);
      }
      onSuccess();
      handleClose();
    } catch (err: any) {
      setError(err?.message || "Failed to delete network(s)");
    } finally {
      setIsDeleting(false);
    }
  };

  const handleClose = () => {
    setError(null);
    onClose();
  };

  return (
    <Dialog open={open} onClose={handleClose} maxWidth="sm" fullWidth>
      <DialogTitle>Delete Network{networkNames.length > 1 ? "s" : ""}</DialogTitle>
      <DialogContent>
        <DialogContentText>
          Are you sure you want to delete the following network
          {networkNames.length > 1 ? "s" : ""}?
        </DialogContentText>
        <Box sx={{ mt: 2, mb: 1 }}>
          {networkNames.map((name) => (
            <Chip
              key={name}
              label={name}
              size="small"
              sx={{ mr: 1, mb: 1 }}
            />
          ))}
        </Box>
        <DialogContentText sx={{ mt: 2, color: "warning.main" }}>
          This action cannot be undone. Networks with connected containers cannot
          be deleted.
        </DialogContentText>
        {error && (
          <Alert severity="error" sx={{ mt: 2 }}>
            {error}
          </Alert>
        )}
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

const NetworkList: React.FC<NetworkListProps> = ({ onMountCreateHandler }) => {
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

  // Clear selection when filtered list changes
  useEffect(() => {
    setSelected((prev) => {
      const filteredIds = new Set(filtered.map((n) => n.Id));
      const newSelected = new Set<string>();
      prev.forEach((id) => {
        if (filteredIds.has(id)) {
          newSelected.add(id);
        }
      });
      return newSelected;
    });
  }, [filtered.map((n) => n.Id).join(",")]);

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

  const selectedNetworks = filtered.filter((n) => selected.has(n.Id));
  const allSelected = filtered.length > 0 && selected.size === filtered.length;
  const someSelected = selected.size > 0 && selected.size < filtered.length;

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
    <Box>
      <Box mb={2} display="flex" alignItems="center" gap={2} flexWrap="wrap">
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
        <Box fontWeight="bold">{filtered.length} shown</Box>
        {selected.size > 0 && (
          <Button
            variant="contained"
            color="error"
            size="small"
            startIcon={<DeleteIcon />}
            onClick={() => setDeleteDialogOpen(true)}
          >
            Delete ({selected.size})
          </Button>
        )}
      </Box>
      <UnifiedCollapsibleTable
        data={filtered}
        columns={columns}
        getRowKey={(network) => network.Id}
        renderFirstCell={(network) => (
          <Checkbox
            size="small"
            checked={selected.has(network.Id)}
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
              <Typography
                variant="body2"
                fontWeight="medium"
                sx={responsiveTextStyles}
              >
                {network.Name}
              </Typography>
            </TableCell>
            <TableCell>
              <Chip
                label={network.Driver}
                size="small"
                sx={{ fontSize: "0.75rem" }}
              />
            </TableCell>
            <TableCell sx={{ display: { xs: "none", md: "table-cell" } }}>
              <Typography variant="body2" sx={responsiveTextStyles}>
                {network.Scope}
              </Typography>
            </TableCell>
            <TableCell sx={{ display: { xs: "none", md: "table-cell" } }}>
              <Chip
                label={network.Internal ? "Yes" : "No"}
                size="small"
                color={network.Internal ? "warning" : "default"}
              />
            </TableCell>
            <TableCell sx={{ display: { xs: "none", lg: "table-cell" } }}>
              <Chip
                label={network.EnableIPv4 !== false ? "Yes" : "No"}
                size="small"
                color={network.EnableIPv4 !== false ? "success" : "default"}
              />
            </TableCell>
            <TableCell sx={{ display: { xs: "none", lg: "table-cell" } }}>
              <Chip
                label={network.EnableIPv6 ? "Yes" : "No"}
                size="small"
                color={network.EnableIPv6 ? "success" : "default"}
              />
            </TableCell>
            <TableCell sx={{ display: { xs: "none", md: "table-cell" } }}>
              <Typography
                variant="body2"
                sx={{
                  fontFamily: "monospace",
                  fontSize: "0.85rem",
                  ...responsiveTextStyles,
                }}
              >
                {network.Id?.slice(0, 12)}
              </Typography>
            </TableCell>
          </>
        )}
        renderExpandedContent={(network) => (
          <>
            <Typography variant="subtitle2" gutterBottom>
              <b>Full Network ID:</b>
            </Typography>
            <Typography
              variant="body2"
              sx={{
                fontFamily: "monospace",
                fontSize: "0.85rem",
                mb: 2,
                ...longTextStyles,
              }}
            >
              {network.Id}
            </Typography>

            <Typography variant="subtitle2" gutterBottom>
              <b>Subnet(s):</b>
            </Typography>
            <Box sx={{ mb: 2, display: "flex", flexWrap: "wrap" }}>
              {network.IPAM?.Config && network.IPAM.Config.length > 0 ? (
                network.IPAM.Config.map((ipam, i) => (
                  <Chip
                    key={i}
                    label={`${ipam.Subnet} / Gateway: ${ipam.Gateway}`}
                    size="small"
                    sx={{ mr: 1, mb: 1, ...wrappableChipStyles }}
                  />
                ))
              ) : (
                <Typography variant="body2" color="text.secondary">
                  (no IPAM config)
                </Typography>
              )}
            </Box>

            <Typography variant="subtitle2" gutterBottom>
              <b>Options:</b>
            </Typography>
            <Box sx={{ mb: 2, display: "flex", flexWrap: "wrap" }}>
              {network.Options && Object.keys(network.Options).length > 0 ? (
                Object.entries(network.Options).map(([key, val]) => (
                  <Chip
                    key={key}
                    label={`${key}: ${val}`}
                    size="small"
                    sx={{ mr: 1, mb: 1, ...wrappableChipStyles }}
                  />
                ))
              ) : (
                <Typography variant="body2" color="text.secondary">
                  (no options)
                </Typography>
              )}
            </Box>

            <Typography variant="subtitle2" gutterBottom>
              <b>Labels:</b>
            </Typography>
            <Box sx={{ mb: 2, display: "flex", flexWrap: "wrap" }}>
              {network.Labels && Object.keys(network.Labels).length > 0 ? (
                Object.entries(network.Labels).map(([key, val]) => (
                  <Chip
                    key={key}
                    label={`${key}: ${val}`}
                    size="small"
                    sx={{ mr: 1, mb: 1, ...wrappableChipStyles }}
                  />
                ))
              ) : (
                <Typography variant="body2" color="text.secondary">
                  (no labels)
                </Typography>
              )}
            </Box>

            <Typography variant="subtitle2" gutterBottom>
              <b>Connected Containers:</b>
            </Typography>
            <Box>
              {network.Containers &&
              Object.keys(network.Containers).length > 0 ? (
                <Table
                  size="small"
                  sx={{
                    bgcolor: (theme) =>
                      theme.palette.mode === "dark"
                        ? "rgba(0,0,0,0.2)"
                        : "rgba(255,255,255,0.5)",
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
                            <Typography
                              variant="body2"
                              sx={responsiveTextStyles}
                            >
                              {info.Name || "-"}
                            </Typography>
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
                            <Typography
                              variant="body2"
                              sx={{
                                fontFamily: "monospace",
                                fontSize: "0.85rem",
                                ...longTextStyles,
                              }}
                            >
                              {info.IPv4Address?.replace(/\/.*/, "") || "-"}
                            </Typography>
                          </TableCell>
                          <TableCell>
                            <Typography
                              variant="body2"
                              sx={{
                                fontFamily: "monospace",
                                fontSize: "0.85rem",
                                ...longTextStyles,
                              }}
                            >
                              {info.IPv6Address?.replace(/\/.*/, "") || "-"}
                            </Typography>
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
                <Typography variant="body2" color="text.secondary">
                  (no containers)
                </Typography>
              )}
            </Box>
          </>
        )}
        emptyMessage="No networks found."
      />

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
    </Box>
  );
};

export default NetworkList;
