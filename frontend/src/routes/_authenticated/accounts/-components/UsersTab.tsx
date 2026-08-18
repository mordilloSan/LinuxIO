import { useSuspenseQuery } from "@tanstack/react-query";
import { getRouteApi } from "@tanstack/react-router";
import {
  useCallback,
  useEffect,
  useEffectEvent,
  useMemo,
  useState,
} from "react";

import { type AccountUser, linuxio, useCallMutation } from "@/api";
import type { UserLockAction } from "@/components/cards/UserCard";
import { RoutedTabSearch } from "@/components/tabbar";
import AppDataTable from "@/components/tables/AppDataTable";
import type { AppDataTableColumnDef } from "@/components/tables/AppDataTable.types";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import Chip from "@/components/ui/AppChip";
import AppHeaderSearch from "@/components/ui/AppHeaderSearch";
import AppTypography from "@/components/ui/AppTypography";
import useAuth from "@/hooks/useAuth";
import { useRegisterCreateHandler } from "@/hooks/useRegisterCreateHandler";
import { useReorderableSurface } from "@/hooks/useReorderableSurface";
import { useReorderableTableDnd } from "@/hooks/useReorderableTableDnd";
import { responsiveTextStyles } from "@/theme/tableStyles";

import ChangePasswordDialog from "./components/ChangePasswordDialog";
import CreateUserDialog from "./components/CreateUserDialog";
import EditUserDialog from "./components/EditUserDialog";
import UserCardsView from "./components/UserCardsView";

const ACCOUNTS_TOAST_META = {
  label: "Open accounts",
  to: "/accounts",
} as const;
const accountsRouteApi = getRouteApi("/_authenticated/accounts/");

interface UsersTabProps {
  onMountCreateHandler?: (handler: () => void) => void;
  setViewMode?: (next: "table" | "card") => void;
  viewMode?: "table" | "card";
}
const getAccountUserId = (user: AccountUser) => user.username;

