import { Box, Button, Collapse, TextField } from "@mui/material";
import React from "react";

import { NetworkInterface } from "./NetworkInterfaceList";

interface Props {
  iface: NetworkInterface;
  expanded: boolean;
  editForm: Record<string, any>;
  setEditForm: React.Dispatch<React.SetStateAction<Record<string, any>>>;
  onClose: () => void;
  onSave: (iface: NetworkInterface) => void;
}

const NetworkInterfaceEditor: React.FC<Props> = ({
  iface,
  expanded,
  editForm,
  setEditForm,
  onClose,
  onSave,
}) => {
  const handleChange = (field: string, value: string) => {
    setEditForm((prev) => ({ ...prev, [field]: value }));
  };

  return (
    <Collapse in={expanded} timeout="auto" unmountOnExit>
      <Box mt={2}>
        <TextField
          fullWidth
          label="IPv4"
          value={editForm.ipv4 || ""}
          onChange={(e) => handleChange("ipv4", e.target.value)}
          sx={{ mb: 1 }}
        />
        <TextField
          fullWidth
          label="IPv6"
          value={editForm.ipv6 || ""}
          onChange={(e) => handleChange("ipv6", e.target.value)}
          sx={{ mb: 1 }}
        />
        <TextField
          fullWidth
          label="DNS"
          value={editForm.dns || ""}
          onChange={(e) => handleChange("dns", e.target.value)}
          sx={{ mb: 1 }}
        />
        <TextField
          fullWidth
          label="Gateway"
          value={editForm.gateway || ""}
          onChange={(e) => handleChange("gateway", e.target.value)}
          sx={{ mb: 1 }}
        />
        <TextField
          fullWidth
          type="number"
          label="MTU"
          value={editForm.mtu || ""}
          onChange={(e) => handleChange("mtu", e.target.value)}
          sx={{ mb: 2 }}
        />
        <Box display="flex" justifyContent="flex-end" gap={1}>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="contained" onClick={() => onSave(iface)}>
            Save
          </Button>
        </Box>
      </Box>
    </Collapse>
  );
};

export default NetworkInterfaceEditor;
