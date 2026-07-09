import type { Update } from "@/api";

import UpdateActions from "./UpdateActions";
import UpdateList from "./UpdateList";

interface UpdateStatusProps {
  error?: string | null;
  eventLog?: string[];
  isLoading: boolean;
  onCancel?: () => void;
  onClearError?: () => void;
  onUpdateOne: (pkg: string) => Promise<void>;
  progress: number;
  status?: string | null;
  updates: Update[];
  updatingPackage: string | null;
}

const UpdateStatus = ({
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
}: UpdateStatusProps) => {
  return (
    <div>
      <UpdateActions
        currentPackage={updatingPackage}
        error={error}
        eventLog={eventLog}
        isUpdating={!!updatingPackage}
        onCancel={onCancel}
        onClearError={onClearError}
        progress={progress}
        status={status}
      />

      <UpdateList
        currentPackage={updatingPackage}
        isLoading={isLoading}
        isUpdating={!!updatingPackage || isLoading}
        onUpdateClick={onUpdateOne}
        updates={updates}
      />
    </div>
  );
};

export default UpdateStatus;
