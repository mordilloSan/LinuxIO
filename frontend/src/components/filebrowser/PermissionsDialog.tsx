import React, { useCallback, useState } from "react";

import AppCheckbox from "../ui/AppCheckbox";

import { linuxio } from "@/api";
import FileBrowserDialog from "@/components/dialog/GeneralDialog";
import AppDataTable from "@/components/tables/AppDataTable";
import type { AppDataTableColumnDef } from "@/components/tables/AppDataTable";
import AppAutocomplete from "@/components/ui/AppAutocomplete";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppTypography from "@/components/ui/AppTypography";
import { useAppMediaQuery, useAppTheme } from "@/theme";

interface PermissionsDialogProps {
  currentMode: string; // e.g., "0755", "755", or "-rw-r--r--"
  group?: string;
  isDirectory: boolean;
  onClose: () => void;
  onConfirm: (
    mode: string,
    recursive: boolean,
    owner?: string,
    group?: string,
  ) => void;
  open: boolean;
  owner?: string;
  pathLabel: string;
  selectionCount: number;
}
interface PermissionBits {
  group: {
    read: boolean;
    write: boolean;
    execute: boolean;
  };
  others: {
    read: boolean;
    write: boolean;
    execute: boolean;
  };
  owner: {
    read: boolean;
    write: boolean;
    execute: boolean;
  };
}

type PermissionCategory = keyof PermissionBits;
type PermissionFlag = "execute" | "read" | "write";

interface PermissionMatrixRow {
  id: PermissionCategory;
  label: string;
}

const permissionRows: PermissionMatrixRow[] = [
  { id: "owner", label: "Owner" },
  { id: "group", label: "Group" },
  { id: "others", label: "Others" },
];
const permissionFlags: PermissionFlag[] = ["read", "write", "execute"];

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

  const [prevOpen, setPrevOpen] = useState(false);
  if (open !== prevOpen) {
    setPrevOpen(open);
    if (open) {
      setOwnerInput(owner || "");
      setGroupInput(group || "");
    }
  }
  // Fetch users and groups when dialog opens
  const { data: usersGroupsData } = linuxio.filebrowser.users_groups.useQuery({
    enabled: open,
  });

  // Derive available users and groups directly from query data
  const availableUsers = usersGroupsData?.users || [];
  const availableGroups = usersGroupsData?.groups || [];
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
  const theme = useAppTheme();
  const isMobile = useAppMediaQuery(theme.breakpoints.down("sm"));
  const permissionColumns: AppDataTableColumnDef<PermissionMatrixRow>[] = [
    {
      accessorKey: "label",
      header: "",
      cell: ({ row }) => (
        <AppTypography fontWeight={500} variant="body2">
          {row.original.label}
        </AppTypography>
      ),
    },
    ...permissionFlags.map<AppDataTableColumnDef<PermissionMatrixRow>>(
      (flag) => ({
        id: flag,
        header: flag[0].toUpperCase() + flag.slice(1),
        cell: ({ row }) => (
          <AppCheckbox
            checked={permissions[row.original.id][flag]}
            onChange={() => handlePermissionChange(row.original.id, flag)}
          />
        ),
        meta: { align: "center" },
      }),
    ),
  ];

  return (
    <FileBrowserDialog
      fullWidth
      key={open ? `${currentMode}-${owner}-${group}` : "closed"}
      maxWidth="sm"
      onClose={onClose}
      open={open}
    >
      <AppDialogTitle
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          gap: 4,
        }}
      >
        <AppTypography component="span" variant="h6">
          Change Permissions
        </AppTypography>
        <AppTypography
          color="text.secondary"
          component="span"
          style={{
            textAlign: "right",
          }}
          variant="caption"
        >
          {pathLabel}
        </AppTypography>
      </AppDialogTitle>
      <AppDialogContent
        style={{
          overflow: "visible",
        }}
      >
        <div
          style={{
            display: "grid",
            gridTemplateColumns: isMobile ? "1fr" : "1fr 1fr",
            gap: theme.spacing(2),
            marginBottom: theme.spacing(3),
            marginTop: theme.spacing(1),
          }}
        >
          <AppAutocomplete
            freeSolo
            fullWidth
            label="Owner"
            maxListHeight={150}
            onChange={setOwnerInput}
            onInputChange={setOwnerInput}
            options={availableUsers}
            shrinkLabel
            size="small"
            value={ownerInput}
          />

          <AppAutocomplete
            freeSolo
            fullWidth
            label="Group"
            maxListHeight={150}
            onChange={setGroupInput}
            onInputChange={setGroupInput}
            options={availableGroups}
            shrinkLabel
            size="small"
            value={groupInput}
          />
        </div>

        <AppDataTable
          ariaLabel="File permissions matrix"
          columns={permissionColumns}
          data={permissionRows}
          density="compact"
          enableSorting={false}
          getRowId={(row) => row.id}
          maxHeight={170}
          variant="embedded"
        />

        {isDirectory && (
          <div
            style={{
              marginTop: theme.spacing(2),
            }}
          >
            <AppFormControlLabel
              control={
                <AppCheckbox
                  checked={recursive}
                  onChange={(e) => setRecursive(e.target.checked)}
                />
              }
              label="Apply recursively to all files and subdirectories"
            />
          </div>
        )}
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={onClose}>Cancel</AppButton>
        <AppButton onClick={handleConfirm} variant="contained">
          Apply
        </AppButton>
      </AppDialogActions>
    </FileBrowserDialog>
  );
};
export default PermissionsDialog;
