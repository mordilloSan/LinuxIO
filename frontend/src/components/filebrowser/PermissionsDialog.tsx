import {
  Autocomplete,
  Box,
  Button,
  Checkbox,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  FormControlLabel,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  TextField,
  Typography,
  useTheme,
} from "@mui/material";
import React, { useState, useCallback, useEffect } from "react";

import * as linuxio from "@/api/linuxio-core";

interface PermissionsDialogProps {
  open: boolean;
  pathLabel: string;
  selectionCount: number;
  currentMode: string; // e.g., "0755", "755", or "-rw-r--r--"
  isDirectory: boolean;
  owner?: string;
  group?: string;
  onClose: () => void;
  onConfirm: (
    mode: string,
    recursive: boolean,
    owner?: string,
    group?: string,
  ) => void;
}

interface PermissionBits {
  owner: { read: boolean; write: boolean; execute: boolean };
  group: { read: boolean; write: boolean; execute: boolean };
  others: { read: boolean; write: boolean; execute: boolean };
}

const parseSymbolicMode = (mode: string): PermissionBits => {
  const charAt = (index: number) => mode[index] || "";
  const hasExec = (value: string) => ["x", "s", "t"].includes(value);

  return {
    owner: {
      read: charAt(1) === "r",
      write: charAt(2) === "w",
      execute: hasExec(charAt(3)),
    },
    group: {
      read: charAt(4) === "r",
      write: charAt(5) === "w",
      execute: hasExec(charAt(6)),
    },
    others: {
      read: charAt(7) === "r",
      write: charAt(8) === "w",
      execute: hasExec(charAt(9)),
    },
  };
};

const parseMode = (mode: string): PermissionBits => {
  const trimmed = mode.trim();
  const octalMatch = trimmed.match(/[0-7]{3}$/);
  if (octalMatch) {
    const octal = octalMatch[0];
    const [owner, group, others] = octal.split("").map(Number);

    return {
      owner: {
        read: (owner & 4) !== 0,
        write: (owner & 2) !== 0,
        execute: (owner & 1) !== 0,
      },
      group: {
        read: (group & 4) !== 0,
        write: (group & 2) !== 0,
        execute: (group & 1) !== 0,
      },
      others: {
        read: (others & 4) !== 0,
        write: (others & 2) !== 0,
        execute: (others & 1) !== 0,
      },
    };
  }

  // Fallback for symbolic strings like "-rw-r--r--"
  return parseSymbolicMode(trimmed);
};

const permissionsToOctal = (perms: PermissionBits): string => {
  const ownerBits =
    (perms.owner.read ? 4 : 0) +
    (perms.owner.write ? 2 : 0) +
    (perms.owner.execute ? 1 : 0);
  const groupBits =
    (perms.group.read ? 4 : 0) +
    (perms.group.write ? 2 : 0) +
    (perms.group.execute ? 1 : 0);
  const othersBits =
    (perms.others.read ? 4 : 0) +
    (perms.others.write ? 2 : 0) +
    (perms.others.execute ? 1 : 0);

  return `${ownerBits}${groupBits}${othersBits}`;
};

