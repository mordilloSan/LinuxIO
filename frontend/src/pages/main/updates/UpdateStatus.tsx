import { Box } from "@mui/material";
import React from "react";

import UpdateActions from "./UpdateActions";
import UpdateList from "./UpdateList";

import { Update } from "@/types/update";

interface UpdateStatusProps {
  updates: Update[];
  isLoading: boolean;
  onUpdateOne: (pkg: string) => Promise<void>;
  updatingPackage: string | null;
  progress: number;
  error?: string | null;
  onClearError?: () => void;
  onComplete: () => void | Promise<any>;
}

const UpdateStatus: React.FC<UpdateStatusProps> = ({
  updates,
  isLoading,
  onUpdateOne,
  updatingPackage,
  progress,
  error,
  onClearError,
  onComplete,
}) => {
  return (
    <Box>
      <UpdateActions
        isUpdating={!!updatingPackage}
        currentPackage={updatingPackage}
        progress={progress}
        error={error}
        onClearError={onClearError}
      />

      <UpdateList
        updates={updates}
        onUpdateClick={onUpdateOne}
        isUpdating={!!updatingPackage || isLoading}
        currentPackage={updatingPackage}
        onComplete={onComplete}
        isLoading={isLoading}
      />
    </Box>
  );
};

export default UpdateStatus;
