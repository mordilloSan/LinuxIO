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
      {/*
        The progress panel follows the adopted transaction, not the recovery
        scan: `recoveryPending` is true on every entry into the section, and
        painting "Preparing… 0%" before a job is found reports an update that
        may not exist. Cancel stays wired throughout — it no-ops until the hook
        holds a job id, and suppressing it would strand the resume window with
        no page-level cancel.
      */}
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
        isUpdating={recoveryPending || !!updatingPackage}
        onUpdateClick={onUpdateOne}
        updates={updates}
      />
    </div>
  );
};

export default UpdateStatus;
