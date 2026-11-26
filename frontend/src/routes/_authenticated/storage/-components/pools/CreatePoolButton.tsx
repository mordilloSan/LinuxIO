import { useState } from "react";

import { linuxio, type BtrfsOpProgress } from "@/api";
import AppButton from "@/components/ui/AppButton";

import BtrfsCreatePoolDialog from "../btrfs/BtrfsCreatePoolDialog";

/**
 * Create pool and its dialog, on the Overview and the Disks page alike. A
 * task, not a call: the bridge trims every disk before mkfs, which can
 * outlast any request timeout on large SSDs.
 */
const CreatePoolButton = ({ pooledDevices }: { pooledDevices: string[] }) => {
  const [open, setOpen] = useState(false);
  const [step, setStep] = useState<BtrfsOpProgress | null>(null);
  const createPool = linuxio.storage.btrfs_pool_create.useMutation({
    onProgress: (progress) => setStep(progress.detail ?? null),
    success: "Pool created",
    error: "Failed to create pool",
    options: { onSettled: () => setStep(null) },
  });
  return (
    <>
      <AppButton onClick={() => setOpen(true)} size="small" variant="outlined">
        Create pool
      </AppButton>
      {open && (
        <BtrfsCreatePoolDialog
          isPending={createPool.isPending}
          onClose={() => setOpen(false)}
          onConfirm={(request) =>
            createPool.mutate(request, { onSuccess: () => setOpen(false) })
          }
          pooledDevices={pooledDevices}
          progress={step}
        />
      )}
    </>
  );
};

export default CreatePoolButton;
