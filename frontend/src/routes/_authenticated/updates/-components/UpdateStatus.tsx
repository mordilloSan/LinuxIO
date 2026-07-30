import type { Update } from "@/api";

import UpdateActions from "./UpdateActions";
import UpdateList from "./UpdateList";

interface UpdateStatusProps {
  canCancel?: boolean;
  error?: string | null;
  eventLog?: string[];
  onCancel?: () => void;
  onClearError?: () => void;
  onUpdateOne: (pkg: string) => Promise<void>;
  progress: number;
  recoveryPending?: boolean;
  isUpdating?: boolean;
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
  isUpdating = !!updatingPackage,
  canCancel = isUpdating,
}: UpdateStatusProps) => {
  return (
    <div>
      {/*
        The progress panel follows the adopted transaction, not the recovery
        scan: `recoveryPending` is true on every entry into the section, and
        painting "Preparing… 0%" before a job is found reports an update that
        may not exist. Cancelability is supplied separately by the controller:
        a recovered attachment exposes it as soon as it has a live job, while a
        finished-but-still-visible panel deliberately does not.
      */}
      <UpdateActions
        currentPackage={updatingPackage}
        error={error}
        eventLog={eventLog}
        isUpdating={isUpdating}
        canCancel={canCancel}
        onCancel={onCancel}
        onClearError={onClearError}
        progress={progress}
        status={status}
      />

      <UpdateList
        currentPackage={updatingPackage}
        isUpdating={recoveryPending || isUpdating}
        onUpdateClick={onUpdateOne}
        updates={updates}
      />
    </div>
  );
};

export default UpdateStatus;
