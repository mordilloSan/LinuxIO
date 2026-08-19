import { useMemo, useState } from "react";

import {
  linuxio,
  type NetworkInterface as BaseNI,
  useCallMutation,
} from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppButton from "@/components/ui/AppButton";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppGrid from "@/components/ui/AppGrid";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import { useScopedToast } from "@/hooks/useScopedToast";
import { CARD_PADDING_LG, GAP_MD, GAP_SM } from "@/theme/constants";

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

  const interfaceDetails = [
    ["IPv4", iface.ipv4.join(", ") || "Not assigned"],
    ["Gateway", defaultGateway || "Not assigned"],
    ["DNS", defaultDns || "Not assigned"],
    [
      "Link",
      iface.speed === "unknown" || iface.speed.startsWith("-1")
        ? "No carrier"
        : `${iface.speed}${iface.duplex !== "unknown" ? ` (${iface.duplex})` : ""}`,
    ],
  ];

  return (
    <>
      <AppGrid size={{ xs: 12, md: 6 }}>
        <FrostedCard
          accent
          style={{
            padding: CARD_PADDING_LG,
            height: "100%",
          }}
        >
          <AppTypography fontWeight={600} variant="subtitle1">
            {iface.name}
          </AppTypography>
          <AppTypography color="text.secondary" variant="body2">
            MAC: {iface.mac}
          </AppTypography>

          <div
            style={{
              display: "flex",
              alignItems: "center",
              justifyContent: "space-between",
              gap: GAP_MD,
              marginTop: GAP_MD,
            }}
          >
            <div aria-busy={toggling || undefined}>
              <AppFormControlLabel
                control={
                  <AppSwitch
                    checked={isConnected || isConnecting}
                    disabled={toggling}
                    onChange={handleConnectionToggle}
                  />
                }
                label={
                  toggling
                    ? "Toggling..."
                    : isConnected
                      ? "Enabled"
                      : isConnecting
                        ? "Connecting..."
                        : "Disabled"
                }
              />
              {toggling ? (
                <AppCircularProgress
                  aria-label={
                    isEnabling ? "Enabling connection" : "Disabling connection"
                  }
                  size={16}
                  style={{
                    marginLeft: GAP_SM,
                    verticalAlign: "middle",
                  }}
                />
              ) : null}
            </div>
            <AppTypography color="text.secondary" variant="body2">
              {iface.ipv4_method === "manual"
                ? "Static IP"
                : iface.ipv4_method === "auto"
                  ? "DHCP"
                  : iface.ipv4_method === "disabled"
                    ? "IPv4 disabled"
                    : "IPv4 unknown"}
            </AppTypography>
          </div>

          <div style={{ display: "flex", gap: GAP_SM, marginTop: GAP_MD }}>
            <AppButton
              fullWidth
              onClick={() => handleModeChange("auto")}
              variant={mode === "auto" ? "contained" : "outlined"}
            >
              Automatic
            </AppButton>
            <AppButton
              fullWidth
              onClick={() => handleModeChange("manual")}
              variant={mode === "manual" ? "contained" : "outlined"}
            >
              Manual
            </AppButton>
          </div>
        </FrostedCard>
      </AppGrid>

      <AppGrid size={{ xs: 12, md: 6 }}>
        <FrostedCard
          style={{
            padding: CARD_PADDING_LG,
            height: "100%",
          }}
        >
          <AppTypography fontWeight={600} variant="subtitle1">
            {mode === "auto"
              ? "Automatic configuration"
              : "Manual configuration"}
          </AppTypography>

          {mode === "auto" ? (
            <div
              style={{
                display: "flex",
                flexDirection: "column",
                gap: GAP_SM,
                marginTop: GAP_MD,
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
            <div style={{ marginTop: GAP_MD }}>
              <AppTypography
                color="text.secondary"
                style={{ marginBottom: GAP_SM }}
                variant="body2"
              >
                Configure static network settings. All fields are required.
              </AppTypography>

              <AppTextField
                fullWidth
                helperText="Format: IP/prefix (e.g., 192.168.1.10/24)"
                label="IPv4 Address (CIDR)"
                onChange={(e) => handleChange("ipv4", e.target.value)}
                placeholder="192.168.1.10/24"
                required
                size="small"
                style={{ marginBottom: GAP_SM }}
                value={editForm.ipv4 ?? ""}
              />

              <AppTextField
                fullWidth
                helperText="The IP address of your network gateway/router"
                label="Gateway"
                onChange={(e) => handleChange("gateway", e.target.value)}
                placeholder="192.168.1.1"
                required
                size="small"
                style={{ marginBottom: GAP_SM }}
                value={editForm.gateway ?? ""}
              />

              <AppTextField
                fullWidth
                helperText="Comma or space separated (e.g., 8.8.8.8, 1.1.1.1)"
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
              marginTop: GAP_MD,
            }}
          >
            <AppButton disabled={saving} onClick={onClose}>
              Cancel
            </AppButton>
            <AppButton
              disabled={saving}
              onClick={handleSave}
              variant="contained"
            >
              {saving ? "Saving…" : "Apply Configuration"}
            </AppButton>
          </div>
        </FrostedCard>
      </AppGrid>
    </>
  );
};
export default NetworkInterfaceEditor;
