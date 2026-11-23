import { Icon } from "@iconify/react";
import { useId } from "react";

import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppCollapse from "@/components/ui/AppCollapse";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppSelect from "@/components/ui/AppSelect";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";

interface CreateInterfaceDialogProps {
  advancedOpen: boolean;
  availableNICs: { name: string; label: string }[];
  CIDR: string;
  dns: string;
  error?: string;
  existingCIDRs: string[];
  existingNames: string[];
  existingPorts: number[];
  host: string;
  loading: boolean;
  mtu: string;
  nic: string;
  onClose: () => void;
  onCreate: () => void;
  open: boolean;
  optionsLoading: boolean;
  peers: number;
  port: string | number;
  serverName: string;
  setAdvancedOpen: (open: boolean) => void;
  setCIDR: (cidr: string) => void;
  setDns: (dns: string) => void;
  setHost: (host: string) => void;
  setMtu: (mtu: string) => void;
  setNic: (nic: string) => void;
  setPeers: (peers: number) => void;
  setPort: (port: number) => void;
  setServerName: (name: string) => void;
}

export const isMtuValid = (value: string): boolean => {
  const trimmed = value.trim();
  if (trimmed === "") return true;
  if (!/^\d+$/.test(trimmed)) return false;
  const mtu = Number(trimmed);
  return mtu === 0 || (mtu >= 1280 && mtu <= 65535);
};

/** The DNS servers as the request sends them: trimmed, without blanks. */
export const dnsServers = (value: string): string[] =>
  value
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);

/**
 * One line naming what Advanced holds, so the choices made for the user stay
 * in view while the section is closed. The NIC is the one the request sends.
 */
export const advancedSummary = ({
  CIDR,
  dns,
  host,
  mtu,
  nic,
  port,
}: {
  CIDR: string;
  dns: string;
  host: string;
  mtu: string;
  nic: string;
  port: string | number;
}): string => {
  const servers = dnsServers(dns);
  const trimmedMtu = mtu.trim();
  return [
    Number(port) ? `Port ${port}` : null,
    CIDR.trim() || null,
    nic ? `through ${nic}` : null,
    servers.length > 0 ? `DNS ${servers.join(", ")}` : null,
    host.trim() ? `endpoint ${host.trim()}` : null,
    trimmedMtu && Number(trimmedMtu) !== 0 ? `MTU ${trimmedMtu}` : null,
  ]
    .filter(Boolean)
    .join(" · ");
};

const CreateInterfaceDialog = ({
  advancedOpen,
  setAdvancedOpen,
  open,
  onClose,
  onCreate,
  loading,
  optionsLoading,
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
  host,
  setHost,
  mtu,
  setMtu,
}: CreateInterfaceDialogProps) => {
  const nameTaken = serverName && existingNames.some((n) => n === serverName);
  const mtuInvalid = !isMtuValid(mtu);
  const portTaken = port && existingPorts.some((p) => Number(port) === p);
  const cidrTaken =
    CIDR &&
    existingCIDRs.some(
      (c) => (typeof c === "string" ? c : "").trim() === CIDR.trim(),
    );
  const noNIC = !optionsLoading && !nic;
  // A field of Advanced that stops the create is never hidden: the section
  // shows while one is, whatever the user chose. A click on Advanced then
  // keeps it open, so fixing the field does not fold it away under the user.
  const advancedInvalid =
    Number(port) === 0 ||
    !!portTaken ||
    !CIDR ||
    !!cidrTaken ||
    mtuInvalid ||
    noNIC;
  const advancedShown = advancedOpen || advancedInvalid;
  const advancedId = useId();

  return (
    <GeneralDialog fullWidth maxWidth="xs" onClose={onClose} open={open}>
      <AppDialogTitle>Create New Interface</AppDialogTitle>
      <AppDialogContent>
        <div className="app-dialog-fields">
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
            fullWidth
            helperText="One configuration is made for each. You can add more later."
            label="Devices"
            onChange={(e) => setPeers(Number(e.target.value))}
            type="number"
            value={peers}
          />
          <AppTypography color="text.secondary" variant="body2">
            {advancedSummary({ CIDR, dns, host, mtu, nic, port })}
          </AppTypography>
          <AppButton
            aria-controls={advancedId}
            aria-expanded={advancedShown}
            color="inherit"
            keepTextOnMobile
            onClick={() => setAdvancedOpen(advancedInvalid || !advancedShown)}
            size="small"
            startIcon={
              <Icon
                aria-hidden="true"
                height={18}
                icon={advancedShown ? "mdi:chevron-up" : "mdi:chevron-down"}
                width={18}
              />
            }
            style={{ alignSelf: "flex-start" }}
          >
            Advanced
          </AppButton>
          <div id={advancedId}>
            <AppCollapse in={advancedShown} unmountOnExit>
              <div className="app-dialog-fields">
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
                <AppSelect
                  disabled={loading}
                  fullWidth
                  label="NIC"
                  onChange={(e) => setNic(e.target.value)}
                  value={nic}
                >
                  {optionsLoading ? (
                    <option disabled value="">
                      Loading NICs…
                    </option>
                  ) : availableNICs.length === 0 ? (
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
                <AppTextField
                  disabled={loading}
                  fullWidth
                  helperText="Separate multiple addresses with commas."
                  label="DNS (optional)"
                  onChange={(e) => setDns(e.target.value)}
                  placeholder="e.g. 192.168.1.1, 1.1.1.1"
                  value={dns}
                />
                <AppTextField
                  disabled={loading}
                  fullWidth
                  helperText="Leave empty to use the detected public IP."
                  label="Endpoint host (optional)"
                  onChange={(e) => setHost(e.target.value)}
                  placeholder="e.g. vpn.example.org"
                  value={host}
                />
                <AppTextField
                  disabled={loading}
                  error={mtuInvalid}
                  fullWidth
                  helperText={
                    mtuInvalid ? "Use 0 or 1280 to 65535" : "Server-side MTU."
                  }
                  label="MTU (optional)"
                  onChange={(e) => setMtu(e.target.value)}
                  value={mtu}
                />
              </div>
            </AppCollapse>
          </div>
          {error && <AppAlert severity="error">{error}</AppAlert>}
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={loading} onClick={onClose}>
          Cancel
        </AppButton>
        <AppButton
          disabled={
            !serverName ||
            Number(port) === 0 ||
            !CIDR ||
            !nic ||
            loading ||
            !!nameTaken ||
            !!portTaken ||
            !!cidrTaken ||
            mtuInvalid ||
            optionsLoading
          }
          onClick={onCreate}
          variant="contained"
        >
          {loading ? "Creating..." : "Create Interface"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default CreateInterfaceDialog;
