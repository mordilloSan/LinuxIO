import { Icon } from "@iconify/react";
import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { toast } from "sonner";

import { linuxio } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import ConfirmDialog from "@/components/filebrowser/ConfirmDialog";
import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTypography from "@/components/ui/AppTypography";
import PathPickerField from "@/components/ui/PathPickerField";
import { useConfig } from "@/hooks/useConfig";
import { useAppTheme } from "@/theme";

const normalizePathInput = (value: string): string => {
  const trimmed = value.trim();
  if (trimmed === "") return "";
  if (/^\/+$/.test(trimmed)) return "/";
  return trimmed.replace(/\/+$/, "");
};

const ensureTrailingSlash = (path: string): string =>
  path.endsWith("/") ? path : `${path}/`;

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

const DockerFolderSettingsSection: React.FC = () => {
  const theme = useAppTheme();
  const { config, updateConfig } = useConfig();
  const dockerFolders = config.docker.folders;
  const setDockerFolders = useCallback(
    (folders: string[]) => updateConfig({ docker: { folders } }),
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
        const validation =
          await linuxio.docker.validate_stack_directory(folder);
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

          await linuxio.filebrowser.resource_post({
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
  }, [askCreatePrompt, drafts, setDockerFolders]);

  const folderIconStyle: React.CSSProperties = {
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
          onClick={isSaving ? undefined : handleAddFolder}
          onKeyDown={(event) => {
            if (isSaving) return;
            if (event.key === "Enter" || event.key === " ") {
              event.preventDefault();
              handleAddFolder();
            }
          }}
          role="button"
          style={{
            padding: 12,
            cursor: isSaving ? "default" : "pointer",
            opacity: isSaving ? 0.65 : 1,
          }}
          tabIndex={isSaving ? -1 : 0}
        >
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: theme.spacing(1.5),
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
          </div>
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

export default DockerFolderSettingsSection;
