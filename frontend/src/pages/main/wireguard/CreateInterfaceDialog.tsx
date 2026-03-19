import AppSelect from "@/components/ui/AppSelect";
import { useTheme } from "@mui/material/styles";
import React from "react";

import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppTextField from "@/components/ui/AppTextField";

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
          <AppTextField
            label="Interface Name"
            value={serverName}
            onChange={(e) => setServerName(e.target.value)}
            fullWidth
            error={!!nameTaken}
            helperText={nameTaken ? "This interface name already exists." : ""}
            disabled={loading}
          />
          <AppTextField
            label="Port"
            type="number"
            value={port}
            onChange={(e) => setPort(Number(e.target.value))}
            fullWidth
            error={!!portTaken}
            helperText={portTaken ? "This port is already in use." : ""}
            disabled={loading}
          />
          <AppTextField
            label="CIDR"
            value={CIDR}
            onChange={(e) => setCIDR(e.target.value)}
            fullWidth
            error={!!cidrTaken}
            helperText={cidrTaken ? "This CIDR is already in use." : ""}
            disabled={loading}
          />
          <AppTextField
            label="DNS (optional, comma-separated)"
            value={dns}
            onChange={(e) => setDns(e.target.value)}
            fullWidth
            disabled={loading}
            placeholder="e.g. 192.168.1.1, 1.1.1.1"
          />
          <AppTextField
            label="Peers"
            type="number"
            value={peers}
            onChange={(e) => setPeers(Number(e.target.value))}
            fullWidth
            disabled={loading}
          />
          <AppSelect
            label="NIC"
            fullWidth
            value={nic}
            onChange={(e) => setNic(e.target.value)}
            disabled={loading}
            style={{ marginBlock: 8 }}
          >
            {availableNICs.length === 0 ? (
              <option value="" disabled>No NICs Available</option>
            ) : (
              availableNICs.map((nicOption) => (
                <option key={nicOption.name} value={nicOption.name}>
                  {nicOption.label}
                </option>
              ))
            )}
          </AppSelect>
          {error && (
            <AppAlert severity="error" style={{ marginTop: 8 }}>
              {error}
            </AppAlert>
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
