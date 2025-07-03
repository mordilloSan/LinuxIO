import React from "react";
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  TextField,
  Box,
  Alert,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
} from "@mui/material";

interface CreateInterfaceDialogProps {
  open: boolean;
  onClose: () => void;
  onCreate: () => void;
  loading: boolean;
  error?: string;
  serverName: string;
  setServerName: (name: string) => void;
  port: string | number;
  setPort: (port: number) => void;
  CIDR: string;
  setCIDR: (cidr: string) => void;
  peers: number;
  setPeers: (peers: number) => void;
  nic: string;
  setNic: (nic: string) => void;
  availableNICs: string[];
}

const CreateInterfaceDialog: React.FC<CreateInterfaceDialogProps> = ({
  open,
  onClose,
  onCreate,
  loading,
  error,
  serverName,
  setServerName,
  port,
  setPort,
  CIDR,
  setCIDR,
  peers,
  setPeers,
  nic,
  setNic,
  availableNICs,
}) => {
  return (
    <Dialog open={open} onClose={onClose} fullWidth maxWidth="sm">
      <DialogTitle>Create New Interface</DialogTitle>
      <DialogContent>
        <Box mt={2}>
          <TextField
            label="Interface Name"
            value={serverName}
            onChange={(e) => setServerName(e.target.value)}
            fullWidth
            margin="normal"
          />
          <TextField
            label="Port"
            type="number"
            value={port}
            onChange={(e) => setPort(Number(e.target.value))}
            fullWidth
            margin="normal"
          />
          <TextField
            label="CIDR"
            value={CIDR}
            onChange={(e) => setCIDR(e.target.value)}
            fullWidth
            margin="normal"
          />
          <TextField
            label="Peers"
            type="number"
            value={peers}
            onChange={(e) => setPeers(Number(e.target.value))}
            fullWidth
            margin="normal"
          />
          <FormControl fullWidth margin="normal">
            <InputLabel id="nic-select-label">NIC</InputLabel>
            <Select
              labelId="nic-select-label"
              value={nic}
              onChange={(e) => setNic(e.target.value as string)}
              label="NIC">
              {availableNICs.length === 0 ? (
                <MenuItem disabled>No NICs Available</MenuItem>
              ) : (
                availableNICs.map((nicOption) => (
                  <MenuItem key={nicOption} value={nicOption}>
                    {nicOption}
                  </MenuItem>
                ))
              )}
            </Select>
          </FormControl>
          {error && (
            <Alert severity="error" sx={{ mt: 2 }}>
              {error}
            </Alert>
          )}
        </Box>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} color="secondary">
          Cancel
        </Button>
        <Button
          onClick={onCreate}
          color="primary"
          disabled={!serverName || !port || loading}>
          {loading ? "Creating..." : "Create Interface"}
        </Button>
      </DialogActions>
    </Dialog>
  );
};

export default CreateInterfaceDialog;
