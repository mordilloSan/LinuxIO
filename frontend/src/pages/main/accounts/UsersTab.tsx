import { Icon } from "@iconify/react";
import {
  Grid,
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
import FrostedCard from "@/components/cards/RootCard";
import UnifiedCollapsibleTable, {
  UnifiedTableColumn,
} from "@/components/tables/UnifiedCollapsibleTable";
import useAuth from "@/hooks/useAuth";
import { responsiveTextStyles } from "@/theme/tableStyles";
import { getMutationErrorMessage } from "@/utils/mutations";

interface UsersTabProps {
  onMountCreateHandler?: (handler: () => void) => void;
  viewMode?: "table" | "card";
}

const UsersTab: React.FC<UsersTabProps> = ({
  onMountCreateHandler,
  viewMode = "table",
}) => {
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
    <div>
      <div
        style={{
          marginBottom: 8,
          display: "flex",
          alignItems: "center",
          gap: 8,
          flexWrap: "wrap",
        }}
      >
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
        <span style={{ fontWeight: "bold" }}>{filtered.length} shown</span>
        {effectiveSelected.size > 0 && (
          <Button
            variant="contained"
            color="error"
            size="small"
            startIcon={<Icon icon="mdi:delete" width={20} height={20} />}
            onClick={() => setDeleteDialogOpen(true)}
          >
            Delete ({effectiveSelected.size})
          </Button>
        )}
      </div>
      {viewMode === "card" ? (
        filtered.length > 0 ? (
          <Grid container spacing={2}>
            {filtered.map((user) => (
              <Grid key={user.username} size={{ xs: 12, sm: 6, md: 4, lg: 3 }}>
                <FrostedCard style={{ padding: 8 }}>
                  <div
                    style={{
                      display: "flex",
                      justifyContent: "space-between",
                      alignItems: "center",
                      gap: 4,
                      marginBottom: 4,
                    }}
                  >
                    <div
                      style={{ display: "flex", alignItems: "center", gap: 4 }}
                    >
                      <Checkbox
                        size="small"
                        checked={effectiveSelected.has(user.username)}
                        onChange={(e) =>
                          handleSelectOne(user.username, e.target.checked)
                        }
                        disabled={
                          user.username === "root" ||
                          user.username === currentUser?.name
                        }
                      />
                      <Typography variant="body2" fontWeight="bold" noWrap>
                        {user.username}
                      </Typography>
                    </div>
                    <div style={{ display: "flex", gap: 2 }}>
                      <Tooltip title="Edit">
                        <IconButton
                          size="small"
                          onClick={() => handleEditUser(user)}
                          disabled={user.username === "root"}
                        >
                          <Icon icon="mdi:pencil" width={20} height={20} />
                        </IconButton>
                      </Tooltip>
                      <Tooltip title="Change Password">
                        <IconButton
                          size="small"
                          onClick={() => handleChangePassword(user)}
                        >
                          <Icon
                            icon="mdi:form-textbox-password"
                            width={20}
                            height={20}
                          />
                        </IconButton>
                      </Tooltip>
                      <Tooltip title={user.isLocked ? "Unlock" : "Lock"}>
                        <IconButton
                          size="small"
                          onClick={() => handleToggleLock(user)}
                          disabled={
                            user.username === "root" ||
                            user.username === currentUser?.name ||
                            isLocking ||
                            isUnlocking
                          }
                        >
                          {user.isLocked ? (
                            <Icon icon="mdi:lock-open" width={20} height={20} />
                          ) : (
                            <Icon icon="mdi:lock" width={20} height={20} />
                          )}
                        </IconButton>
                      </Tooltip>
                    </div>
                  </div>

                  <div
                    style={{
                      display: "flex",
                      flexWrap: "wrap",
                      gap: 3,
                      marginBottom: 4,
                    }}
                  >
                    {user.username === currentUser?.name && (
                      <Chip label="Your account" size="small" color="primary" />
                    )}
                    {user.isLocked && (
                      <Chip label="Locked" size="small" color="warning" />
                    )}
                  </div>

                  <Typography variant="body2" sx={responsiveTextStyles}>
                    Full name: {user.gecos || "-"}
                  </Typography>
                  <Typography variant="body2" sx={responsiveTextStyles}>
                    UID: {user.uid}
                  </Typography>
                  <Typography variant="body2" sx={responsiveTextStyles}>
                    Last active:{" "}
                    {formatLastLogin(user.lastLogin, user.username)}
                  </Typography>
                  <Typography variant="body2" sx={responsiveTextStyles}>
                    Shell: {user.shell}
                  </Typography>
                  <Typography
                    variant="body2"
                    sx={{ fontFamily: "monospace", ...responsiveTextStyles }}
                  >
                    Home: {user.homeDir}
                  </Typography>

                  <div
                    style={{
                      marginTop: 4,
                      display: "flex",
                      flexWrap: "wrap",
                      gap: 2,
                    }}
                  >
                    {getAllGroups(user).map((group, idx) => (
                      <Chip
                        key={`${user.username}-${group}`}
                        label={idx === 0 ? `${group} (primary)` : group}
                        size="small"
                        variant={idx === 0 ? "filled" : "outlined"}
                        sx={{ fontSize: "0.7rem" }}
                      />
                    ))}
                  </div>
                </FrostedCard>
              </Grid>
            ))}
          </Grid>
        ) : (
          <div style={{ textAlign: "center", paddingBlock: 16 }}>
            <Typography variant="body2" color="text.secondary">
              No users found.
            </Typography>
          </div>
        )
      ) : (
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
                <div
                  style={{
                    display: "flex",
                    alignItems: "center",
                    gap: 4,
                    flexWrap: "wrap",
                  }}
                >
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
                </div>
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
                <div style={{ display: "flex", flexWrap: "wrap", gap: 2 }}>
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
                </div>
              </TableCell>
              <TableCell align="right">
                <div
                  style={{
                    display: "flex",
                    justifyContent: "flex-end",
                    gap: 2,
                  }}
                >
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
                </div>
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
              <div
                style={{ marginBottom: 8, display: "flex", flexWrap: "wrap" }}
              >
                {getAllGroups(user).map((group, idx) => (
                  <Chip
                    key={group}
                    label={idx === 0 ? `${group} (primary)` : group}
                    size="small"
                    variant={idx === 0 ? "filled" : "outlined"}
                    sx={{ mr: 1, mb: 1 }}
                  />
                ))}
              </div>
            </>
          )}
          emptyMessage="No users found."
        />
      )}

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
    </div>
  );
};

export default UsersTab;
