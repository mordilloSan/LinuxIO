import { useMemo, useState } from "react";

import {
  linuxio,
  type NetworkInterface as BaseNI,
  useCallMutation,
} from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppButton from "@/components/ui/AppButton";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppGrid from "@/components/ui/AppGrid";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useAppTheme } from "@/theme";
import { CARD_PADDING_LG, GAP_MD, GAP_SM } from "@/theme/constants";

import {
  getNetworkStateColor,
  getNetworkStateSummary,
} from "./networkInterfaceState";

const NETWORK_TOAST_META = {
  label: "Open network",
  to: "/network",
} as const;

/* ================= helpers ================= */

const isIPv4 = (s: string) =>
  /^\s*(25[0-5]|2[0-4]\d|1?\d?\d)(\.(25[0-5]|2[0-4]\d|1?\d?\d)){3}\s*$/.test(s);
const toCIDR = (addr?: string, prefix?: number | string) => {
  const p = typeof prefix === "string" ? parseInt(prefix, 10) : prefix!;
  return addr && Number.isInteger(p) ? `${addr}/${p}` : "";
};
function getIPv4FromIface(i: any): string {
  if (typeof i?.ipv4_cidr === "string" && i.ipv4_cidr.includes("/")) {
    return i.ipv4_cidr;
  }
  if (
    i?.ipv4?.address &&
    (Number.isInteger(i?.ipv4?.prefix) || typeof i?.ipv4?.prefix === "string")
  ) {
    return toCIDR(i.ipv4.address, i.ipv4.prefix);
  }
  if (Array.isArray(i?.ipv4)) {
    const obj = i.ipv4.find(
      (x: any) =>
        x?.address &&
        (Number.isInteger(x?.prefix) || typeof x?.prefix === "string"),
    );
    if (obj) return toCIDR(obj.address, obj.prefix);
    const str = i.ipv4.find(
      (x: any) => typeof x === "string" && x.includes("/"),
    );
    if (str) return str;
  }
  if (typeof i?.ipv4?.cidr === "string") return i.ipv4.cidr;
  return "";
}
function getGatewayV4(i: any): string {
  const cand =
    i?.gateway_v4 ??
    i?.ipv4_gateway ??
    (typeof i?.gateway === "string" ? i.gateway : i?.gateway?.ipv4);
  return typeof cand === "string" && isIPv4(cand) ? cand.trim() : "";
}
function getDNSv4List(i: any): string[] {
  let list: any =
    i?.dns_v4 ??
    i?.nameservers_v4 ??
    i?.dns?.ipv4 ??
    i?.nameservers ??
    i?.dns ??
    [];
  if (typeof list === "string") list = list.split(/[,\s]+/);
  if (!Array.isArray(list)) return [];
  return list
    .map((item: any) => String(item))
    .map((s: string) => s.trim())
    .filter((s: string) => isIPv4(s));
}

/* ============================================ */

interface Props {
  expanded: boolean;
  iface: BaseNI;
  onClose: () => void;
}

type IPv4Mode = "auto" | "manual";

interface ManualIPv4Form {
  dns: string;
  gateway: string;
  ipv4: string;
}

interface EditorSession {
  draft: ManualIPv4Form | null;
  expanded: boolean;
  ifaceName: string;
  mode: IPv4Mode;
  sourceIpv4Method: string | undefined;
}

const modeFromInterface = (iface: BaseNI): IPv4Mode =>
  iface.ipv4_method === "manual" ? "manual" : "auto";

const createEditorSession = (
  iface: BaseNI,
  expanded: boolean,
): EditorSession => ({
  draft: null,
  expanded,
  ifaceName: iface.name,
  mode: modeFromInterface(iface),
  sourceIpv4Method: iface.ipv4_method,
});

const isCurrentSession = (
  session: EditorSession,
  iface: BaseNI,
  expanded: boolean,
) =>
  session.expanded === expanded &&
  session.ifaceName === iface.name &&
  session.sourceIpv4Method === iface.ipv4_method;

