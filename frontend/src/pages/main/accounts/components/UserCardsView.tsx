import { motion } from "framer-motion";
import React from "react";

import type { AccountUser } from "@/api";

import UserCard from "@/components/cards/UserCard";
import AppGrid from "@/components/ui/AppGrid";
import AppTypography from "@/components/ui/AppTypography";
import { useAppMediaQuery, useAppTheme } from "@/theme";

import {
  UserActivityCard,
  UserDetailsPanel,
  UserHomeSSHPanel,
  UserProcessPanel,
} from "./UserAccountDetails";

interface UserCardsViewProps {
  currentUsername?: string;
  isLocking: boolean;
  isUnlocking: boolean;
  onChangePassword: (user: AccountUser) => void;
  onEdit: (user: AccountUser) => void;
  onSelect: (username: string | null) => void;
  onToggleLock: (user: AccountUser) => void;
  selectedUser: AccountUser | null;
  users: AccountUser[];
}

const UserCardsView: React.FC<UserCardsViewProps> = ({
  users,
  selectedUser,
  currentUsername,
  isLocking,
  isUnlocking,
  onSelect,
  onEdit,
  onChangePassword,
  onToggleLock,
}) => {
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
      <AppGrid container spacing={2}>
        {users.map((user) => (
          <AppGrid key={user.username} size={{ xs: 12, sm: 6, md: 4, lg: 3 }}>
            <UserCard
              currentUsername={currentUsername}
              isLocking={isLocking}
              isUnlocking={isUnlocking}
              onChangePassword={() => onChangePassword(user)}
              onEdit={() => onEdit(user)}
              onOpen={() => onSelect(user.username)}
              onToggleLock={() => onToggleLock(user)}
              user={user}
            />
          </AppGrid>
        ))}
      </AppGrid>
    );
  }

  return (
    <AppGrid alignItems="stretch" container spacing={2.5}>
      <AppGrid size={{ xs: 12, lg: 4 }} style={{ display: "flex" }}>
        <UserCard
          currentUsername={currentUsername}
          isLocking={isLocking}
          isSelected
          isUnlocking={isUnlocking}
          onChangePassword={() => onChangePassword(selectedUser)}
          onEdit={() => onEdit(selectedUser)}
          onOpen={() => onSelect(null)}
          onToggleLock={() => onToggleLock(selectedUser)}
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
