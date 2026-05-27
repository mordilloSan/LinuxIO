import React from "react";

import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppSelect from "@/components/ui/AppSelect";
import AppTextField from "@/components/ui/AppTextField";
import { useAppTheme } from "@/theme";

interface CreateInterfaceDialogProps {
  availableNICs: { name: string; label: string }[];
  CIDR: string;
  dns: string;
  error?: string;
  existingCIDRs: string[];
  existingNames: string[];
  existingPorts: number[];
  loading: boolean;
  nic: string;
  onClose: () => void;
  onCreate: () => void;
  open: boolean;
  peers: number;
  port: string | number;
  serverName: string;
  setCIDR: (cidr: string) => void;
  setDns: (dns: string) => void;
  setNic: (nic: string) => void;
  setPeers: (peers: number) => void;
  setPort: (port: number) => void;
  setServerName: (name: string) => void;
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
  const theme = useAppTheme();
  const nameTaken = serverName && existingNames.some((n) => n === serverName);
  const portTaken =
    port && existingPorts.some((p) => Number(port) === Number(p));
  const cidrTaken =
    CIDR &&
    existingCIDRs.some(
      (c) => (typeof c === "string" ? c : "").trim() === CIDR.trim(),
    );

  return (
    <GeneralDialog fullWidth maxWidth="xs" onClose={onClose} open={open}>
      <AppDialogTitle>Create New Interface</AppDialogTitle>
      <AppDialogContent>
        <div style={{ marginTop: theme.spacing(2) }}>
          <AppTextField
            disabled={loading}
            error={!!nameTaken}
            fullWidth
            helperText={nameTaken ? "This interface name already exists." : ""}
            label="Interface Name"
            onChange={(e) => setServerName(e.target.value)}
            value={serverName}
          />
          <AppTextField
            disabled={loading}
            error={!!portTaken}
            fullWidth
            helperText={portTaken ? "This port is already in use." : ""}
            label="Port"
            onChange={(e) => setPort(Number(e.target.value))}
            type="number"
            value={port}
          />
          <AppTextField
            disabled={loading}
            error={!!cidrTaken}
            fullWidth
            helperText={cidrTaken ? "This CIDR is already in use." : ""}
            label="CIDR"
            onChange={(e) => setCIDR(e.target.value)}
            value={CIDR}
          />
          <AppTextField
            disabled={loading}
            fullWidth
            label="DNS (optional, comma-separated)"
            onChange={(e) => setDns(e.target.value)}
            placeholder="e.g. 192.168.1.1, 1.1.1.1"
            value={dns}
          />
          <AppTextField
            disabled={loading}
            fullWidth
            label="Peers"
            onChange={(e) => setPeers(Number(e.target.value))}
            type="number"
            value={peers}
          />
          <AppSelect
            disabled={loading}
            fullWidth
            label="NIC"
            onChange={(e) => setNic(e.target.value)}
            style={{ marginBlock: 8 }}
            value={nic}
          >
            {availableNICs.length === 0 ? (
              <option disabled value="">
                No NICs Available
              </option>
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
        <AppButton color="secondary" disabled={loading} onClick={onClose}>
          Cancel
        </AppButton>
        <AppButton
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
          onClick={onCreate}
        >
          {loading ? "Creating..." : "Create Interface"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default CreateInterfaceDialog;
