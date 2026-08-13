import { linuxio, useCallMutation } from "@/api";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import { useCapability } from "@/hooks/useCapabilities";
import { useScopedToast } from "@/hooks/useScopedToast";

const DOCKER_TOAST_META = { label: "Open Docker", to: "/docker" } as const;

export function useDockerUpdateCheck() {
  const toast = useScopedToast(DOCKER_TOAST_META);
  const { isEnabled: dockerUpdatesEnabled, reason: dockerUpdatesReason } =
    useCapability("dockerUpdatesAvailable");
  const { mutate: checkUpdates, isPending: isCheckingUpdates } =
    useCallMutation(linuxio.docker.check_updates, {
      success: (result) => {
        const checked = result?.checked ?? 0;
        const errors = result?.errors ?? 0;
        const uncheckable = result?.uncheckable ?? 0;
        const updates = result?.updates ?? 0;
        if (errors > 0) {
          const uncheckableMessage =
            uncheckable > 0 ? `, ${uncheckable} cannot be checked` : "";
          toast.warning(
            `Checked ${checked} container(s), ${errors} check error(s)${uncheckableMessage}, found ${updates} update(s)`,
          );
          return;
        }
        if (uncheckable > 0 && errors === 0) {
          toast.warning(
            `Checked ${checked} container(s), ${uncheckable} cannot be checked, found ${updates} update(s)`,
          );
          return;
        }
        toast.success(
          `Checked ${checked} container(s), found ${updates} update(s)`,
        );
      },
      error: "Failed to check updates",
      toast: DOCKER_TOAST_META,
    });

  return {
    button: (
      <AppActionIconButton
        ariaLabel="Check container updates"
        disabled={!dockerUpdatesEnabled}
        icon="mdi:refresh"
        iconSize={20}
        label={dockerUpdatesEnabled ? "Check Updates" : dockerUpdatesReason}
        loading={isCheckingUpdates}
        onClick={() => checkUpdates()}
      />
    ),
    isCheckingUpdates,
  };
}
