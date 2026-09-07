import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { useState } from "react";

import {
  linuxio,
  useCallMutation,
  type VMCreateProgress,
  type VMTemplate,
} from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTypography from "@/components/ui/AppTypography";
import { useScopedToast } from "@/hooks/useScopedToast";
import { formatFileSize } from "@/utils/formaters";
import { getMutationErrorMessage } from "@/utils/mutations";

import { IMAGE_PRESETS, VM_TOAST } from "./vmShared";

const VMImagesPage = () => {
  const { data: preflight } = useSuspenseQuery({
    ...linuxio.virt.preflight({}),
    refetchOnMount: false,
  });
  const library = useQuery({
    ...linuxio.virt.templates,
    refetchOnMount: "always",
  });
  const toast = useScopedToast(VM_TOAST);
  const [progress, setProgress] = useState<VMCreateProgress | null>(null);
  const [deleting, setDeleting] = useState<VMTemplate | null>(null);
  const download = linuxio.virt.template_download.useTaskStreamAction({
    onProgress: (event) =>
      setProgress(
        event.detail ?? {
          message: event.message ?? "Preparing template",
          phase: event.phase ?? "download",
          percent: event.percentage,
        },
      ),
    success: (template) => {
      setProgress(null);
      toast.success(`${template.label} ${template.version} is saved and ready`);
    },
    error: (error) =>
      setProgress({
        phase: "error",
        message: getMutationErrorMessage(error, "Template download failed"),
      }),
    options: {
      onMutate: () =>
        setProgress({
          phase: "starting",
          message: "Checking for a template update",
        }),
    },
  });
  const remove = useCallMutation(linuxio.virt.template_delete, {
    success: () => {
      setDeleting(null);
      toast.success("Deleted saved template");
    },
    error: (error) =>
      toast.error(getMutationErrorMessage(error, "Failed to delete template")),
  });

  return (
    <div style={{ display: "grid", gap: "var(--app-space-16)" }}>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          gap: "var(--app-space-12)",
        }}
      >
        <AppTypography component="h2" variant="h6">
          VM templates
        </AppTypography>
        <AppButton
          disabled={library.isFetching}
          onClick={() => library.refetch()}
          variant="outlined"
        >
          Refresh templates
        </AppButton>
      </div>
      <AppTypography color="text.secondary" variant="body2">
        Download a template once, then reuse it for new VMs. Updates are
        downloaded only when you choose; existing VMs keep their own disks.
      </AppTypography>
      {library.data ? (
        <AppTypography component="div" variant="body2">
          Template folder:{" "}
          <code style={{ overflowWrap: "anywhere" }}>{library.data.path}</code>
        </AppTypography>
      ) : null}
      <AppTypography component="div" color="text.secondary" variant="body2">
        VM disks:{" "}
        <code style={{ overflowWrap: "anywhere" }}>
          {preflight.managedPaths.cloudImages}
        </code>
        <br />
        ISO installers:{" "}
        <code style={{ overflowWrap: "anywhere" }}>
          {preflight.managedPaths.isos}
        </code>
      </AppTypography>
      {library.isPending ? (
        <AppLinearProgress aria-label="Loading templates" />
      ) : null}
      {library.isError ? (
        <AppAlert severity="error">
          Unable to load saved templates: {library.error.message}
        </AppAlert>
      ) : null}
      {progress ? (
        <div
          aria-live="polite"
          style={{ display: "grid", gap: "var(--app-space-8)" }}
        >
          <AppAlert severity={progress.phase === "error" ? "error" : "info"}>
            {progress.message}
          </AppAlert>
          {download.isPending ? (
            <AppLinearProgress
              value={progress.percent ?? 0}
              variant={
                progress.percent === undefined ? "indeterminate" : "determinate"
              }
            />
          ) : null}
        </div>
      ) : null}
      {IMAGE_PRESETS.map((preset) => {
        const versions = (library.data?.templates ?? []).filter(
          (template) => template.imagePresetId === preset.imagePresetId,
        );
        return (
          <FrostedCard
            key={preset.id}
            style={{
              display: "grid",
              gap: "var(--app-space-12)",
              padding: "var(--app-space-16)",
            }}
          >
            <div
              style={{
                display: "flex",
                flexWrap: "wrap",
                alignItems: "center",
                justifyContent: "space-between",
                gap: "var(--app-space-12)",
              }}
            >
              <AppTypography component="h3" fontWeight={700} variant="body1">
                {preset.label}
              </AppTypography>
              <AppButton
                aria-label={`${versions.length ? "Download update for" : "Download"} ${preset.label}`}
                disabled={
                  download.isPending || remove.isPending || !library.data
                }
                onClick={() =>
                  download.mutate({ imagePresetId: preset.imagePresetId })
                }
                variant="outlined"
              >
                {versions.length ? "Download update" : "Download"}
              </AppButton>
            </div>
            {versions.length === 0 && library.data ? (
              <AppTypography color="text.secondary" variant="body2">
                Not downloaded. Creating the first VM also saves this template.
              </AppTypography>
            ) : null}
            {versions.map((template, index) => (
              <div
                key={template.id}
                style={{
                  display: "grid",
                  gap: "var(--app-space-8)",
                  paddingTop: "var(--app-space-12)",
                  borderTop: "1px solid var(--app-palette-divider)",
                  minWidth: 0,
                }}
              >
                <div
                  style={{
                    display: "flex",
                    flexWrap: "wrap",
                    alignItems: "center",
                    justifyContent: "space-between",
                    gap: "var(--app-space-8)",
                  }}
                >
                  <AppTypography variant="body2" fontWeight={600}>
                    Version {template.version}
                    {index === 0 ? " · Newest saved" : ""}
                  </AppTypography>
                  <AppButton
                    aria-label={`Delete ${template.label} ${template.version} ${template.id.slice(0, 12)}`}
                    color="error"
                    disabled={download.isPending || remove.isPending}
                    onClick={() => setDeleting(template)}
                    size="small"
                    variant="text"
                  >
                    Delete
                  </AppButton>
                </div>
                <AppTypography color="text.secondary" variant="body2">
                  {formatFileSize(template.sizeBytes)} · Saved{" "}
                  {new Date(template.downloadedAt).toLocaleString()}
                </AppTypography>
                <AppTypography
                  component="div"
                  variant="caption"
                  style={{ overflowWrap: "anywhere" }}
                >
                  Path: <code>{template.path}</code>
                  <br />
                  Source: {template.sourceUrl}
                  <br />
                  Download SHA-256: <code>{template.id}</code>
                </AppTypography>
              </div>
            ))}
          </FrostedCard>
        );
      })}
      <GeneralDialog
        open={deleting !== null}
        onClose={() => {
          if (!remove.isPending) setDeleting(null);
        }}
        fullWidth
        maxWidth="sm"
      >
        <AppDialogTitle>Delete saved template</AppDialogTitle>
        <AppDialogContent>
          <AppTypography variant="body2">
            Delete {deleting?.label} {deleting?.version}? Existing VMs keep
            their independent disks. This version will no longer be available
            for new VMs unless downloaded again.
          </AppTypography>
          <AppTypography
            component="div"
            variant="caption"
            style={{
              overflowWrap: "anywhere",
              marginTop: "var(--app-space-12)",
            }}
          >
            {deleting?.path}
          </AppTypography>
        </AppDialogContent>
        <AppDialogActions>
          <AppButton
            disabled={remove.isPending}
            onClick={() => setDeleting(null)}
            variant="text"
          >
            Cancel
          </AppButton>
          <AppButton
            color="error"
            disabled={remove.isPending || !deleting}
            onClick={() => {
              if (deleting)
                remove.mutate({
                  imagePresetId: deleting.imagePresetId,
                  templateId: deleting.id,
                });
            }}
            variant="contained"
          >
            Delete template
          </AppButton>
        </AppDialogActions>
      </GeneralDialog>
    </div>
  );
};

export default VMImagesPage;
