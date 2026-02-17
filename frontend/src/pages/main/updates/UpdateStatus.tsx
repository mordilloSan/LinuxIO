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
  eventLog?: string[];
  error?: string | null;
  onClearError?: () => void;
  onCancel?: () => void;
}

const UpdateStatus: React.FC<UpdateStatusProps> = ({
  updates,
  isLoading,
  onUpdateOne,
  updatingPackage,
  progress,
  status,
  eventLog,
  error,
  onClearError,
  onCancel,
}) => {
  return (
    <Box>
      <UpdateActions
        isUpdating={!!updatingPackage}
        currentPackage={updatingPackage}
        progress={progress}
        status={status}
        eventLog={eventLog}
        error={error}
        onClearError={onClearError}
        onCancel={onCancel}
      />

      <UpdateList
        updates={updates}
        onUpdateClick={onUpdateOne}
        isUpdating={!!updatingPackage || isLoading}
        currentPackage={updatingPackage}
        isLoading={isLoading}
      />
    </Box>
  );
};

export default UpdateStatus;
