import { useQueryClient } from "@tanstack/react-query";
import React, { useCallback, useEffect, useEffectEvent, useState } from "react";
import { useSearchParams } from "react-router-dom";

import ChangePasswordDialog from "./components/ChangePasswordDialog";
import CreateUserDialog from "./components/CreateUserDialog";
import EditUserDialog from "./components/EditUserDialog";
import UserCardsView from "./components/UserCardsView";

import { type AccountUser, linuxio } from "@/api";
import AppDataTable from "@/components/tables/AppDataTable";
import type { AppDataTableColumnDef } from "@/components/tables/AppDataTable";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import Chip from "@/components/ui/AppChip";
import AppSearchField from "@/components/ui/AppSearchField";
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
      unlockUser({ username: user.username });
    } else {
      lockUser({ username: user.username });
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
  const columns: AppDataTableColumnDef<AccountUser>[] = [
    {
      accessorKey: "username",
      header: "Username",
      cell: ({ row }) => {
        const user = row.original;
        return (
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
        );
      },
      meta: { align: "left" },
    },
    {
      accessorKey: "gecos",
      header: "Full Name",
      cell: ({ row }) => (
        <AppTypography style={responsiveTextStyles} variant="body2">
          {row.original.gecos || "-"}
        </AppTypography>
      ),
      meta: {
        align: "left",
        hideBelow: "sm",
      },
    },
    {
      accessorKey: "uid",
      header: "ID",
      cell: ({ row }) => (
        <AppTypography style={responsiveTextStyles} variant="body2">
          {row.original.uid}
        </AppTypography>
      ),
      meta: {
        align: "left",
        hideBelow: "md",
        width: "80px",
      },
    },
    {
      accessorKey: "lastLogin",
      header: "Last Active",
      cell: ({ row }) => {
        const user = row.original;
        return (
          <AppTypography
            color={
              user.username === currentUser?.name ? "success" : "text.secondary"
            }
            style={responsiveTextStyles}
            variant="body2"
          >
            {formatLastLogin(user.lastLogin, user.username)}
          </AppTypography>
        );
      },
      meta: {
        align: "left",
        hideBelow: "lg",
      },
    },
    {
      accessorFn: (user) => getAllGroups(user).length,
      id: "groups",
      header: "Groups",
      cell: ({ row }) => {
        const user = row.original;
        const groups = getAllGroups(user);
        return (
          <div
            style={{
              display: "flex",
              flexWrap: "wrap",
              gap: 2,
            }}
          >
            {groups.slice(0, 3).map((group, idx) => (
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
            {groups.length > 3 && (
              <Chip
                label={`+${groups.length - 3}`}
                size="small"
                style={{
                  fontSize: "0.65rem",
                  height: 20,
                }}
                variant="soft"
              />
            )}
          </div>
        );
      },
      meta: {
        align: "left",
        hideBelow: "xl",
      },
    },
    {
      id: "actions",
      header: "Actions",
      enableSorting: false,
      cell: ({ row }) => {
        const user = row.original;
        return (
          <div
            style={{
              display: "flex",
              justifyContent: "flex-end",
              gap: 2,
            }}
          >
            <AppActionIconButton
              disabled={user.username === "root"}
              icon="mdi:pencil"
              iconSize={20}
              label="Edit"
              onClick={(e) => {
                e.stopPropagation();
                handleEditUser(user);
              }}
            />
            <AppActionIconButton
              icon="mdi:form-textbox-password"
              iconSize={20}
              label="Change Password"
              onClick={(e) => {
                e.stopPropagation();
                handleChangePassword(user);
              }}
            />
            <AppActionIconButton
              disabled={
                user.username === "root" ||
                user.username === currentUser?.name ||
                isLocking ||
                isUnlocking
              }
              icon={user.isLocked ? "mdi:lock-open" : "mdi:lock"}
              iconSize={20}
              label={user.isLocked ? "Unlock" : "Lock"}
              onClick={(e) => {
                e.stopPropagation();
                handleToggleLock(user);
              }}
            />
          </div>
        );
      },
      meta: {
        align: "right",
        width: "150px",
      },
    },
  ];
  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        height: "100%",
        minHeight: 0,
      }}
    >
      {!detailUser && (
        <div
          style={{
            display: "flex",
            flexShrink: 0,
            flexWrap: "wrap",
            alignItems: "center",
            gap: 8,
            marginBottom: 8,
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
        <AppDataTable
          ariaLabel="Users"
          columns={columns}
          data={filtered}
          emptyMessage="No users found."
          fillAvailable
          getRowId={(user) => user.username}
          onRowClick={({ original: user }) =>
            setSelectedUsername(user.username)
          }
          renderExpandedContent={({ original: user }) => (
            <div className="expand-panel">
              <div>
                <AppTypography gutterBottom variant="subtitle2">
                  <b>Home Directory:</b>
                </AppTypography>
                <AppTypography className="expand-panel__mono" variant="body2">
                  {user.homeDir}
                </AppTypography>
              </div>

              <div>
                <AppTypography gutterBottom variant="subtitle2">
                  <b>Shell:</b>
                </AppTypography>
                <AppTypography className="expand-panel__mono" variant="body2">
                  {user.shell}
                </AppTypography>
              </div>

              <div>
                <AppTypography gutterBottom variant="subtitle2">
                  <b>All Groups:</b>
                </AppTypography>
                <div className="expand-panel__chips">
                  {getAllGroups(user).map((group, idx) => (
                    <Chip
                      key={group}
                      label={idx === 0 ? `${group} (primary)` : group}
                      size="small"
                      variant="soft"
                    />
                  ))}
                </div>
              </div>
            </div>
          )}
          selectedRowId={selectedUsername}
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
