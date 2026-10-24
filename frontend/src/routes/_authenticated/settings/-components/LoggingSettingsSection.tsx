import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";

import { linuxio, useCallMutation } from "@/api";
import {
  SectionCard,
  ToggleCard,
} from "@/components/settings/SettingsSectionPrimitives";
import AppAlert from "@/components/ui/AppAlert";

const LoggingSettingsSection = () => {
  const { data, error, isLoading, refetch } = useQuery(
    linuxio.logging.get_debug,
  );
  const setDebug = useCallMutation(linuxio.logging.set_debug, {
    success: (_, { enabled }) =>
      toast.success(
        enabled ? "Debug logging enabled" : "Debug logging disabled",
      ),
    error: "Failed to change debug logging",
    // The switch can change before the daemon reload fails, so the failure
    // path re-reads the real state; success is covered by the route invalidation.
    options: { onError: () => void refetch() },
  });

  return (
    <SectionCard icon="mdi:bug-outline" title="Logging">
      {error ? (
        <AppAlert severity="error">{error.message}</AppAlert>
      ) : (
        <ToggleCard
          checked={data?.enabled ?? false}
          description="Writes debug lines to the journal until switched off"
          disabled={isLoading || setDebug.isPending}
          label="Debug logging"
          onChange={(enabled) => setDebug.mutate({ enabled })}
        />
      )}
    </SectionCard>
  );
};

export default LoggingSettingsSection;
