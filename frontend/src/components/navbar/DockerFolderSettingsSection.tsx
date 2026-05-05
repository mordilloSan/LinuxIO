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
import DockerIndexerDialog from "@/components/docker/DockerIndexerDialog";
import ConfirmDialog from "@/components/filebrowser/ConfirmDialog";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import { useCapability } from "@/hooks/useCapabilities";
import { useConfigValue } from "@/hooks/useConfig";
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
  const [dockerFolders, setDockerFolders] = useConfigValue("dockerFolders");
  const {
    isEnabled: indexerEnabled,
    status: indexerStatus,
    reason: indexerReason,
  } = useCapability("indexerAvailable");

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
  const [reindexPromptOpen, setReindexPromptOpen] = useState(false);
  const [indexerDialogOpen, setIndexerDialogOpen] = useState(false);

  const createPromptResolverRef = useRef<((confirmed: boolean) => void) | null>(
    null,
  );
  const reindexPromptResolverRef = useRef<
    ((confirmed: boolean) => void) | null
  >(null);

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

  const resolveReindexPrompt = useCallback((confirmed: boolean) => {
    setReindexPromptOpen(false);
    const resolve = reindexPromptResolverRef.current;
    reindexPromptResolverRef.current = null;
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

  const askReindexPrompt = useCallback(async (): Promise<boolean> => {
    if (reindexPromptResolverRef.current) {
      reindexPromptResolverRef.current(false);
    }
    return new Promise((resolve) => {
      reindexPromptResolverRef.current = resolve;
      setReindexPromptOpen(true);
    });
  }, []);

  useEffect(
    () => () => {
      if (createPromptResolverRef.current) {
        createPromptResolverRef.current(false);
        createPromptResolverRef.current = null;
      }
      if (reindexPromptResolverRef.current) {
        reindexPromptResolverRef.current(false);
        reindexPromptResolverRef.current = null;
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
          await linuxio.docker.validate_stack_directory.call(folder);
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

          await linuxio.filebrowser.resource_post.call(
            ensureTrailingSlash(folder),
          );
          toast.success("Docker folder created.");
        }
      }

      setDockerFolders(folders);
      setDrafts(folders);
      toast.success("Docker folders saved.");

      if (!indexerEnabled) {
        toast.info(`${indexerReason} Reindex later from the Docker page.`);
        return;
      }

      const shouldReindex = await askReindexPrompt();
      if (shouldReindex) {
        setIndexerDialogOpen(true);
      }
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
    askReindexPrompt,
    drafts,
    indexerEnabled,
    indexerReason,
    setDockerFolders,
  ]);

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
          <AppTypography variant="body1" fontWeight={600}>
            Docker Folders
          </AppTypography>
          <AppTypography variant="caption" color="text.secondary">
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
                <Icon icon="mdi:folder-open-outline" width={22} height={22} />
              </div>

              <AppTextField
                label={index === 0 ? "Path" : `Path ${index + 1}`}
                size="small"
                value={draft}
                onChange={(event) =>
                  handleDraftChange(index, event.target.value)
                }
                placeholder="/home/user/docker"
                fullWidth
                error={Boolean(errorTexts[index])}
                helperText={
                  errorTexts[index] ||
                  "Absolute path only. Root (/) is not allowed."
                }
                disabled={isSaving}
                style={{ flex: 1 }}
              />

              {drafts.length > 1 ? (
                <AppIconButton
                  size="small"
                  onClick={() => handleRemoveFolder(index)}
                  disabled={isSaving}
                  aria-label={`Remove Docker folder ${index + 1}`}
                  style={{ marginTop: 3 }}
                >
                  <Icon icon="mdi:close" width={16} height={16} />
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
          tabIndex={isSaving ? -1 : 0}
          style={{
            padding: 12,
            cursor: isSaving ? "default" : "pointer",
            opacity: isSaving ? 0.65 : 1,
          }}
        >
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: theme.spacing(1.5),
            }}
          >
            <div style={folderIconStyle}>
              <Icon icon="mdi:plus" width={22} height={22} />
            </div>
            <div>
              <AppTypography variant="body2" fontWeight={600}>
                Add Docker folder
              </AppTypography>
              <AppTypography variant="caption" color="text.secondary">
                Scan another directory for compose stacks.
              </AppTypography>
            </div>
          </div>
        </FrostedCard>

        {!indexerEnabled && (
          <AppAlert severity="info">
            {indexerStatus === "unknown"
              ? "Indexer availability is being checked. You can still save these paths and reindex later."
              : "Indexer service is unavailable. You can still save these paths and reindex later."}
          </AppAlert>
        )}

        <div
          style={{
            display: "flex",
            justifyContent: "flex-end",
            gap: theme.spacing(1.5),
            paddingTop: theme.spacing(0.5),
          }}
        >
          <AppButton onClick={handleReset} disabled={!isDirty || isSaving}>
            Reset
          </AppButton>
          <AppButton
            variant="contained"
            onClick={() => void handleSave()}
            disabled={!isDirty || isSaving}
          >
            {isSaving ? "Saving..." : "Save"}
          </AppButton>
        </div>
      </div>

      <ConfirmDialog
        open={createPromptOpen}
        onClose={() => resolveCreatePrompt(false)}
        onConfirm={() => resolveCreatePrompt(true)}
        title="Create Docker Folder?"
        message={`This directory does not exist yet: "${createPromptPath ?? ""}". Create it now?`}
        confirmText="Create"
        cancelText="Cancel"
      />

      <ConfirmDialog
        open={reindexPromptOpen}
        onClose={() => resolveReindexPrompt(false)}
        onConfirm={() => resolveReindexPrompt(true)}
        title="Reindex Docker Folders?"
        message="Docker folders updated successfully. Start a new scan now?"
        confirmText="Scan Now"
        cancelText="Later"
      />

      <DockerIndexerDialog
        open={indexerDialogOpen}
        onClose={() => setIndexerDialogOpen(false)}
        onComplete={() => {
          toast.success("Docker folders indexed successfully.");
        }}
      />
    </>
  );
};

export default DockerFolderSettingsSection;
