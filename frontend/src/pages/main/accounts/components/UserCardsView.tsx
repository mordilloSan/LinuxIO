import { motion } from "framer-motion";
import React from "react";

import {
  UserActivityCard,
  UserDetailsPanel,
  UserHomeSSHPanel,
  UserProcessPanel,
} from "./UserAccountDetails";

import type { AccountUser } from "@/api";
import UserCard from "@/components/cards/UserCard";
import AppGrid from "@/components/ui/AppGrid";
import AppTypography from "@/components/ui/AppTypography";
import { useAppMediaQuery, useAppTheme } from "@/theme";

interface UserCardsViewProps {
  users: AccountUser[];
  selectedUser: AccountUser | null;
  currentUsername?: string;
  isLocking: boolean;
  isUnlocking: boolean;
  onSelect: (username: string | null) => void;
  onEdit: (user: AccountUser) => void;
  onChangePassword: (user: AccountUser) => void;
  onToggleLock: (user: AccountUser) => void;
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
        <AppTypography variant="body2" color="text.secondary">
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
              user={user}
              currentUsername={currentUsername}
              isLocking={isLocking}
              isUnlocking={isUnlocking}
              onOpen={() => onSelect(user.username)}
              onEdit={() => onEdit(user)}
              onChangePassword={() => onChangePassword(user)}
              onToggleLock={() => onToggleLock(user)}
            />
          </AppGrid>
        ))}
      </AppGrid>
    );
  }

  return (
    <AppGrid container spacing={2.5} alignItems="stretch">
      <AppGrid size={{ xs: 12, lg: 4 }} style={{ display: "flex" }}>
        <UserCard
          user={selectedUser}
          currentUsername={currentUsername}
          isLocking={isLocking}
          isUnlocking={isUnlocking}
          isSelected
          onOpen={() => onSelect(null)}
          onEdit={() => onEdit(selectedUser)}
          onChangePassword={() => onChangePassword(selectedUser)}
          onToggleLock={() => onToggleLock(selectedUser)}
        />
      </AppGrid>
      <AppGrid
        component={motion.div}
        size={{ xs: 12, md: 6, lg: 4 }}
        style={{ display: "flex" }}
        initial={enterFromSide}
        animate={{ opacity: 1, x: 0, y: 0 }}
        transition={{ duration: 0.25, delay: 0.05 }}
      >
        <UserHomeSSHPanel username={selectedUser.username} />
      </AppGrid>
      <AppGrid
        component={motion.div}
        size={{ xs: 12, md: 6, lg: 4 }}
        style={{ display: "flex" }}
        initial={enterFromSide}
        animate={{ opacity: 1, x: 0, y: 0 }}
        transition={{ duration: 0.25, delay: 0.08 }}
      >
        <UserDetailsPanel
          user={selectedUser}
          currentUsername={currentUsername}
          onClose={() => onSelect(null)}
        />
      </AppGrid>
      <AppGrid
        component={motion.div}
        size={{ xs: 12, lg: 8 }}
        initial={{ opacity: 0, y: 20 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.25, delay: 0.1 }}
      >
        <UserActivityCard username={selectedUser.username} />
      </AppGrid>
      <AppGrid
        component={motion.div}
        size={{ xs: 12, lg: 4 }}
        initial={{ opacity: 0, y: 20 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.25, delay: 0.12 }}
      >
        <UserProcessPanel username={selectedUser.username} />
      </AppGrid>
    </AppGrid>
  );
};

export default UserCardsView;