const PermissionsDialog: React.FC<PermissionsDialogProps> = ({
  open,
  pathLabel,
  currentMode,
  isDirectory,
  owner,
  group,
  onClose,
  onConfirm,
}) => {
  const [permissions, setPermissions] = useState<PermissionBits>(() =>
    parseMode(currentMode),
  );
  const [recursive, setRecursive] = useState(false);
  const [ownerInput, setOwnerInput] = useState(owner || "");
  const [groupInput, setGroupInput] = useState(group || "");
  const [availableUsers, setAvailableUsers] = useState<string[]>([]);
  const [availableGroups, setAvailableGroups] = useState<string[]>([]);

  // Fetch users and groups when dialog opens
  useEffect(() => {
    if (open) {
      const fetchUsersAndGroups = async () => {
        try {
          // Args: []
          const data = await linuxio.call<{
            users: string[];
            groups: string[];
          }>("filebrowser", "users_groups", []);
          setAvailableUsers(data.users || []);
          setAvailableGroups(data.groups || []);
        } catch (error) {
          console.error("Failed to fetch users and groups:", error);
          // Continue without autocomplete data
        }
      };
      fetchUsersAndGroups();
    }
  }, [open]);

  const handlePermissionChange = useCallback(
    (category: keyof PermissionBits, type: "read" | "write" | "execute") => {
      setPermissions((prev) => ({
        ...prev,
        [category]: {
          ...prev[category],
          [type]: !prev[category][type],
        },
      }));
    },
    [],
  );

  const handleConfirm = useCallback(() => {
    const mode = permissionsToOctal(permissions);
    const nextOwner = ownerInput.trim() || undefined;
    const nextGroup = groupInput.trim() || undefined;
    onConfirm(mode, recursive, nextOwner, nextGroup);
    onClose();
  }, [permissions, recursive, ownerInput, groupInput, onConfirm, onClose]);

  const theme = useTheme();

  return (
    <Dialog
      open={open}
      onClose={onClose}
      maxWidth="sm"
      fullWidth
      key={open ? `${currentMode}-${owner}-${group}` : "closed"}
      slotProps={{
        paper: {
          sx: {
            backgroundColor: theme.header.background,
            borderRadius: 4,
            border: `1px solid rgba(255, 255, 255, 0.2)`,
            boxShadow: `0 0 10px rgba(255, 255, 255, 0.5), 0 0 20px rgba(255, 255, 255, 0.3), inset 0 0 20px rgba(255, 255, 255, 0.1)`,
            backdropFilter: "blur(10px)",
          },
        },
        backdrop: {
          sx: {
            backdropFilter: "blur(4px)",
            backgroundColor: "rgba(0, 0, 0, 0.7)",
          },
        },
      }}
    >
      <DialogTitle
        sx={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          gap: 1,
        }}
      >
        <Typography component="span" variant="h6">
          Change Permissions
        </Typography>
        <Typography
          component="span"
          variant="caption"
          color="text.secondary"
          sx={{ textAlign: "right" }}
        >
          {pathLabel}
        </Typography>
      </DialogTitle>
      <DialogContent sx={{ overflow: "visible" }}>
        <Box
          sx={{
            display: "grid",
            gridTemplateColumns: { xs: "1fr", sm: "1fr 1fr" },
            gap: 2,
            mb: 3,
            mt: 1,
          }}
        >
          <Autocomplete
            freeSolo
            options={availableUsers}
            value={ownerInput}
            onInputChange={(_, newValue) => setOwnerInput(newValue)}
            slotProps={{
              listbox: {
                className: "custom-scrollbar",
                sx: {
                  maxHeight: "200px",
                },
              },
            }}
            sx={{
              "& .MuiOutlinedInput-root:hover .MuiOutlinedInput-notchedOutline":
                {
                  borderColor: theme.palette.primary.main,
                },
            }}
            renderInput={(params) => (
              <TextField {...params} label="Owner" size="small" />
            )}
          />
          <Autocomplete
            freeSolo
            options={availableGroups}
            value={groupInput}
            onInputChange={(_, newValue) => setGroupInput(newValue)}
            slotProps={{
              listbox: {
                className: "custom-scrollbar",
                sx: {
                  maxHeight: "200px",
                },
              },
            }}
            sx={{
              "& .MuiOutlinedInput-root:hover .MuiOutlinedInput-notchedOutline":
                {
                  borderColor: theme.palette.primary.main,
                },
            }}
            renderInput={(params) => (
              <TextField {...params} label="Group" size="small" />
            )}
          />
        </Box>

        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell></TableCell>
              <TableCell align="center">Read</TableCell>
              <TableCell align="center">Write</TableCell>
              <TableCell align="center">Execute</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            <TableRow>
              <TableCell>
                <Typography variant="body2" fontWeight="medium">
                  Owner
                </Typography>
              </TableCell>
              <TableCell align="center">
                <Checkbox
                  checked={permissions.owner.read}
                  onChange={() => handlePermissionChange("owner", "read")}
                />
              </TableCell>
              <TableCell align="center">
                <Checkbox
                  checked={permissions.owner.write}
                  onChange={() => handlePermissionChange("owner", "write")}
                />
              </TableCell>
              <TableCell align="center">
                <Checkbox
                  checked={permissions.owner.execute}
                  onChange={() => handlePermissionChange("owner", "execute")}
                />
              </TableCell>
            </TableRow>
            <TableRow>
              <TableCell>
                <Typography variant="body2" fontWeight="medium">
                  Group
                </Typography>
              </TableCell>
              <TableCell align="center">
                <Checkbox
                  checked={permissions.group.read}
                  onChange={() => handlePermissionChange("group", "read")}
                />
              </TableCell>
              <TableCell align="center">
                <Checkbox
                  checked={permissions.group.write}
                  onChange={() => handlePermissionChange("group", "write")}
                />
              </TableCell>
              <TableCell align="center">
                <Checkbox
                  checked={permissions.group.execute}
                  onChange={() => handlePermissionChange("group", "execute")}
                />
              </TableCell>
            </TableRow>
            <TableRow>
              <TableCell>
                <Typography variant="body2" fontWeight="medium">
                  Others
                </Typography>
              </TableCell>
              <TableCell align="center">
                <Checkbox
                  checked={permissions.others.read}
                  onChange={() => handlePermissionChange("others", "read")}
                />
              </TableCell>
              <TableCell align="center">
                <Checkbox
                  checked={permissions.others.write}
                  onChange={() => handlePermissionChange("others", "write")}
                />
              </TableCell>
              <TableCell align="center">
                <Checkbox
                  checked={permissions.others.execute}
                  onChange={() => handlePermissionChange("others", "execute")}
                />
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>

        {isDirectory && (
          <Box sx={{ mt: 2 }}>
            <FormControlLabel
              control={
                <Checkbox
                  checked={recursive}
                  onChange={(e) => setRecursive(e.target.checked)}
                />
              }
              label="Apply recursively to all files and subdirectories"
            />
          </Box>
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Cancel</Button>
        <Button onClick={handleConfirm} variant="contained">
          Apply
        </Button>
      </DialogActions>
    </Dialog>
  );
};

export default PermissionsDialog;