// Format last login for display
const formatLastLogin = (
  lastLogin: string,
  username: string,
  currentUserName?: string,
): string => {
  if (!lastLogin || lastLogin === "Never") {
    return "Never logged in";
  }
  if (username === currentUserName) {
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

const UsersTab = ({
  onMountCreateHandler,
  viewMode = "table",
}: UsersTabProps) => {
  const { user: currentUser } = useAuth();
  const { data: users } = useSuspenseQuery({
    ...linuxio.accounts.list_users,
    refetchInterval: 10000,
  });
  const [search, setSearch] = useState("");
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [editDialogOpen, setEditDialogOpen] = useState(false);
  const [passwordDialogOpen, setPasswordDialogOpen] = useState(false);
  const [dialogUser, setDialogUser] = useState<AccountUser | null>(null);
  const [pendingLockActions, setPendingLockActions] = useState<
    Map<string, UserLockAction>
  >(() => new Map());
  const navigate = accountsRouteApi.useNavigate();
  const routeSearch = accountsRouteApi.useSearch();
  const selectedUsername =
    typeof routeSearch.user === "string" ? routeSearch.user : undefined;
  const usersList = Array.isArray(users) ? users : [];

  const setSelectedUsername = useCallback(
    (username: string | null) => {
      void navigate({
        to: "/accounts",
        search: (previous) => ({
          ...previous,
          user: username ?? undefined,
        }),
      });
    },
    [navigate],
  );

  const handleRowClick = useCallback(
    ({ original: user }: { original: AccountUser }) =>
      setSelectedUsername(user.username),
    [setSelectedUsername],
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
  useRegisterCreateHandler(onMountCreateHandler, handleCreateUser);
  const surface = useReorderableSurface({
    getId: getAccountUserId,
    items: usersList,
    surface: "accounts.users",
  });
  const tableDnd = useReorderableTableDnd<AccountUser, AccountUser>({
    handleAriaLabel: "Reorder user",
    surface,
  });
  const filtered = useMemo(
    () =>
      surface.items.filter(
        (user) =>
          user.username.toLowerCase().includes(search.toLowerCase()) ||
          user.gecos.toLowerCase().includes(search.toLowerCase()) ||
          user.primaryGroup.toLowerCase().includes(search.toLowerCase()),
      ),
    [search, surface.items],
  );
  const detailUser = selectedUsername
    ? (filtered.find((user) => user.username === selectedUsername) ?? null)
    : null;
  const handleEditUser = useCallback((user: AccountUser) => {
    setDialogUser(user);
    setEditDialogOpen(true);
  }, []);
  const handleChangePassword = useCallback((user: AccountUser) => {
    setDialogUser(user);
    setPasswordDialogOpen(true);
  }, []);
  const { mutateAsync: lockUser } = useCallMutation(
    linuxio.accounts.lock_user,
    {
      success: "User locked successfully",
      error: "Failed to lock user",
      toast: ACCOUNTS_TOAST_META,
    },
  );
  const { mutateAsync: unlockUser } = useCallMutation(
    linuxio.accounts.unlock_user,
    {
      success: "User unlocked successfully",
      error: "Failed to unlock user",
      toast: ACCOUNTS_TOAST_META,
    },
  );

  const handleToggleLock = useCallback(
    (user: AccountUser) => {
      if (user.username === "root" || user.username === currentUser?.name) {
        return;
      }
      if (pendingLockActions.has(user.username)) return;

      const action: UserLockAction = user.isLocked ? "unlock" : "lock";
      setPendingLockActions((current) => {
        const next = new Map(current);
        next.set(user.username, action);
        return next;
      });
      const operation =
        action === "unlock"
          ? unlockUser({ username: user.username })
          : lockUser({ username: user.username });
      void operation
        .finally(() => {
          setPendingLockActions((current) => {
            if (current.get(user.username) !== action) return current;
            const next = new Map(current);
            next.delete(user.username);
            return next;
          });
        })
        .catch(() => undefined);
    },
    [currentUser?.name, lockUser, pendingLockActions, unlockUser],
  );

  // Stable column defs: cells render through flexRender, so a rebuilt array
  // remounts every cell subtree — including on the press that arms a
  // reorder hold. See docs/table-row-gestures.md.
  const columns = useMemo<AppDataTableColumnDef<AccountUser>[]>(
    () => [
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
                user.username === currentUser?.name
                  ? "success"
                  : "text.secondary"
              }
              style={responsiveTextStyles}
              variant="body2"
            >
              {formatLastLogin(
                user.lastLogin,
                user.username,
                currentUser?.name,
              )}
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
          const pendingLockAction = pendingLockActions.get(user.username);
          const lockLabel = user.isLocked ? "Unlock" : "Lock";
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
                ariaLabel={
                  pendingLockAction
                    ? `${pendingLockAction === "lock" ? "Locking" : "Unlocking"} ${user.username}`
                    : `${lockLabel} ${user.username}`
                }
                disabled={
                  user.username === "root" ||
                  user.username === currentUser?.name ||
                  Boolean(pendingLockAction)
                }
                icon={user.isLocked ? "mdi:lock-open" : "mdi:lock"}
                iconSize={20}
                label={lockLabel}
                loading={Boolean(pendingLockAction)}
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
    ],
    [
      currentUser?.name,
      handleChangePassword,
      handleEditUser,
      handleToggleLock,
      pendingLockActions,
    ],
  );
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
        <RoutedTabSearch active={search !== ""}>
          <AppHeaderSearch
            clearOnDocumentEscape
            onChange={setSearch}
            placeholder="Search users…"
            value={search}
          />
        </RoutedTabSearch>
      )}
      {effectiveViewMode === "card" ? (
        <UserCardsView
          currentUsername={currentUser?.name}
          onChangePassword={handleChangePassword}
          onEdit={handleEditUser}
          onSelect={setSelectedUsername}
          onToggleLock={handleToggleLock}
          pendingLockActions={pendingLockActions}
          selectedUser={detailUser}
          surface={surface}
          users={filtered}
        />
      ) : (
        <AppDataTable
          ariaLabel="Users"
          columns={columns}
          data={filtered}
          dnd={tableDnd}
          emptyMessage="No users found."
          fillAvailable
          getRowId={getAccountUserId}
          // A row's one action is opening the user's card, which already shows
          // the home directory, shell and groups an inline panel would repeat.
          onRowClick={handleRowClick}
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
