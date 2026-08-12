import { motion } from "motion/react";

import type { AccountUser } from "@/api";
import UserCard from "@/components/cards/UserCard";
import type { UserLockAction } from "@/components/cards/UserCard";
import ReorderableCardGrid from "@/components/reorder/ReorderableCardGrid";
import AppGrid from "@/components/ui/AppGrid";
import AppTypography from "@/components/ui/AppTypography";
import type { ReorderableSurface } from "@/hooks/useReorderableSurface";
import { useAppMediaQuery, useAppTheme } from "@/theme";

import {
  UserActivityCard,
  UserDetailsPanel,
  UserHomeSSHPanel,
  UserProcessPanel,
} from "./UserAccountDetails";

interface UserCardsViewProps {
  currentUsername?: string;
  onChangePassword: (user: AccountUser) => void;
  onEdit: (user: AccountUser) => void;
  onSelect: (username: string | null) => void;
  onToggleLock: (user: AccountUser) => void;
  pendingLockActions: ReadonlyMap<string, UserLockAction>;
  selectedUser: AccountUser | null;
  /** Reorder wiring for the collapsed card grid. */
  surface: ReorderableSurface<AccountUser>;
  users: AccountUser[];
}

const getUsername = (user: AccountUser) => user.username;

// A press in layout mode belongs to the drag, not to opening the user.
const noopOpen = () => {};

const UserCardsView = ({
  surface,
  users,
  selectedUser,
  currentUsername,
  onSelect,
  onEdit,
  onChangePassword,
  onToggleLock,
  pendingLockActions,
}: UserCardsViewProps) => {
  const theme = useAppTheme();
  const isCompactLayout = useAppMediaQuery(theme.breakpoints.down("md"));
  const enterFromSide = {
    opacity: 0,
    x: isCompactLayout ? 0 : 40,
    y: isCompactLayout ? 20 : 0,
  };

  if (users.length === 0) {
    return (
      <div style={{ textAlign: "center", paddingBlock: 16 }}>
        <AppTypography color="text.secondary" variant="body2">
          No users found.
        </AppTypography>
      </div>
    );
  }

  if (!selectedUser) {
    return (
      <ReorderableCardGrid
        getId={getUsername}
        items={users}
        renderItem={(user) => (
          <UserCard
            currentUsername={currentUsername}
            onChangePassword={() => onChangePassword(user)}
            onEdit={() => onEdit(user)}
            onOpen={surface.editMode ? noopOpen : () => onSelect(user.username)}
            onToggleLock={() => onToggleLock(user)}
            pendingLockAction={pendingLockActions.get(user.username)}
            user={user}
          />
        )}
        size={{ xs: 12, sm: 6, md: 4, lg: 3 }}
        surface={surface}
      />
    );
  }

  return (
    <AppGrid alignItems="stretch" container spacing={2.5}>
      <AppGrid size={{ xs: 12, lg: 4 }} style={{ display: "flex" }}>
        <UserCard
          currentUsername={currentUsername}
          isSelected
          onChangePassword={() => onChangePassword(selectedUser)}
          onEdit={() => onEdit(selectedUser)}
          onOpen={() => onSelect(null)}
          onToggleLock={() => onToggleLock(selectedUser)}
          pendingLockAction={pendingLockActions.get(selectedUser.username)}
          user={selectedUser}
        />
      </AppGrid>
      <AppGrid
        animate={{ opacity: 1, x: 0, y: 0 }}
        component={motion.div}
        initial={enterFromSide}
        size={{ xs: 12, md: 6, lg: 4 }}
        style={{ display: "flex" }}
        transition={{ duration: 0.25, delay: 0.05 }}
      >
        <UserHomeSSHPanel username={selectedUser.username} />
      </AppGrid>
      <AppGrid
        animate={{ opacity: 1, x: 0, y: 0 }}
        component={motion.div}
        initial={enterFromSide}
        size={{ xs: 12, md: 6, lg: 4 }}
        style={{ display: "flex" }}
        transition={{ duration: 0.25, delay: 0.08 }}
      >
        <UserDetailsPanel
          currentUsername={currentUsername}
          onClose={() => onSelect(null)}
          user={selectedUser}
        />
      </AppGrid>
      <AppGrid
        animate={{ opacity: 1, y: 0 }}
        component={motion.div}
        initial={{ opacity: 0, y: 20 }}
        size={{ xs: 12, lg: 8 }}
        style={{ display: "flex" }}
        transition={{ duration: 0.25, delay: 0.1 }}
      >
        <UserActivityCard username={selectedUser.username} />
      </AppGrid>
      <AppGrid
        animate={{ opacity: 1, y: 0 }}
        component={motion.div}
        initial={{ opacity: 0, y: 20 }}
        size={{ xs: 12, lg: 4 }}
        style={{ display: "flex" }}
        transition={{ duration: 0.25, delay: 0.12 }}
      >
        <UserProcessPanel username={selectedUser.username} />
      </AppGrid>
    </AppGrid>
  );
};

export default UserCardsView;
