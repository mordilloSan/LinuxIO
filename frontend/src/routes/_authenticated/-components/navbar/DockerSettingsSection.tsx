import { Icon } from "@iconify/react";
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
} from "react";
import { toast } from "sonner";

import { linuxio, useCallMutation } from "@/api";
import { cardBodyToggleProps } from "@/components/cards/cardBodyToggle";
import FrostedCard from "@/components/cards/FrostedCard";
import ConfirmDialog from "@/components/filebrowser/ConfirmDialog";
import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTypography from "@/components/ui/AppTypography";
import PathPickerField from "@/components/ui/PathPickerField";
import useAuth from "@/hooks/useAuth";
import { useCapability } from "@/hooks/useCapabilities";
import { useConfig } from "@/hooks/useConfig";
import { useAppTheme } from "@/theme";
import { ensureTrailingSlash } from "@/utils/path";

import DockerAutoUpdateSettingsSection from "./DockerAutoUpdateSettingsSection";
import { useDockerAutoUpdateState } from "./useDockerAutoUpdateState";

const normalizePathInput = (value: string): string => {
  const trimmed = value.trim();
  if (trimmed === "") return "";
  if (/^\/+$/.test(trimmed)) return "/";
  return trimmed.replace(/\/+$/, "");
};

const normalizeFolderList = (values: readonly string[]): string[] =>
  values.map(normalizePathInput).filter(Boolean);

const areStringListsEqual = (
  left: readonly string[],
  right: readonly string[],
): boolean =>
  left.length === right.length &&
  left.every((value, index) => value === right[index]);

const validateDraftFolders = (
  drafts: readonly string[],
): { folders: string[]; errors: string[] } => {
  const folders = drafts.map(normalizePathInput);
  const errors = drafts.map(() => "");
  const seen = new Map<string, number>();

  folders.forEach((folder, index) => {
    if (!folder) {
      errors[index] = "Docker folder is required.";
      return;
    }
    if (!folder.startsWith("/")) {
      errors[index] = "Docker folder must be an absolute path.";
      return;
    }
    if (folder === "/") {
      errors[index] = 'Docker folder cannot be "/".';
      return;
    }

    const duplicateIndex = seen.get(folder);
    if (duplicateIndex != null) {
      errors[index] = "Docker folder is already listed.";
      return;
    }
    seen.set(folder, index);
  });

  return { folders, errors };
};

