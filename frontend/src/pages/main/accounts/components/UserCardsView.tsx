import { motion } from "framer-motion";
import React from "react";

import {
  UserDetailsPanel,
  UserLoginHistoryCard,
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
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: theme.spacing(3),
      }}
    >
      <div
        style={{
          display: "flex",
          flexDirection: isCompactLayout ? "column" : "row",
          alignItems: "stretch",
          gap: theme.spacing(2.5),
        }}
      >
        <div
          style={{
            flex: isCompactLayout ? "0 0 auto" : 1,
            width: isCompactLayout ? "100%" : undefined,
            display: "flex",
          }}
        >
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
        </div>
        <motion.div
          style={{
            width: isCompactLayout ? "100%" : "33.33%",
            flexShrink: 0,
            display: "flex",
          }}
          initial={{
            opacity: 0,
            x: isCompactLayout ? 0 : 40,
            y: isCompactLayout ? 20 : 0,
          }}
          animate={{ opacity: 1, x: 0, y: 0 }}
          transition={{ duration: 0.25, delay: 0.05 }}
        >
          <UserDetailsPanel
            user={selectedUser}
            currentUsername={currentUsername}
            onClose={() => onSelect(null)}
          />
        </motion.div>
      </div>
      <motion.div
        initial={{ opacity: 0, y: 20 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.25, delay: 0.1 }}
      >
        <UserLoginHistoryCard username={selectedUser.username} />
      </motion.div>
    </div>
  );
};

export default UserCardsView;
