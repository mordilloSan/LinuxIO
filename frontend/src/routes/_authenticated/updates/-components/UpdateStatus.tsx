import type { Update } from "@/api";

import UpdateActions from "./UpdateActions";
import UpdateList from "./UpdateList";

interface UpdateStatusProps {
  error?: string | null;
  eventLog?: string[];
  onCancel?: () => void;
  onClearError?: () => void;
  onUpdateOne: (pkg: string) => Promise<void>;
  progress: number;
  recoveryPending?: boolean;
  status?: string | null;
  updates: Update[];
  updatingPackage: string | null;
}

const UpdateStatus = ({
  updates,
  onUpdateOne,
  updatingPackage,
  progress,
  status,
  eventLog,
  error,
  onClearError,
  onCancel,
  recoveryPending = false,
}: UpdateStatusProps) => {
  return (
    <div>
      <UpdateActions
        currentPackage={updatingPackage}
        error={error}
        eventLog={eventLog}
        isUpdating={recoveryPending || !!updatingPackage}
        onCancel={recoveryPending ? undefined : onCancel}
        onClearError={onClearError}
        progress={progress}
        status={status}
      />

      <UpdateList
        currentPackage={updatingPackage}
        isUpdating={recoveryPending || !!updatingPackage}
        onUpdateClick={onUpdateOne}
        updates={updates}
      />
    </div>
  );
};

export default UpdateStatus;
