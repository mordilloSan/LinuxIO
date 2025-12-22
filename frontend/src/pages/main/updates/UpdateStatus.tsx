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
  status?: string | null;
  error?: string | null;
  onClearError?: () => void;
  onCancel?: () => void;
  onComplete: () => void | Promise<unknown>;
}

const UpdateStatus: React.FC<UpdateStatusProps> = ({
  updates,
  isLoading,
  onUpdateOne,
  updatingPackage,
  progress,
  status,
  error,
  onClearError,
  onCancel,
  onComplete,
}) => {
  return (
    <Box>
      <UpdateActions
        isUpdating={!!updatingPackage}
        currentPackage={updatingPackage}
        progress={progress}
        status={status}
        error={error}
        onClearError={onClearError}
        onCancel={onCancel}
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
