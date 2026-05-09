import { Icon } from "@iconify/react";
import { useQueryClient } from "@tanstack/react-query";
import React, { useCallback, useEffect, useEffectEvent, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { toast } from "sonner";

import ChangePasswordDialog from "./components/ChangePasswordDialog";
import CreateUserDialog from "./components/CreateUserDialog";
import EditUserDialog from "./components/EditUserDialog";
import UserCardsView from "./components/UserCardsView";

import { linuxio, type AccountUser } from "@/api";
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
import { responsiveTextStyles } from "@/theme/tableStyles";
import { getMutationErrorMessage } from "@/utils/mutations";
interface UsersTabProps {
  onMountCreateHandler?: (handler: () => void) => void;
  viewMode?: "table" | "card";
  setViewMode?: (next: "table" | "card") => void;
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
            placeholder="Search users…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            style={{ width: 320 }}
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
          users={filtered}
          selectedUser={detailUser}
          currentUsername={currentUser?.name}
          isLocking={isLocking}
          isUnlocking={isUnlocking}
          onSelect={setSelectedUsername}
          onEdit={handleEditUser}
          onChangePassword={handleChangePassword}
          onToggleLock={handleToggleLock}
        />
      ) : (
        <UnifiedCollapsibleTable
          data={filtered}
          columns={columns}
          getRowKey={(user) => user.username}
          selectedKey={selectedUsername}
          onRowClick={(user) => setSelectedUsername(user.username)}
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
                    variant="body2"
                    fontWeight={500}
                    style={responsiveTextStyles}
                  >
                    {user.username}
                  </AppTypography>
                  {user.username === currentUser?.name && (
                    <Chip
                      label="Your account"
                      size="small"
                      color="primary"
                      variant="soft"
                      style={{
                        fontSize: "0.65rem",
                        height: 20,
                      }}
                    />
                  )}
                  {user.isLocked && (
                    <Chip
                      label="locked"
                      size="small"
                      color="warning"
                      variant="soft"
                      style={{
                        fontSize: "0.65rem",
                        height: 20,
                      }}
                    />
                  )}
                </div>
              </AppTableCell>
              <AppTableCell className="app-table-hide-below-sm">
                <AppTypography variant="body2" style={responsiveTextStyles}>
                  {user.gecos || "-"}
                </AppTypography>
              </AppTableCell>
              <AppTableCell className="app-table-hide-below-md">
                <AppTypography variant="body2" style={responsiveTextStyles}>
                  {user.uid}
                </AppTypography>
              </AppTableCell>
              <AppTableCell className="app-table-hide-below-lg">
                <AppTypography
                  variant="body2"
                  color={
                    user.username === currentUser?.name
                      ? "success"
                      : "text.secondary"
                  }
                  style={responsiveTextStyles}
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
                        variant="soft"
                        style={{
                          fontSize: "0.65rem",
                          height: 20,
                        }}
                      />
                    ))}
                  {getAllGroups(user).length > 3 && (
                    <Chip
                      label={`+${getAllGroups(user).length - 3}`}
                      size="small"
                      variant="soft"
                      style={{
                        fontSize: "0.65rem",
                        height: 20,
                      }}
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
                      size="small"
                      onClick={(e) => {
                        e.stopPropagation();
                        handleEditUser(user);
                      }}
                      disabled={user.username === "root"}
                    >
                      <Icon icon="mdi:pencil" width={20} height={20} />
                    </AppIconButton>
                  </AppTooltip>
                  <AppTooltip title="Change Password">
                    <AppIconButton
                      size="small"
                      onClick={(e) => {
                        e.stopPropagation();
                        handleChangePassword(user);
                      }}
                    >
                      <Icon
                        icon="mdi:form-textbox-password"
                        width={20}
                        height={20}
                      />
                    </AppIconButton>
                  </AppTooltip>
                  <AppTooltip title={user.isLocked ? "Unlock" : "Lock"}>
                    <AppIconButton
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
                        <Icon icon="mdi:lock-open" width={20} height={20} />
                      ) : (
                        <Icon icon="mdi:lock" width={20} height={20} />
                      )}
                    </AppIconButton>
                  </AppTooltip>
                </div>
              </AppTableCell>
            </>
          )}
          renderExpandedContent={(user) => (
            <>
              <AppTypography variant="subtitle2" gutterBottom>
                <b>Home Directory:</b>
              </AppTypography>
              <AppTypography
                variant="body2"
                style={{
                  fontFamily: "monospace",
                  fontSize: "0.85rem",
                  marginBottom: 8,
                }}
              >
                {user.homeDir}
              </AppTypography>

              <AppTypography variant="subtitle2" gutterBottom>
                <b>Shell:</b>
              </AppTypography>
              <AppTypography
                variant="body2"
                style={{
                  marginBottom: 8,
                  fontSize: "0.85rem",
                }}
              >
                {user.shell}
              </AppTypography>

              <AppTypography variant="subtitle2" gutterBottom>
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
                    variant="soft"
                    style={{
                      marginRight: 4,
                      marginBottom: 4,
                    }}
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

      {dialogUser && (
        <>
          <EditUserDialog
            open={editDialogOpen}
            onClose={() => {
              setEditDialogOpen(false);
              setDialogUser(null);
            }}
            user={dialogUser}
          />
          <ChangePasswordDialog
            open={passwordDialogOpen}
            onClose={() => {
              setPasswordDialogOpen(false);
              setDialogUser(null);
            }}
            username={dialogUser.username}
          />
        </>
      )}
    </div>
  );
};
export default UsersTab;
