import {
  Alert,
  FormControl,
  FormHelperText,
  InputLabel,
  MenuItem,
  Select,
  TextField,
} from "@mui/material";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import { useTheme } from "@mui/material/styles";
import React from "react";

import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppButton from "@/components/ui/AppButton";

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
  availableNICs: { name: string; label: string }[];
  existingNames: string[];
  existingPorts: number[];
  existingCIDRs: string[];
  dns: string;
  setDns: (dns: string) => void;
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
  existingNames,
  existingPorts,
  existingCIDRs,
  dns,
  setDns,
}) => {
  const theme = useTheme();
  const nameTaken = serverName && existingNames.some((n) => n === serverName);
  const portTaken =
    port && existingPorts.some((p) => Number(port) === Number(p));
  const cidrTaken =
    CIDR &&
    existingCIDRs.some(
      (c) => (typeof c === "string" ? c : "").trim() === CIDR.trim(),
    );

  return (
    <GeneralDialog open={open} onClose={onClose} fullWidth maxWidth="xs">
      <AppDialogTitle>Create New Interface</AppDialogTitle>
      <AppDialogContent>
        <div style={{ marginTop: theme.spacing(2) }}>
          <TextField
            label="Interface Name"
            value={serverName}
            onChange={(e) => setServerName(e.target.value)}
            fullWidth
            margin="normal"
            error={!!nameTaken}
            helperText={nameTaken ? "This interface name already exists." : ""}
            disabled={loading}
          />
          <TextField
            label="Port"
            type="number"
            value={port}
            onChange={(e) => setPort(Number(e.target.value))}
            fullWidth
            margin="normal"
            error={!!portTaken}
            helperText={portTaken ? "This port is already in use." : ""}
            disabled={loading}
          />
          <TextField
            label="CIDR"
            value={CIDR}
            onChange={(e) => setCIDR(e.target.value)}
            fullWidth
            margin="normal"
            error={!!cidrTaken}
            helperText={cidrTaken ? "This CIDR is already in use." : ""}
            disabled={loading}
          />
          <TextField
            label="DNS (optional, comma-separated)"
            value={dns}
            onChange={(e) => setDns(e.target.value)}
            fullWidth
            margin="normal"
            disabled={loading}
            placeholder="e.g. 192.168.1.1, 1.1.1.1"
          />
          <TextField
            label="Peers"
            type="number"
            value={peers}
            onChange={(e) => setPeers(Number(e.target.value))}
            fullWidth
            margin="normal"
            disabled={loading}
          />
          <FormControl fullWidth margin="normal" disabled={loading}>
            <InputLabel id="nic-select-label">NIC</InputLabel>
            <Select
              labelId="nic-select-label"
              value={nic}
              onChange={(e) => setNic(e.target.value as string)}
              label="NIC"
            >
              {availableNICs.length === 0 ? (
                <MenuItem disabled>No NICs Available</MenuItem>
              ) : (
                availableNICs.map((nicOption) => (
                  <MenuItem key={nicOption.name} value={nicOption.name}>
                    {nicOption.label}
                  </MenuItem>
                ))
              )}
            </Select>
            <FormHelperText>
              {availableNICs.length === 0 ? "No NICs available" : ""}
            </FormHelperText>
          </FormControl>
          {error && (
            <Alert severity="error" sx={{ mt: 2 }}>
              {error}
            </Alert>
          )}
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={onClose} color="secondary" disabled={loading}>
          Cancel
        </AppButton>
        <AppButton
          onClick={onCreate}
          color="primary"
          disabled={
            !serverName ||
            Number(port) === 0 ||
            !CIDR ||
            loading ||
            !!nameTaken ||
            !!portTaken ||
            !!cidrTaken
          }
        >
          {loading ? "Creating..." : "Create Interface"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default CreateInterfaceDialog;
