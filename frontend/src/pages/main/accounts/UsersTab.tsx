import { Icon } from "@iconify/react";
import { useQueryClient } from "@tanstack/react-query";
import React, { useCallback, useEffect, useEffectEvent, useState } from "react";
import { useSearchParams } from "react-router-dom";

import ChangePasswordDialog from "./components/ChangePasswordDialog";
import CreateUserDialog from "./components/CreateUserDialog";
import EditUserDialog from "./components/EditUserDialog";
import UserCardsView from "./components/UserCardsView";

import { type AccountUser, linuxio } from "@/api";
import UnifiedCollapsibleTable, {
  UnifiedTableColumn,
} from "@/components/tables/UnifiedCollapsibleTable";
import Chip from "@/components/ui/AppChip";
import AppIconButton from "@/components/ui/AppIconButton";
import AppSearchField from "@/components/ui/AppSearchField";
import { AppTableCell } from "@/components/ui/AppTable";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import useAuth from "@/hooks/useAuth";
import { useScopedToast } from "@/hooks/useScopedToast";
import { responsiveTextStyles } from "@/theme/tableStyles";
import { getMutationErrorMessage } from "@/utils/mutations";

interface UsersTabProps {
  onMountCreateHandler?: (handler: () => void) => void;
  setViewMode?: (next: "table" | "card") => void;
  viewMode?: "table" | "card";
}
const UsersTab: React.FC<UsersTabProps> = ({
  onMountCreateHandler,
  viewMode = "table",
}) => {
  const toast = useScopedToast({ href: "/accounts", label: "Open accounts" });
  const queryClient = useQueryClient();
  const { user: currentUser } = useAuth();
  const { data: users = [] } = linuxio.accounts.list_users.useQuery({
    refetchInterval: 10000,
  });
  const [search, setSearch] = useState("");
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [editDialogOpen, setEditDialogOpen] = useState(false);
  const [passwordDialogOpen, setPasswordDialogOpen] = useState(false);
  const [dialogUser, setDialogUser] = useState<AccountUser | null>(null);
  const [searchParams, setSearchParams] = useSearchParams();
  const selectedUsername = searchParams.get("user");
  const usersList = Array.isArray(users) ? users : [];

  const setSelectedUsername = useCallback(
    (username: string | null) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (username === null) {
            next.delete("user");
          } else {
            next.set("user", username);
          }
          return next;
        },
        { replace: false },
      );
    },
    [setSearchParams],
  );

  const effectiveViewMode = selectedUsername ? "card" : viewMode;

  const handleEscapeKey = useEffectEvent((event: KeyboardEvent) => {
    if (event.key === "Escape") {
      setSelectedUsername(null);
    }
  });

  useEffect(() => {
    window.addEventListener("keydown", handleEscapeKey);
    return () => window.removeEventListener("keydown", handleEscapeKey);
  }, []);

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
  const detailUser = selectedUsername
    ? (filtered.find((user) => user.username === selectedUsername) ?? null)
    : null;
  const handleEditUser = (user: AccountUser) => {
    setDialogUser(user);
    setEditDialogOpen(true);
  };
  const handleChangePassword = (user: AccountUser) => {
    setDialogUser(user);
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
    {
      field: "username",
      headerName: "Username",
      align: "left",
    },
    {
      field: "gecos",
      headerName: "Full Name",
      align: "left",
      className: "app-table-hide-below-sm",
    },
    {
      field: "uid",
      headerName: "ID",
      align: "left",
      width: "80px",
      className: "app-table-hide-below-md",
    },
    {
      field: "lastLogin",
      headerName: "Last Active",
      align: "left",
      className: "app-table-hide-below-lg",
    },
    {
      field: "groups",
      headerName: "Groups",
      align: "left",
      className: "app-table-hide-below-xl",
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
      {!detailUser && (
        <div
          style={{
            marginBottom: 8,
            display: "flex",
            alignItems: "center",
            gap: 8,
            flexWrap: "wrap",
          }}
        >
          <AppSearchField
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search users…"
            style={{ width: 320 }}
            value={search}
          />
          <span
            style={{
              fontWeight: "bold",
            }}
          >
            {filtered.length} shown
          </span>
        </div>
      )}
      {effectiveViewMode === "card" ? (
        <UserCardsView
          currentUsername={currentUser?.name}
          isLocking={isLocking}
          isUnlocking={isUnlocking}
          onChangePassword={handleChangePassword}
          onEdit={handleEditUser}
          onSelect={setSelectedUsername}
          onToggleLock={handleToggleLock}
          selectedUser={detailUser}
          users={filtered}
        />
      ) : (
        <UnifiedCollapsibleTable
          columns={columns}
          data={filtered}
          emptyMessage="No users found."
          getRowKey={(user) => user.username}
          onRowClick={(user) => setSelectedUsername(user.username)}
          renderExpandedContent={(user) => (
            <>
              <AppTypography gutterBottom variant="subtitle2">
                <b>Home Directory:</b>
              </AppTypography>
              <AppTypography
                style={{
                  fontFamily: "monospace",
                  fontSize: "0.85rem",
                  marginBottom: 8,
                }}
                variant="body2"
              >
                {user.homeDir}
              </AppTypography>

              <AppTypography gutterBottom variant="subtitle2">
                <b>Shell:</b>
              </AppTypography>
              <AppTypography
                style={{
                  marginBottom: 8,
                  fontSize: "0.85rem",
                }}
                variant="body2"
              >
                {user.shell}
              </AppTypography>

              <AppTypography gutterBottom variant="subtitle2">
                <b>All Groups:</b>
              </AppTypography>
              <div
                style={{
                  marginBottom: 8,
                  display: "flex",
                  flexWrap: "wrap",
                }}
              >
                {getAllGroups(user).map((group, idx) => (
                  <Chip
                    key={group}
                    label={idx === 0 ? `${group} (primary)` : group}
                    size="small"
                    style={{
                      marginRight: 4,
                      marginBottom: 4,
                    }}
                    variant="soft"
                  />
                ))}
              </div>
            </>
          )}
          renderMainRow={(user) => (
            <>
              <AppTableCell>
                <div
                  style={{
                    display: "flex",
                    alignItems: "center",
                    gap: 4,
                    flexWrap: "wrap",
                  }}
                >
                  <AppTypography
                    fontWeight={500}
                    style={responsiveTextStyles}
                    variant="body2"
                  >
                    {user.username}
                  </AppTypography>
                  {user.username === currentUser?.name && (
                    <Chip
                      color="primary"
                      label="Your account"
                      size="small"
                      style={{
                        fontSize: "0.65rem",
                        height: 20,
                      }}
                      variant="soft"
                    />
                  )}
                  {user.isLocked && (
                    <Chip
                      color="warning"
                      label="locked"
                      size="small"
                      style={{
                        fontSize: "0.65rem",
                        height: 20,
                      }}
                      variant="soft"
                    />
                  )}
                </div>
              </AppTableCell>
              <AppTableCell className="app-table-hide-below-sm">
                <AppTypography style={responsiveTextStyles} variant="body2">
                  {user.gecos || "-"}
                </AppTypography>
              </AppTableCell>
              <AppTableCell className="app-table-hide-below-md">
                <AppTypography style={responsiveTextStyles} variant="body2">
                  {user.uid}
                </AppTypography>
              </AppTableCell>
              <AppTableCell className="app-table-hide-below-lg">
                <AppTypography
                  color={
                    user.username === currentUser?.name
                      ? "success"
                      : "text.secondary"
                  }
                  style={responsiveTextStyles}
                  variant="body2"
                >
                  {formatLastLogin(user.lastLogin, user.username)}
                </AppTypography>
              </AppTableCell>
              <AppTableCell className="app-table-hide-below-xl">
                <div
                  style={{
                    display: "flex",
                    flexWrap: "wrap",
                    gap: 2,
                  }}
                >
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
                        style={{
                          fontSize: "0.65rem",
                          height: 20,
                        }}
                        variant="soft"
                      />
                    ))}
                  {getAllGroups(user).length > 3 && (
                    <Chip
                      label={`+${getAllGroups(user).length - 3}`}
                      size="small"
                      style={{
                        fontSize: "0.65rem",
                        height: 20,
                      }}
                      variant="soft"
                    />
                  )}
                </div>
              </AppTableCell>
              <AppTableCell align="right">
                <div
                  style={{
                    display: "flex",
                    justifyContent: "flex-end",
                    gap: 2,
                  }}
                >
                  <AppTooltip title="Edit">
                    <AppIconButton
                      disabled={user.username === "root"}
                      onClick={(e) => {
                        e.stopPropagation();
                        handleEditUser(user);
                      }}
                      size="small"
                    >
                      <Icon height={20} icon="mdi:pencil" width={20} />
                    </AppIconButton>
                  </AppTooltip>
                  <AppTooltip title="Change Password">
                    <AppIconButton
                      onClick={(e) => {
                        e.stopPropagation();
                        handleChangePassword(user);
                      }}
                      size="small"
                    >
                      <Icon
                        height={20}
                        icon="mdi:form-textbox-password"
                        width={20}
                      />
                    </AppIconButton>
                  </AppTooltip>
                  <AppTooltip title={user.isLocked ? "Unlock" : "Lock"}>
                    <AppIconButton
                      disabled={
                        user.username === "root" ||
                        user.username === currentUser?.name ||
                        isLocking ||
                        isUnlocking
                      }
                      onClick={(e) => {
                        e.stopPropagation();
                        handleToggleLock(user);
                      }}
                      size="small"
                    >
                      {user.isLocked ? (
                        <Icon height={20} icon="mdi:lock-open" width={20} />
                      ) : (
                        <Icon height={20} icon="mdi:lock" width={20} />
                      )}
                    </AppIconButton>
                  </AppTooltip>
                </div>
              </AppTableCell>
            </>
          )}
          selectedKey={selectedUsername}
        />
      )}

      <CreateUserDialog
        onClose={() => setCreateDialogOpen(false)}
        open={createDialogOpen}
      />

      {dialogUser && (
        <>
          <EditUserDialog
            onClose={() => {
              setEditDialogOpen(false);
              setDialogUser(null);
            }}
            open={editDialogOpen}
            user={dialogUser}
          />
          <ChangePasswordDialog
            onClose={() => {
              setPasswordDialogOpen(false);
              setDialogUser(null);
            }}
            open={passwordDialogOpen}
            username={dialogUser.username}
          />
        </>
      )}
    </div>
  );
};
export default UsersTab;