const DockerSettingsSection = () => {
  const theme = useAppTheme();
  const { privileged } = useAuth();
  const { config, updateConfig } = useConfig();
  const dockerAutoUpdate = useDockerAutoUpdateState();
  const { isEnabled: dockerUpdatesEnabled, reason: dockerUpdatesReason } =
    useCapability("dockerUpdatesAvailable");
  // Errors and toasts are handled by handleSave's try/catch.
  const { mutateAsync: createDockerFolder } = useCallMutation(
    linuxio.filebrowser.resource_post,
  );
  const { mutateAsync: validateDockerFolder } = useCallMutation(
    linuxio.docker.validate_stack_directory,
  );
  const dockerFolders = config.docker.folders;
  const requireMountsForFolders = config.docker.requireMountsForFolders;
  const setDockerFolders = useCallback(
    (folders: string[]) => updateConfig({ docker: { folders } }),
    [updateConfig],
  );
  const setRequireMountsForFolders = useCallback(
    (enabled: boolean) =>
      updateConfig({ docker: { requireMountsForFolders: enabled } }, () =>
        toast.success(
          enabled
            ? "Docker will wait for configured folder mounts."
            : "Docker folder mount ordering disabled.",
        ),
      ),
    [updateConfig],
  );
  const configuredFolders = useMemo(
    () => normalizeFolderList(dockerFolders ?? []),
    [dockerFolders],
  );
  const configuredFoldersKey = configuredFolders.join("\n");

  const [drafts, setDrafts] = useState<string[]>(
    configuredFolders.length > 0 ? configuredFolders : [""],
  );
  const [errorTexts, setErrorTexts] = useState<string[]>([]);
  const [isSaving, setIsSaving] = useState(false);

  const [createPromptOpen, setCreatePromptOpen] = useState(false);
  const [createPromptPath, setCreatePromptPath] = useState<string | null>(null);

  const createPromptResolverRef = useRef<((confirmed: boolean) => void) | null>(
    null,
  );

  const [prevConfiguredFoldersKey, setPrevConfiguredFoldersKey] =
    useState(configuredFoldersKey);
  if (configuredFoldersKey !== prevConfiguredFoldersKey) {
    setPrevConfiguredFoldersKey(configuredFoldersKey);
    setDrafts(configuredFolders.length > 0 ? configuredFolders : [""]);
    setErrorTexts([]);
  }

  const draftFolders = useMemo(() => drafts.map(normalizePathInput), [drafts]);
  const configuredComparisonFolders =
    configuredFolders.length > 0 ? configuredFolders : [""];
  const isDirty = !areStringListsEqual(
    draftFolders,
    configuredComparisonFolders,
  );

  const resolveCreatePrompt = useCallback((confirmed: boolean) => {
    setCreatePromptOpen(false);
    setCreatePromptPath(null);
    const resolve = createPromptResolverRef.current;
    createPromptResolverRef.current = null;
    resolve?.(confirmed);
  }, []);

  const askCreatePrompt = useCallback(
    async (path: string): Promise<boolean> => {
      if (createPromptResolverRef.current) {
        createPromptResolverRef.current(false);
      }
      return new Promise((resolve) => {
        createPromptResolverRef.current = resolve;
        setCreatePromptPath(path);
        setCreatePromptOpen(true);
      });
    },
    [],
  );

  useEffect(
    () => () => {
      if (createPromptResolverRef.current) {
        createPromptResolverRef.current(false);
        createPromptResolverRef.current = null;
      }
    },
    [],
  );

  const handleReset = useCallback(() => {
    setDrafts(configuredFolders.length > 0 ? configuredFolders : [""]);
    setErrorTexts([]);
  }, [configuredFolders]);

  const handleAddFolder = useCallback(() => {
    setDrafts((prev) => [...prev, ""]);
  }, []);

  const handleRemoveFolder = useCallback((index: number) => {
    setDrafts((prev) => prev.filter((_, itemIndex) => itemIndex !== index));
    setErrorTexts((prev) => prev.filter((_, itemIndex) => itemIndex !== index));
  }, []);

  const handleDraftChange = useCallback((index: number, value: string) => {
    setDrafts((prev) =>
      prev.map((item, itemIndex) => (itemIndex === index ? value : item)),
    );
    setErrorTexts((prev) =>
      prev.map((item, itemIndex) => (itemIndex === index ? "" : item)),
    );
  }, []);

  const handleSave = useCallback(async () => {
    const { folders, errors } = validateDraftFolders(drafts);

    if (errors.some(Boolean)) {
      setErrorTexts(errors);
      return;
    }

    setErrorTexts([]);
    setIsSaving(true);

    try {
      for (let index = 0; index < folders.length; index += 1) {
        const folder = folders[index];
        const validation = await validateDockerFolder({ dirPath: folder });
        if (!validation.valid) {
          setErrorTexts((prev) => {
            const next = [...prev];
            next[index] = validation.error || "Docker folder is not valid.";
            return next;
          });
          return;
        }

        if (!validation.exists) {
          const shouldCreate = await askCreatePrompt(folder);
          if (!shouldCreate) {
            toast.info("Docker folder was not created. Save canceled.");
            return;
          }

          await createDockerFolder({
            path: ensureTrailingSlash(folder),
          });
          toast.success("Docker folder created.");
        }
      }

      setDockerFolders(folders);
      setDrafts(folders);
      toast.success("Docker folders saved.");
    } catch (error: unknown) {
      const message =
        error instanceof Error
          ? error.message
          : "Failed to save Docker folders";
      toast.error(message);
    } finally {
      setIsSaving(false);
    }
  }, [
    askCreatePrompt,
    createDockerFolder,
    drafts,
    setDockerFolders,
    validateDockerFolder,
  ]);

  const folderIconStyle: CSSProperties = {
    display: "inline-flex",
    alignItems: "center",
    justifyContent: "center",
    flexShrink: 0,
    width: 36,
    height: 36,
    borderRadius: 8,
    background: theme.palette.action.hover,
    color: theme.palette.primary.main,
  };

  return (
    <>
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          gap: theme.spacing(1.5),
        }}
      >
        <div>
          <AppTypography fontWeight={600} variant="body1">
            Docker Folders
          </AppTypography>
          <AppTypography color="text.secondary" variant="caption">
            Directories scanned for Docker Compose stacks.
          </AppTypography>
        </div>

        {drafts.map((draft, index) => (
          <FrostedCard key={index} style={{ padding: 12 }}>
            <div
              style={{
                display: "flex",
                alignItems: "flex-start",
                gap: theme.spacing(1.5),
              }}
            >
              <div style={folderIconStyle}>
                <Icon height={22} icon="mdi:folder-open-outline" width={22} />
              </div>

              <PathPickerField
                disabled={isSaving}
                editable
                error={Boolean(errorTexts[index])}
                helperText={
                  errorTexts[index] ||
                  "Absolute path only. Root (/) is not allowed."
                }
                label={index === 0 ? "Path" : `Path ${index + 1}`}
                onChange={(value) => handleDraftChange(index, value)}
                placeholder="/home/user/docker"
                style={{ flex: 1 }}
                value={draft}
              />

              {drafts.length > 1 ? (
                <AppIconButton
                  aria-label={`Remove Docker folder ${index + 1}`}
                  disabled={isSaving}
                  onClick={() => handleRemoveFolder(index)}
                  size="small"
                  style={{ marginTop: 3 }}
                >
                  <Icon height={16} icon="mdi:close" width={16} />
                </AppIconButton>
              ) : null}
            </div>
          </FrostedCard>
        ))}

        <FrostedCard
          hoverLift
          {...cardBodyToggleProps({
            checked: requireMountsForFolders,
            disabled: !privileged,
            onChange: setRequireMountsForFolders,
          })}
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            gap: theme.spacing(1.5),
            opacity: privileged ? 1 : 0.72,
            padding: 12,
          }}
        >
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: theme.spacing(1.5),
              minWidth: 0,
            }}
          >
            <div style={folderIconStyle}>
              <Icon height={22} icon="mdi:server-network" width={22} />
            </div>
            <div style={{ minWidth: 0 }}>
              <AppTypography fontWeight={600} variant="body2">
                Wait for folder mounts
              </AppTypography>
              <AppTypography color="text.secondary" variant="caption">
                {privileged
                  ? "Require configured folders before Docker starts."
                  : "Privileged mode is required to change Docker startup ordering."}
              </AppTypography>
            </div>
          </div>
          <AppSwitch
            aria-label="Wait for Docker folder mounts"
            checked={requireMountsForFolders}
            disabled={!privileged}
            onChange={(_, checked) => setRequireMountsForFolders(checked)}
          />
        </FrostedCard>

        <FrostedCard hoverLift={!isSaving} style={{ padding: 0 }}>
          <AppButton
            color="inherit"
            disabled={isSaving}
            fullWidth
            onClick={handleAddFolder}
            style={{
              alignItems: "center",
              borderRadius: "inherit",
              display: "flex",
              gap: theme.spacing(1.5),
              justifyContent: "flex-start",
              minWidth: 0,
              opacity: isSaving ? 0.65 : 1,
              padding: 12,
              textAlign: "left",
            }}
          >
            <div style={folderIconStyle}>
              <Icon height={22} icon="mdi:plus" width={22} />
            </div>
            <div>
              <AppTypography fontWeight={600} variant="body2">
                Add Docker folder
              </AppTypography>
              <AppTypography color="text.secondary" variant="caption">
                Add another directory for compose stacks.
              </AppTypography>
            </div>
          </AppButton>
        </FrostedCard>

        <div
          style={{
            display: "flex",
            justifyContent: "flex-end",
            gap: theme.spacing(1.5),
            paddingTop: theme.spacing(0.5),
          }}
        >
          <AppButton disabled={!isDirty || isSaving} onClick={handleReset}>
            Reset
          </AppButton>
          <AppButton
            disabled={!isDirty || isSaving}
            onClick={() => void handleSave()}
            variant="contained"
          >
            {isSaving ? "Saving..." : "Save"}
          </AppButton>
        </div>
      </div>

      <div
        style={{
          display: "flex",
          flexDirection: "column",
          gap: theme.spacing(1.5),
          marginTop: theme.spacing(1.5),
        }}
      >
        <div>
          <AppTypography fontWeight={600} variant="body1">
            Scheduled Updates
          </AppTypography>
          <AppTypography color="text.secondary" variant="caption">
            Schedule image checks and automatic container updates.
          </AppTypography>
        </div>
        <DockerAutoUpdateSettingsSection
          autoUpdate={dockerAutoUpdate}
          dockerUpdatesEnabled={dockerUpdatesEnabled}
          dockerUpdatesReason={dockerUpdatesReason}
        />
      </div>

      <ConfirmDialog
        cancelText="Cancel"
        confirmText="Create"
        message={`This directory does not exist yet: "${createPromptPath ?? ""}". Create it now?`}
        onClose={() => resolveCreatePrompt(false)}
        onConfirm={() => resolveCreatePrompt(true)}
        open={createPromptOpen}
        title="Create Docker Folder?"
      />
    </>
  );
};

export default DockerSettingsSection;
