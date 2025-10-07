import {
  Box,
  Button,
  Collapse,
  TextField,
  ToggleButton,
  ToggleButtonGroup,
  Typography,
  Chip,
} from "@mui/material";
import React, { useState } from "react";
import { toast } from "sonner";

import type { NetworkInterface as BaseNI } from "./NetworkInterfaceList";

import axios from "@/utils/axios";

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
  const [saving, setSaving] = useState(false);

  // Update mode when interface changes
  React.useEffect(() => {
    const detectedMode = iface.ipv4_method === "manual" ? "manual" : "auto";
    setMode(detectedMode);
  }, [iface.ipv4_method]);

  React.useEffect(() => {
    if (!expanded) return;
    if (mode === "manual") {
      // start clean so placeholders show
      setEditForm({ ipv4: "", gateway: "", dns: "" });
    } else if (mode === "auto") {
      setEditForm({});
    }
  }, [mode, expanded, setEditForm]);

  const handleModeChange = (
    _: React.MouseEvent<HTMLElement>,
    newMode: "auto" | "manual" | null,
  ) => {
    if (newMode !== null) {
      setMode(newMode);
      // Reset form when switching modes
      if (newMode === "auto") {
        setEditForm({});
      }
    }
  };

  const handleChange = (field: string, value: string) => {
    setEditForm((prev) => ({ ...prev, [field]: value }));
  };

  const handleDNSChange = (value: string) => {
    setEditForm((prev) => ({ ...prev, dns: value }));
  };

  const validateIPv4CIDR = (cidr: string): boolean => {
    if (!cidr.includes("/")) return false;
    const [ip, prefix] = cidr.split("/");
    const parts = ip.split(".");
    if (parts.length !== 4) return false;
    const prefixNum = parseInt(prefix);
    if (isNaN(prefixNum) || prefixNum < 0 || prefixNum > 32) return false;
    return parts.every((part) => {
      const num = parseInt(part);
      return !isNaN(num) && num >= 0 && num <= 255;
    });
  };

  const validateIPv4 = (ip: string): boolean => {
    const parts = ip.split(".");
    if (parts.length !== 4) return false;
    return parts.every((part) => {
      const num = parseInt(part);
      return !isNaN(num) && num >= 0 && num <= 255;
    });
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      if (mode === "auto") {
        // Set DHCP
        await axios.post("/network/set-ipv4-dhcp", {
          interface: iface.name,
        });
        toast.success("Switched to DHCP mode");
      } else {
        // Manual mode - validate all fields
        const ipv4 = (editForm.ipv4 || "").trim();
        const gateway = (editForm.gateway || "").trim();
        const dnsInput = (editForm.dns || "").trim();

        if (!ipv4) {
          toast.error("IP address is required");
          setSaving(false);
          return;
        }

        if (!validateIPv4CIDR(ipv4)) {
          toast.error(
            "Invalid IPv4 address. Use CIDR format (e.g., 192.168.1.10/24)",
          );
          setSaving(false);
          return;
        }

        if (!gateway) {
          toast.error("Gateway is required");
          setSaving(false);
          return;
        }

        if (!validateIPv4(gateway)) {
          toast.error("Invalid gateway address");
          setSaving(false);
          return;
        }

        if (!dnsInput) {
          toast.error("At least one DNS server is required");
          setSaving(false);
          return;
        }

        // Parse DNS servers (comma or space separated)
        const dnsServers = dnsInput
          .split(/[,\s]+/)
          .map((s: string) => s.trim())
          .filter((s: string) => s.length > 0);

        if (dnsServers.length === 0) {
          toast.error("At least one DNS server is required");
          setSaving(false);
          return;
        }

        // Validate each DNS server
        for (const dns of dnsServers) {
          if (!validateIPv4(dns)) {
            toast.error(`Invalid DNS server: ${dns}`);
            setSaving(false);
            return;
          }
        }

        // Call the unified manual configuration endpoint
        await axios.post("/network/set-ipv4-manual", {
          interface: iface.name,
          address_cidr: ipv4,
          gateway: gateway,
          dns: dnsServers,
        });

        toast.success("Manual configuration saved");
      }

      onSave(iface);
      onClose();
    } catch (e: any) {
      toast.error(
        e?.response?.data?.error || "Failed to save network configuration",
      );
    } finally {
      setSaving(false);
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
          <Typography variant="subtitle2">Configuration Mode</Typography>
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
              value={editForm.ipv4 || ""}
              onChange={(e) => handleChange("ipv4", e.target.value)}
              helperText="Format: IP/prefix (e.g., 192.168.1.10/24)"
              sx={{ mb: 2 }}
            />

            <TextField
              fullWidth
              required
              label="Gateway"
              placeholder="192.168.1.1"
              value={editForm.gateway || ""}
              onChange={(e) => handleChange("gateway", e.target.value)}
              helperText="The IP address of your network gateway/router"
              sx={{ mb: 2 }}
            />

            <TextField
              fullWidth
              required
              label="DNS Servers"
              placeholder="8.8.8.8, 8.8.4.4"
              value={editForm.dns || ""}
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
