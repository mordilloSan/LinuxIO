import { Button, Divider, TextField, Typography, Alert } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { toast } from "sonner";

import { linuxio } from "@/api";
import DockerIndexerDialog from "@/components/docker/DockerIndexerDialog";
import ConfirmDialog from "@/components/filebrowser/ConfirmDialog";
import useAuth from "@/hooks/useAuth";
import { useConfigValue } from "@/hooks/useConfig";

const normalizePathInput = (value: string): string => {
  const trimmed = value.trim();
  if (trimmed === "") return "";
  if (/^\/+$/.test(trimmed)) return "/";
  return trimmed.replace(/\/+$/, "");
};

const ensureTrailingSlash = (path: string): string =>
  path.endsWith("/") ? path : `${path}/`;

const DockerFolderSettingsSection: React.FC = () => {
  const theme = useTheme();
  const [dockerFolder, setDockerFolder] = useConfigValue("dockerFolder");
  const { indexerAvailable } = useAuth();

  const [draft, setDraft] = useState(dockerFolder ?? "");
  const [errorText, setErrorText] = useState<string | null>(null);
  const [isSaving, setIsSaving] = useState(false);

  const [createPromptOpen, setCreatePromptOpen] = useState(false);
  const [reindexPromptOpen, setReindexPromptOpen] = useState(false);
  const [indexerDialogOpen, setIndexerDialogOpen] = useState(false);

  const createPromptResolverRef = useRef<((confirmed: boolean) => void) | null>(
    null,
  );
  const reindexPromptResolverRef = useRef<
    ((confirmed: boolean) => void) | null
  >(null);

  useEffect(() => {
    setDraft(dockerFolder ?? "");
    setErrorText(null);
  }, [dockerFolder]);

  const currentNormalized = useMemo(
    () => normalizePathInput(dockerFolder ?? ""),
    [dockerFolder],
  );
  const draftNormalized = useMemo(() => normalizePathInput(draft), [draft]);
  const isDirty = draftNormalized !== currentNormalized;

  const resolveCreatePrompt = useCallback((confirmed: boolean) => {
    setCreatePromptOpen(false);
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

  const askCreatePrompt = useCallback(async (): Promise<boolean> => {
    if (createPromptResolverRef.current) {
      createPromptResolverRef.current(false);
    }
    return new Promise((resolve) => {
      createPromptResolverRef.current = resolve;
      setCreatePromptOpen(true);
    });
  }, []);

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
    setDraft(dockerFolder ?? "");
    setErrorText(null);
  }, [dockerFolder]);

  const handleSave = useCallback(async () => {
    const normalized = normalizePathInput(draft);

    if (!normalized) {
      setErrorText("Docker folder is required.");
      return;
    }
    if (!normalized.startsWith("/")) {
      setErrorText("Docker folder must be an absolute path.");
      return;
    }
    if (normalized === "/") {
      setErrorText('Docker folder cannot be "/".');
      return;
    }

    setErrorText(null);
    setIsSaving(true);

    try {
      const validation =
        await linuxio.docker.validate_stack_directory.call(normalized);
      if (!validation.valid) {
        setErrorText(validation.error || "Docker folder is not valid.");
        return;
      }

      if (!validation.exists) {
        const shouldCreate = await askCreatePrompt();
        if (!shouldCreate) {
          toast.info("Docker folder was not created. Save canceled.");
          return;
        }

        await linuxio.filebrowser.resource_post.call(
          ensureTrailingSlash(normalized),
        );
        toast.success("Docker folder created.");
      }

      setDockerFolder(normalized);
      setDraft(normalized);
      toast.success("Docker folder saved.");

      if (indexerAvailable === false) {
        toast.info(
          "Indexer is unavailable. Start linuxio-indexer.service to reindex later.",
        );
        return;
      }

      const shouldReindex = await askReindexPrompt();
      if (shouldReindex) {
        setIndexerDialogOpen(true);
      }
    } catch (error: unknown) {
      const message =
        error instanceof Error ? error.message : "Failed to save Docker folder";
      setErrorText(message);
      toast.error(message);
    } finally {
      setIsSaving(false);
    }
  }, [
    askCreatePrompt,
    askReindexPrompt,
    draft,
    indexerAvailable,
    setDockerFolder,
  ]);

  return (
    <>
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          gap: theme.spacing(2),
        }}
      >
        <Typography variant="body1" fontWeight={600}>
          Docker Folder
        </Typography>
        <Typography variant="body2" color="text.secondary">
          Set the directory scanned for Docker Compose stacks.
        </Typography>

        <TextField
          label="Docker folder"
          value={draft}
          onChange={(event) => {
            setDraft(event.target.value);
            if (errorText) setErrorText(null);
          }}
          placeholder="/home/user/docker"
          fullWidth
          error={Boolean(errorText)}
          helperText={
            errorText || "Absolute path only. Root (/) is not allowed."
          }
          disabled={isSaving}
        />

        {indexerAvailable === false && (
          <Alert severity="info">
            Indexer service is unavailable. You can still save this path and
            reindex later.
          </Alert>
        )}

        <Divider />

        <div
          style={{
            display: "flex",
            justifyContent: "flex-end",
            gap: theme.spacing(1.5),
          }}
        >
          <Button onClick={handleReset} disabled={!isDirty || isSaving}>
            Reset
          </Button>
          <Button
            variant="contained"
            onClick={() => void handleSave()}
            disabled={!isDirty || isSaving}
          >
            {isSaving ? "Saving..." : "Save"}
          </Button>
        </div>
      </div>

      <ConfirmDialog
        open={createPromptOpen}
        onClose={() => resolveCreatePrompt(false)}
        onConfirm={() => resolveCreatePrompt(true)}
        title="Create Docker Folder?"
        message={`This directory does not exist yet: "${draftNormalized}". Create it now?`}
        confirmText="Create"
        cancelText="Cancel"
      />

      <ConfirmDialog
        open={reindexPromptOpen}
        onClose={() => resolveReindexPrompt(false)}
        onConfirm={() => resolveReindexPrompt(true)}
        title="Reindex Docker Folder?"
        message="Docker folder updated successfully. Start a new scan now?"
        confirmText="Scan Now"
        cancelText="Later"
      />

      <DockerIndexerDialog
        open={indexerDialogOpen}
        onClose={() => setIndexerDialogOpen(false)}
        onComplete={() => {
          toast.success("Docker folder indexed successfully.");
        }}
      />
    </>
  );
};

export default DockerFolderSettingsSection;
