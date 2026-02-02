import {
  Box,
  Button,
  Collapse,
  TextField,
  ToggleButton,
  ToggleButtonGroup,
  Typography,
  Chip,
  Switch,
  FormControlLabel,
} from "@mui/material";
import { useQueryClient } from "@tanstack/react-query";
import React, { useEffect, useEffectEvent, useMemo, useState } from "react";
import { toast } from "sonner";

import type { NetworkInterface as BaseNI } from "./NetworkInterfaceList";

import linuxio from "@/api/react-query";
import { getMutationErrorMessage } from "@/utils/mutations";

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

const isEmptyForm = (f: Record<string, any> | undefined) =>
  !f ||
  (Object.keys(f).length === 0 && f.constructor === Object) ||
  (!f.ipv4 && !f.gateway && !f.dns);

/* ============================================ */

interface Props {
  iface: BaseNI;
  expanded: boolean;
  editForm: Record<string, any>;
  setEditForm: React.Dispatch<React.SetStateAction<Record<string, any>>>;
  onClose: () => void;
  onSave: (iface: BaseNI) => void;
}

const NetworkInterfaceEditor: React.FC<Props> = ({
  iface,
  expanded,
  editForm,
  setEditForm,
  onClose,
  onSave,
}) => {
  const [mode, setMode] = useState<"auto" | "manual">("auto");
  const [dirty, setDirty] = useState(false);
  const queryClient = useQueryClient();

  // Mutations
  const { mutate: setIPv4, isPending: isSettingIPv4 } =
    linuxio.dbus.SetIPv4.useMutation({
      onSuccess: () => {
        toast.success("Switched to DHCP mode");
        queryClient.invalidateQueries({
          queryKey: ["linuxio", "dbus", "ListNetworkInterfaces"],
        });
        onSave(iface);
        onClose();
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to set DHCP configuration"),
        );
      },
    });

  const { mutate: setIPv4Manual, isPending: isSettingIPv4Manual } =
    linuxio.dbus.SetIPv4Manual.useMutation({
      onSuccess: () => {
        toast.success("Manual configuration saved");
        queryClient.invalidateQueries({
          queryKey: ["linuxio", "dbus", "ListNetworkInterfaces"],
        });
        onSave(iface);
        onClose();
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(
            error,
            "Failed to save network configuration",
          ),
        );
      },
    });

  const { mutate: enableConnection, isPending: isEnabling } =
    linuxio.dbus.EnableConnection.useMutation({
      onSuccess: () => {
        toast.success("Connection enabled");
        queryClient.invalidateQueries({
          queryKey: ["linuxio", "dbus", "GetNetworkInfo"],
        });
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to enable connection"),
        );
      },
    });

  const { mutate: disableConnection, isPending: isDisabling } =
    linuxio.dbus.DisableConnection.useMutation({
      onSuccess: () => {
        toast.success("Connection disabled");
        queryClient.invalidateQueries({
          queryKey: ["linuxio", "dbus", "GetNetworkInfo"],
        });
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to disable connection"),
        );
      },
    });

  const saving = isSettingIPv4 || isSettingIPv4Manual;
  const toggling = isEnabling || isDisabling;
  const isConnected = iface.state === 100;
  const isConnecting = iface.state >= 40 && iface.state <= 90;

  const handleConnectionToggle = () => {
    if (isConnected || isConnecting) {
      disableConnection([iface.name]);
    } else {
      enableConnection([iface.name]);
    }
  };

  // Compute sane defaults from iface (will be used to prefill manual fields)
  const defaults = useMemo(() => {
    const ipv4 = getIPv4FromIface(iface);
    const gateway = getGatewayV4(iface);
    const dnsArr = getDNSv4List(iface);
    return { ipv4, gateway, dns: dnsArr.join(", ") };
  }, [iface]);

  const syncModeWithIface = useEffectEvent(() => {
    setMode(iface.ipv4_method === "manual" ? "manual" : "auto");
  });

  const resetDirtyState = useEffectEvent(() => {
    setDirty(false);
  });

  // Keep mode in sync with iface
  useEffect(() => {
    syncModeWithIface();
  }, [iface.ipv4_method]);

  // Prefill when expanded + manual (without clobbering user input)
  useEffect(() => {
    if (!expanded) return;
    if (mode === "manual") {
      if (!dirty || isEmptyForm(editForm)) {
        setEditForm({
          ipv4: defaults.ipv4 || "",
          gateway: defaults.gateway || "",
          dns: defaults.dns || "",
        });
      }
    } else {
      // Auto mode: clear manual-only inputs
      setEditForm({});
    }
  }, [expanded, mode, defaults, dirty, editForm, setEditForm]);

  // Reset dirty when switching to another interface
  useEffect(() => {
    resetDirtyState();
  }, [iface.name]);

  const handleModeChange = (
    _: React.MouseEvent<HTMLElement>,
    newMode: "auto" | "manual" | null,
  ) => {
    if (!newMode) return;
    setMode(newMode);
    if (newMode === "auto") {
      setEditForm({});
    } else {
      // Prefill immediately when switching to manual
      setEditForm({
        ipv4: defaults.ipv4 || "",
        gateway: defaults.gateway || "",
        dns: defaults.dns || "",
      });
      setDirty(false);
    }
  };

  const handleChange = (field: string, value: string) => {
    setDirty(true);
    setEditForm((prev) => ({ ...prev, [field]: value }));
  };

  const handleDNSChange = (value: string) => {
    setDirty(true);
    setEditForm((prev) => ({ ...prev, dns: value }));
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
      setIPv4([iface.name, "dhcp"]);
    } else {
      const ipv4 = (editForm.ipv4 || "").trim();
      const gateway = (editForm.gateway || "").trim();
      const dnsInput = (editForm.dns || "").trim();

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

      // SetIPv4Manual: args = [interface, addressCIDR, gateway, ...dnsServers]
      setIPv4Manual([iface.name, ipv4, gateway, ...dnsServers]);
    }
  };

  return (
    <Collapse in={expanded} timeout="auto" unmountOnExit>
      <Box mt={2} p={2} borderRadius={1}>
        <Box
          display="flex"
          alignItems="center"
          justifyContent="space-between"
          sx={{ mb: 2 }}
        >
          <FormControlLabel
            control={
              <Switch
                checked={isConnected || isConnecting}
                onChange={handleConnectionToggle}
                disabled={toggling}
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
          <Chip
            size="small"
            color="primary"
            label={
              iface.ipv4_method === "manual"
                ? "static IP"
                : iface.ipv4_method === "auto"
                  ? "DHCP"
                  : iface.ipv4_method === "disabled"
                    ? "IPv4 disabled"
                    : "IPv4: unknown"
            }
          />
        </Box>

        <ToggleButtonGroup
          value={mode}
          exclusive
          onChange={handleModeChange}
          fullWidth
          sx={{ mb: 2 }}
        >
          <ToggleButton value="auto">Automatic</ToggleButton>
          <ToggleButton value="manual">Manual</ToggleButton>
        </ToggleButtonGroup>

        {mode === "auto" ? (
          <Box>
            <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
              The interface will automatically obtain IP address, gateway, and
              DNS from a DHCP server.
            </Typography>
          </Box>
        ) : (
          <Box>
            <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
              Configure static network settings. All fields are required.
            </Typography>

            <TextField
              fullWidth
              required
              label="IPv4 Address (CIDR)"
              placeholder="192.168.1.10/24"
              value={editForm.ipv4 ?? ""}
              onChange={(e) => handleChange("ipv4", e.target.value)}
              helperText="Format: IP/prefix (e.g., 192.168.1.10/24)"
              sx={{ mb: 2 }}
            />

            <TextField
              fullWidth
              required
              label="Gateway"
              placeholder="192.168.1.1"
              value={editForm.gateway ?? ""}
              onChange={(e) => handleChange("gateway", e.target.value)}
              helperText="The IP address of your network gateway/router"
              sx={{ mb: 2 }}
            />

            <TextField
              fullWidth
              required
              label="DNS Servers"
              placeholder="8.8.8.8, 8.8.4.4"
              value={editForm.dns ?? ""}
              onChange={(e) => handleDNSChange(e.target.value)}
              helperText="Comma or space separated (e.g., 8.8.8.8, 1.1.1.1)"
              sx={{ mb: 2 }}
            />
          </Box>
        )}

        <Box display="flex" justifyContent="flex-end" gap={1} mt={2}>
          <Button onClick={onClose} disabled={saving}>
            Cancel
          </Button>
          <Button variant="contained" onClick={handleSave} disabled={saving}>
            {saving ? "Saving…" : "Apply Configuration"}
          </Button>
        </Box>
      </Box>
    </Collapse>
  );
};

export default NetworkInterfaceEditor;
