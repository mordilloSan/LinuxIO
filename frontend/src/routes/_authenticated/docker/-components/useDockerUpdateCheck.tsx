import { linuxio } from "@/api";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import { useCapability } from "@/hooks/useCapabilities";
import { useScopedToast } from "@/hooks/useScopedToast";

const DOCKER_TOAST_META = { label: "Open Docker", to: "/docker" } as const;

export function useDockerUpdateCheck() {
  const toast = useScopedToast(DOCKER_TOAST_META);
  const { isEnabled: watchtowerEnabled, reason: watchtowerReason } =
    useCapability("watchtowerAvailable");
  const { mutate: checkUpdates, isPending: isCheckingUpdates } =
    linuxio.docker.check_updates.useAction({
      success: (result) => {
        const checked = result?.checked ?? 0;
        const updates = result?.updates ?? 0;
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
        disabled={!watchtowerEnabled}
        icon="mdi:refresh"
        iconSize={20}
        label={watchtowerEnabled ? "Check Updates" : watchtowerReason}
        loading={isCheckingUpdates}
        onClick={() => checkUpdates()}
      />
    ),
    isCheckingUpdates,
  };
}
