import DeleteIcon from "@mui/icons-material/Delete";
import EditIcon from "@mui/icons-material/Edit";
import LockIcon from "@mui/icons-material/Lock";
import LockOpenIcon from "@mui/icons-material/LockOpen";
import PasswordIcon from "@mui/icons-material/Password";
import {
  Box,
  TableCell,
  TextField,
  Chip,
  Typography,
  Checkbox,
  Button,
  IconButton,
  Tooltip,
} from "@mui/material";
import { useQueryClient } from "@tanstack/react-query";
import React, { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";

import ChangePasswordDialog from "./components/ChangePasswordDialog";
import CreateUserDialog from "./components/CreateUserDialog";
import DeleteUserDialog from "./components/DeleteUserDialog";
import EditUserDialog from "./components/EditUserDialog";

import { linuxio, type AccountUser } from "@/api";
import UnifiedCollapsibleTable, {
  UnifiedTableColumn,
} from "@/components/tables/UnifiedCollapsibleTable";
import useAuth from "@/hooks/useAuth";
import { responsiveTextStyles } from "@/theme/tableStyles";
import { getMutationErrorMessage } from "@/utils/mutations";

interface UsersTabProps {
  onMountCreateHandler?: (handler: () => void) => void;
}

const UsersTab: React.FC<UsersTabProps> = ({ onMountCreateHandler }) => {
  const queryClient = useQueryClient();
  const { user: currentUser } = useAuth();
  const { data: users = [] } = linuxio.accounts.list_users.useQuery({
    refetchInterval: 10000,
  });

  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [editDialogOpen, setEditDialogOpen] = useState(false);
  const [passwordDialogOpen, setPasswordDialogOpen] = useState(false);
  const [selectedUser, setSelectedUser] = useState<AccountUser | null>(null);

  const usersList = Array.isArray(users) ? users : [];

  const handleCreateUser = useCallback(() => {
    setCreateDialogOpen(true);
  }, []);

  useEffect(() => {
    if (onMountCreateHandler) {
      onMountCreateHandler(handleCreateUser);
    }
  }, [onMountCreateHandler, handleCreateUser]);

  const filtered = usersList.filter(
    (user) =>
      user.username.toLowerCase().includes(search.toLowerCase()) ||
      user.gecos.toLowerCase().includes(search.toLowerCase()) ||
      user.primaryGroup.toLowerCase().includes(search.toLowerCase()),
  );

  const effectiveSelected = useMemo(() => {
    const filteredNames = new Set(filtered.map((u) => u.username));
    const result = new Set<string>();
    selected.forEach((name) => {
      if (filteredNames.has(name)) {
        result.add(name);
      }
    });
    return result;
  }, [selected, filtered]);

  const handleSelectAll = (checked: boolean) => {
    if (checked) {
      // Don't allow selecting root or current user
      setSelected(
        new Set(
          filtered
            .filter(
              (u) => u.username !== "root" && u.username !== currentUser?.name,
            )
            .map((u) => u.username),
        ),
      );
    } else {
      setSelected(new Set());
    }
  };

  const handleSelectOne = (username: string, checked: boolean) => {
    if (username === "root" || username === currentUser?.name) return;
    setSelected((prev) => {
      const next = new Set(prev);
      if (checked) {
        next.add(username);
      } else {
        next.delete(username);
      }
      return next;
    });
  };

  const handleDeleteSuccess = () => {
    setSelected(new Set());
  };

  const handleEditUser = (user: AccountUser) => {
    setSelectedUser(user);
    setEditDialogOpen(true);
  };

  const handleChangePassword = (user: AccountUser) => {
    setSelectedUser(user);
    setPasswordDialogOpen(true);
  };

  const { mutate: lockUser, isPending: isLocking } =
    linuxio.accounts.lock_user.useMutation({
      onSuccess: () => {
        toast.success("User locked successfully");
        queryClient.invalidateQueries({
          queryKey: linuxio.accounts.list_users.queryKey(),
        });
      },
      onError: (error: Error) => {
        toast.error(getMutationErrorMessage(error, "Failed to lock user"));
      },
    });

  const { mutate: unlockUser, isPending: isUnlocking } =
    linuxio.accounts.unlock_user.useMutation({
      onSuccess: () => {
        toast.success("User unlocked successfully");
        queryClient.invalidateQueries({
          queryKey: linuxio.accounts.list_users.queryKey(),
        });
      },
      onError: (error: Error) => {
        toast.error(getMutationErrorMessage(error, "Failed to unlock user"));
      },
    });

  const handleToggleLock = (user: AccountUser) => {
    if (user.username === "root" || user.username === currentUser?.name) return;
    if (user.isLocked) {
      unlockUser([user.username]);
    } else {
      lockUser([user.username]);
    }
  };

  const selectedUsers = filtered.filter((u) =>
    effectiveSelected.has(u.username),
  );
  const selectableUsers = filtered.filter(
    (u) => u.username !== "root" && u.username !== currentUser?.name,
  );
  const allSelected =
    selectableUsers.length > 0 &&
    effectiveSelected.size === selectableUsers.length;
  const someSelected =
    effectiveSelected.size > 0 &&
    effectiveSelected.size < selectableUsers.length;

  // Format last login for display
  const formatLastLogin = (lastLogin: string, username: string): string => {
    if (!lastLogin || lastLogin === "Never") {
      return "Never logged in";
    }
    if (username === currentUser?.name) {
      return "Logged in";
    }
    return lastLogin;
  };

  // Get all groups for a user (primary + secondary)
  const getAllGroups = (user: AccountUser): string[] => {
    const allGroups: string[] = [user.primaryGroup];
    if (user.groups) {
      user.groups.forEach((g) => {
        if (!allGroups.includes(g)) {
          allGroups.push(g);
        }
      });
    }
    return allGroups;
  };

  const columns: UnifiedTableColumn[] = [
    { field: "username", headerName: "Username", align: "left" },
    {
      field: "gecos",
      headerName: "Full Name",
      align: "left",
      sx: { display: { xs: "none", sm: "table-cell" } },
    },
    {
      field: "uid",
      headerName: "ID",
      align: "left",
      width: "80px",
      sx: { display: { xs: "none", md: "table-cell" } },
    },
    {
      field: "lastLogin",
      headerName: "Last Active",
      align: "left",
      sx: { display: { xs: "none", lg: "table-cell" } },
    },
    {
      field: "groups",
      headerName: "Groups",
      align: "left",
      sx: { display: { xs: "none", xl: "table-cell" } },
    },
    {
      field: "actions",
      headerName: "Actions",
      align: "right",
      width: "150px",
    },
  ];

  return (
    <Box>
      <Box mb={2} display="flex" alignItems="center" gap={2} flexWrap="wrap">
        <TextField
          variant="outlined"
          size="small"
          placeholder="Search users…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          sx={{
            width: 320,
            "@media (max-width: 600px)": {
              width: "100%",
            },
          }}
        />
        <Box fontWeight="bold">{filtered.length} shown</Box>
        {effectiveSelected.size > 0 && (
          <Button
            variant="contained"
            color="error"
            size="small"
            startIcon={<DeleteIcon />}
            onClick={() => setDeleteDialogOpen(true)}
          >
            Delete ({effectiveSelected.size})
          </Button>
        )}
      </Box>
      <UnifiedCollapsibleTable
        data={filtered}
        columns={columns}
        getRowKey={(user) => user.username}
        renderFirstCell={(user) => (
          <Checkbox
            size="small"
            checked={effectiveSelected.has(user.username)}
            onChange={(e) => handleSelectOne(user.username, e.target.checked)}
            onClick={(e) => e.stopPropagation()}
            disabled={
              user.username === "root" || user.username === currentUser?.name
            }
          />
        )}
        renderHeaderFirstCell={() => (
          <Checkbox
            size="small"
            checked={allSelected}
            indeterminate={someSelected}
            onChange={(e) => handleSelectAll(e.target.checked)}
          />
        )}
        renderMainRow={(user) => (
          <>
            <TableCell>
              <Box display="flex" alignItems="center" gap={1} flexWrap="wrap">
                <Typography
                  variant="body2"
                  fontWeight="medium"
                  sx={responsiveTextStyles}
                >
                  {user.username}
                </Typography>
                {user.username === currentUser?.name && (
                  <Chip
                    label="Your account"
                    size="small"
                    color="primary"
                    sx={{ fontSize: "0.65rem", height: 20 }}
                  />
                )}
                {user.isLocked && (
                  <Chip
                    label="locked"
                    size="small"
                    color="warning"
                    sx={{ fontSize: "0.65rem", height: 20 }}
                  />
                )}
              </Box>
            </TableCell>
            <TableCell sx={{ display: { xs: "none", sm: "table-cell" } }}>
              <Typography variant="body2" sx={responsiveTextStyles}>
                {user.gecos || "-"}
              </Typography>
            </TableCell>
            <TableCell sx={{ display: { xs: "none", md: "table-cell" } }}>
              <Typography variant="body2" sx={responsiveTextStyles}>
                {user.uid}
              </Typography>
            </TableCell>
            <TableCell sx={{ display: { xs: "none", lg: "table-cell" } }}>
              <Typography
                variant="body2"
                sx={responsiveTextStyles}
                color={
                  user.username === currentUser?.name
                    ? "success.main"
                    : "text.secondary"
                }
              >
                {formatLastLogin(user.lastLogin, user.username)}
              </Typography>
            </TableCell>
            <TableCell sx={{ display: { xs: "none", xl: "table-cell" } }}>
              <Box display="flex" flexWrap="wrap" gap={0.5}>
                {getAllGroups(user)
                  .slice(0, 3)
                  .map((group, idx) => (
                    <Chip
                      key={group}
                      label={
                        idx === 0
                          ? `${group} (${user.primaryGroup === group ? "primary" : ""})`.replace(
                              " ()",
                              "",
                            )
                          : group
                      }
                      size="small"
                      variant={idx === 0 ? "filled" : "outlined"}
                      sx={{ fontSize: "0.65rem", height: 20 }}
                    />
                  ))}
                {getAllGroups(user).length > 3 && (
                  <Chip
                    label={`+${getAllGroups(user).length - 3}`}
                    size="small"
                    variant="outlined"
                    sx={{ fontSize: "0.65rem", height: 20 }}
                  />
                )}
              </Box>
            </TableCell>
            <TableCell align="right">
              <Box display="flex" justifyContent="flex-end" gap={0.5}>
                <Tooltip title="Edit">
                  <IconButton
                    size="small"
                    onClick={(e) => {
                      e.stopPropagation();
                      handleEditUser(user);
                    }}
                    disabled={user.username === "root"}
                  >
                    <EditIcon fontSize="small" />
                  </IconButton>
                </Tooltip>
                <Tooltip title="Change Password">
                  <IconButton
                    size="small"
                    onClick={(e) => {
                      e.stopPropagation();
                      handleChangePassword(user);
                    }}
                  >
                    <PasswordIcon fontSize="small" />
                  </IconButton>
                </Tooltip>
                <Tooltip title={user.isLocked ? "Unlock" : "Lock"}>
                  <IconButton
                    size="small"
                    onClick={(e) => {
                      e.stopPropagation();
                      handleToggleLock(user);
                    }}
                    disabled={
                      user.username === "root" ||
                      user.username === currentUser?.name ||
                      isLocking ||
                      isUnlocking
                    }
                  >
                    {user.isLocked ? (
                      <LockOpenIcon fontSize="small" />
                    ) : (
                      <LockIcon fontSize="small" />
                    )}
                  </IconButton>
                </Tooltip>
              </Box>
            </TableCell>
          </>
        )}
        renderExpandedContent={(user) => (
          <>
            <Typography variant="subtitle2" gutterBottom>
              <b>Home Directory:</b>
            </Typography>
            <Typography
              variant="body2"
              sx={{
                fontFamily: "monospace",
                fontSize: "0.85rem",
                mb: 2,
              }}
            >
              {user.homeDir}
            </Typography>

            <Typography variant="subtitle2" gutterBottom>
              <b>Shell:</b>
            </Typography>
            <Typography variant="body2" sx={{ mb: 2, fontSize: "0.85rem" }}>
              {user.shell}
            </Typography>

            <Typography variant="subtitle2" gutterBottom>
              <b>All Groups:</b>
            </Typography>
            <Box sx={{ mb: 2, display: "flex", flexWrap: "wrap" }}>
              {getAllGroups(user).map((group, idx) => (
                <Chip
                  key={group}
                  label={idx === 0 ? `${group} (primary)` : group}
                  size="small"
                  variant={idx === 0 ? "filled" : "outlined"}
                  sx={{ mr: 1, mb: 1 }}
                />
              ))}
            </Box>
          </>
        )}
        emptyMessage="No users found."
      />

      <CreateUserDialog
        open={createDialogOpen}
        onClose={() => setCreateDialogOpen(false)}
      />

      <DeleteUserDialog
        open={deleteDialogOpen}
        onClose={() => setDeleteDialogOpen(false)}
        usernames={selectedUsers.map((u) => u.username)}
        onSuccess={handleDeleteSuccess}
      />

      {selectedUser && (
        <>
          <EditUserDialog
            open={editDialogOpen}
            onClose={() => {
              setEditDialogOpen(false);
              setSelectedUser(null);
            }}
            user={selectedUser}
          />
          <ChangePasswordDialog
            open={passwordDialogOpen}
            onClose={() => {
              setPasswordDialogOpen(false);
              setSelectedUser(null);
            }}
            username={selectedUser.username}
          />
        </>
      )}
    </Box>
  );
};

export default UsersTab;