const NetworkInterfaceEditor = ({ iface, expanded, onClose }: Props) => {
  const theme = useAppTheme();
  const toast = useScopedToast(NETWORK_TOAST_META);

  // Compute sane defaults from iface — stabilised on the actual values,
  // NOT the iface object reference (which changes every refetch).
  const defaultIpv4 = getIPv4FromIface(iface);
  const defaultGateway = getGatewayV4(iface);
  const defaultDns = getDNSv4List(iface).join(", ");
  const defaults = useMemo<ManualIPv4Form>(
    () => ({
      ipv4: defaultIpv4,
      gateway: defaultGateway,
      dns: defaultDns,
    }),
    [defaultIpv4, defaultGateway, defaultDns],
  );

  const [storedSession, setStoredSession] = useState<EditorSession>(() =>
    createEditorSession(iface, expanded),
  );
  const sessionIsCurrent = isCurrentSession(storedSession, iface, expanded);
  const session = sessionIsCurrent
    ? storedSession
    : createEditorSession(iface, expanded);

  // Reset synchronously for a new open/close session, another interface, or a
  // backend method change. Polling updates to values still flow through
  // `defaults` until the user creates a draft.
  if (!sessionIsCurrent) {
    setStoredSession(session);
  }

  const { mode } = session;
  const editForm = session.draft ?? defaults;

  const updateSession = (update: (current: EditorSession) => EditorSession) => {
    setStoredSession((current) =>
      update(
        isCurrentSession(current, iface, expanded)
          ? current
          : createEditorSession(iface, expanded),
      ),
    );
  };

  // Mutations
  const { mutate: setIPv4, isPending: isSettingIPv4 } = useCallMutation(
    linuxio.network.set_ipv4,
    {
      success: () => {
        toast.success("Switched to DHCP mode");
        onClose();
      },
      error: "Failed to set DHCP configuration",
      toast: NETWORK_TOAST_META,
    },
  );
  const { mutate: setIPv4Manual, isPending: isSettingIPv4Manual } =
    useCallMutation(linuxio.network.set_ipv4_manual, {
      success: () => {
        toast.success("Manual configuration saved");
        onClose();
      },
      error: "Failed to save network configuration",
      toast: NETWORK_TOAST_META,
    });
  const { mutate: enableConnection, isPending: isEnabling } = useCallMutation(
    linuxio.network.enable_connection,
    {
      success: "Connection enabled",
      error: "Failed to enable connection",
      toast: NETWORK_TOAST_META,
    },
  );
  const { mutate: disableConnection, isPending: isDisabling } = useCallMutation(
    linuxio.network.disable_connection,
    {
      success: "Connection disabled",
      error: "Failed to disable connection",
      toast: NETWORK_TOAST_META,
    },
  );
  const saving = isSettingIPv4 || isSettingIPv4Manual;
  const toggling = isEnabling || isDisabling;
  const isConnected = iface.state === 100;
  const isConnecting = iface.state >= 40 && iface.state <= 90;
  const handleConnectionToggle = () => {
    if (toggling) return;
    if (isConnected || isConnecting) {
      disableConnection({ iface: iface.name });
    } else {
      enableConnection({ iface: iface.name });
    }
  };

  const handleModeChange = (newMode: IPv4Mode) => {
    updateSession((current) => ({
      ...current,
      draft: null,
      mode: newMode,
    }));
  };
  const handleChange = (field: keyof ManualIPv4Form, value: string) => {
    updateSession((current) => ({
      ...current,
      draft: {
        ...(current.draft ?? defaults),
        [field]: value,
      },
    }));
  };
  const handleDNSChange = (value: string) => {
    handleChange("dns", value);
  };
  const validateIPv4CIDR = (cidr: string): boolean => {
    if (!cidr.includes("/")) return false;
    const [ip, prefix] = cidr.split("/");
    const parts = ip.split(".");
    if (parts.length !== 4) return false;
    const prefixNum = parseInt(prefix, 10);
    if (!Number.isInteger(prefixNum) || prefixNum < 0 || prefixNum > 32)
      return false;
    return parts.every((part) => {
      const num = parseInt(part, 10);
      return Number.isInteger(num) && num >= 0 && num <= 255;
    });
  };
  const validateIPv4 = (ip: string): boolean => {
    const parts = ip.split(".");
    if (parts.length !== 4) return false;
    return parts.every((part) => {
      const num = parseInt(part, 10);
      return Number.isInteger(num) && num >= 0 && num <= 255;
    });
  };
  const handleSave = () => {
    if (mode === "auto") {
      // SetIPv4 with method "dhcp"
      setIPv4({ iface: iface.name, method: "dhcp" });
    } else {
      const ipv4 = editForm.ipv4.trim();
      const gateway = editForm.gateway.trim();
      const dnsInput = editForm.dns.trim();
      if (!ipv4) {
        toast.error("IP address is required");
        return;
      }
      if (!validateIPv4CIDR(ipv4)) {
        toast.error(
          "Invalid IPv4 address. Use CIDR format (e.g., 192.168.1.10/24)",
        );
        return;
      }
      if (!gateway) {
        toast.error("Gateway is required");
        return;
      }
      if (!validateIPv4(gateway)) {
        toast.error("Invalid gateway address");
        return;
      }
      if (!dnsInput) {
        toast.error("At least one DNS server is required");
        return;
      }
      const dnsServers: string[] = Array.from(
        new Set(
          dnsInput
            .split(/[,\s]+/)
            .map((s: string) => s.trim())
            .filter(Boolean),
        ),
      );
      if (dnsServers.length === 0) {
        toast.error("At least one DNS server is required");
        return;
      }
      for (const dns of dnsServers) {
        if (!validateIPv4(dns)) {
          toast.error(`Invalid DNS server: ${dns}`);
          return;
        }
      }

      setIPv4Manual({
        iface: iface.name,
        address: ipv4,
        gateway,
        dns: dnsServers.join(","),
      });
    }
  };
  if (!expanded) return null;

  // Link speed, driver and counters belong to the statistics card beside this
  // one; these are the three values this form owns.
  const interfaceDetails = [
    ["IPv4", iface.ipv4.join(", ") || "Not assigned"],
    ["Gateway", defaultGateway || "Not assigned"],
    ["DNS", defaultDns || "Not assigned"],
  ];
  // The switch carries no visible label any more, so this is both its
  // accessible name and the only place the transient states are spelled out.
  const connectionLabel = toggling
    ? "Toggling"
    : isConnected
      ? "Enabled"
      : isConnecting
        ? "Connecting"
        : "Disabled";
  // The applied method, which is not the same as the selected mode: it only
  // changes once a configuration is applied.
  const methodLabel =
    iface.ipv4_method === "manual"
      ? "Static IP"
      : iface.ipv4_method === "auto"
        ? "DHCP"
        : iface.ipv4_method === "disabled"
          ? "IPv4 disabled"
          : "IPv4 unknown";

  return (
    <AppGrid size={{ xs: 12, sm: 6, md: 4 }}>
      <FrostedCard
        accent
        style={{
          display: "flex",
          flexDirection: "column",
          gap: GAP_SM,
          height: "100%",
          padding: CARD_PADDING_LG,
        }}
      >
        <div>
          <div
            aria-busy={toggling || undefined}
            style={{
              alignItems: "center",
              display: "flex",
              gap: GAP_SM,
              justifyContent: "space-between",
            }}
          >
            <div
              style={{
                alignItems: "center",
                display: "flex",
                gap: GAP_SM,
                minWidth: 0,
              }}
            >
              <StatusDot
                color={getNetworkStateColor(iface.state, theme)}
                size={8}
                tooltip={getNetworkStateSummary(iface)}
              />
              <AppTypography fontWeight={600} noWrap variant="subtitle1">
                {iface.name}
              </AppTypography>
            </div>
            <div style={{ alignItems: "center", display: "flex", gap: GAP_SM }}>
              {toggling ? (
                <AppCircularProgress
                  aria-label={
                    isEnabling ? "Enabling connection" : "Disabling connection"
                  }
                  size={16}
                />
              ) : null}
              <AppSwitch
                aria-label={`Connection: ${connectionLabel}`}
                checked={isConnected || isConnecting}
                disabled={toggling}
                onChange={handleConnectionToggle}
                size="small"
              />
            </div>
          </div>
          <AppTypography color="text.secondary" noWrap variant="body2">
            MAC: {iface.mac}
          </AppTypography>
        </div>

        <div
          style={{
            alignItems: "center",
            display: "flex",
            gap: GAP_SM,
          }}
        >
          <AppButton
            onClick={() => handleModeChange("auto")}
            size="small"
            variant={mode === "auto" ? "contained" : "outlined"}
          >
            Automatic
          </AppButton>
          <AppButton
            onClick={() => handleModeChange("manual")}
            size="small"
            variant={mode === "manual" ? "contained" : "outlined"}
          >
            Manual
          </AppButton>
          <AppTypography
            color="text.secondary"
            noWrap
            style={{ marginLeft: "auto" }}
            variant="caption"
          >
            {methodLabel}
          </AppTypography>
        </div>
        {mode === "auto" ? (
          <div
            style={{
              display: "flex",
              flexDirection: "column",
              gap: GAP_MD,
            }}
          >
            <AppTypography color="text.secondary" variant="body2">
              The interface obtains its address, gateway, and DNS from DHCP.
            </AppTypography>
            {interfaceDetails.map(([label, value]) => (
              <div
                key={label}
                style={{
                  display: "flex",
                  gap: GAP_SM,
                  justifyContent: "space-between",
                }}
              >
                <AppTypography color="text.secondary" variant="body2">
                  {label}
                </AppTypography>
                <AppTypography noWrap variant="body2">
                  {value}
                </AppTypography>
              </div>
            ))}
          </div>
        ) : (
          <div
            style={{
              display: "flex",
              flexDirection: "column",
              gap: GAP_MD,
            }}
          >
            <AppTextField
              fullWidth
              label="IPv4 Address (CIDR)"
              onChange={(e) => handleChange("ipv4", e.target.value)}
              placeholder="192.168.1.10/24"
              required
              size="small"
              value={editForm.ipv4 ?? ""}
            />
            <AppTextField
              fullWidth
              label="Gateway"
              onChange={(e) => handleChange("gateway", e.target.value)}
              placeholder="192.168.1.1"
              required
              size="small"
              value={editForm.gateway ?? ""}
            />
            <AppTextField
              fullWidth
              label="DNS Servers"
              onChange={(e) => handleDNSChange(e.target.value)}
              placeholder="8.8.8.8, 8.8.4.4"
              required
              size="small"
              value={editForm.dns ?? ""}
            />
          </div>
        )}

        <div
          style={{
            display: "flex",
            justifyContent: "flex-end",
            gap: GAP_SM,
            marginTop: "auto",
          }}
        >
          <AppButton disabled={saving} onClick={onClose} size="small">
            Cancel
          </AppButton>
          <AppButton
            disabled={saving}
            onClick={handleSave}
            size="small"
            variant="contained"
          >
            {saving ? "Saving…" : "Apply Configuration"}
          </AppButton>
        </div>
      </FrostedCard>
    </AppGrid>
  );
};
export default NetworkInterfaceEditor;
