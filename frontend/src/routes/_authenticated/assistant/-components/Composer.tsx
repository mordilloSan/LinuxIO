import type * as acp from "@agentclientprotocol/sdk";
import { Icon } from "@iconify/react";
import {
  type ChangeEvent,
  type ClipboardEvent,
  type DragEvent,
  type KeyboardEvent,
  useLayoutEffect,
  useRef,
  useState,
} from "react";

import { call, linuxio } from "@/api";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppChip from "@/components/ui/AppChip";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppIconButton from "@/components/ui/AppIconButton";
import AppMenu, { AppMenuItem } from "@/components/ui/AppMenu";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import PathPickerField from "@/components/ui/PathPickerField";

import type { AssistantState, MessageDisplay } from "./assistant-store";
import {
  type Attachment,
  attachmentName,
  buildPrompt,
  fileAttachment,
  imageAttachment,
} from "./attachments";
import ConfigControls from "./ConfigControls";
import ContextGauge from "./ContextGauge";
import ModeMenu from "./ModeMenu";
import { useElapsed } from "./useElapsed";

export interface ComposerProps {
  disabled: boolean;
  running: boolean;
  canSteer: boolean;
  canImages: boolean;
  canEmbed: boolean;
  canFork: boolean;
  commands: acp.AvailableCommand[];
  configOptions: acp.SessionConfigOption[];
  modes: acp.SessionModeState | null;
  connecting: boolean;
  turnStartedAt: number | null;
  usage: AssistantState["usage"];
  onBtw: (text: string) => void;
  onSetConfigOption: (configId: string, value: string | boolean) => void;
  onSetMode: (modeId: string) => void;
  onSend: (blocks: acp.ContentBlock[], display: MessageDisplay) => void;
  onSteer: (blocks: acp.ContentBlock[], display: MessageDisplay) => void;
  onStop: () => void;
}

const SLASH_PREFIX = /^\/(\S*)$/;
const BTW = /^\/btw(?:\s+([\s\S]*))?$/;
const MAX_LINES = 10;

// Handled by the composer itself, never sent to the agent.
const BTW_COMMAND: acp.AvailableCommand = {
  name: "btw",
  description: "Ask a side question without touching this chat",
  input: { hint: "<question>" },
};

// Grows the textarea with its content, up to 10 lines or 40% of the viewport.
function fitHeight(element: HTMLInputElement | HTMLTextAreaElement | null) {
  if (!(element instanceof HTMLTextAreaElement)) return;
  const style = getComputedStyle(element);
  const lineHeight = parseFloat(style.lineHeight) || 20;
  const padding =
    parseFloat(style.paddingTop) + parseFloat(style.paddingBottom) || 0;
  const max = Math.min(MAX_LINES * lineHeight, 0.4 * window.innerHeight);
  // Pin the box while the textarea collapses so the page does not jump.
  const box = element.closest<HTMLElement>(".assistant__composer-box");
  if (box) box.style.minHeight = `${box.offsetHeight}px`;
  element.style.height = "auto";
  element.style.height = `${Math.min(element.scrollHeight - padding, max)}px`;
  if (box) box.style.minHeight = "";
}

function SendButton({
  running,
  disabled,
  onClick,
}: {
  running: boolean;
  disabled: boolean;
  onClick: () => void;
}) {
  return (
    <AppIconButton
      aria-label={running ? "Stop" : "Send"}
      className="assistant__send"
      disabled={disabled}
      onClick={onClick}
    >
      <Icon
        height={18}
        icon={running ? "mdi:stop" : "mdi:arrow-up"}
        width={18}
      />
    </AppIconButton>
  );
}

const readText = async (path: string) =>
  (await call(linuxio.filebrowser.read_text.route, { path })).content;

const errorText = (error: unknown) =>
  error instanceof Error ? error.message : String(error);

const imageFiles = (files: FileList | null | undefined) =>
  Array.from(files ?? []).filter((file) => file.type.startsWith("image/"));

export default function Composer({
  disabled,
  running,
  canSteer,
  canImages,
  canEmbed,
  canFork,
  commands: agentCommands,
  configOptions,
  modes,
  connecting,
  turnStartedAt,
  usage,
  onBtw,
  onSetConfigOption,
  onSetMode,
  onSend,
  onSteer,
  onStop,
}: ComposerProps) {
  const [draft, setDraft] = useState("");
  const [attachments, setAttachments] = useState<Attachment[]>([]);
  const [attachError, setAttachError] = useState<string | null>(null);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [slashIndex, setSlashIndex] = useState(0);
  const [slashDismissedFor, setSlashDismissedFor] = useState<string | null>(
    null,
  );
  const [commandsAnchor, setCommandsAnchor] = useState<HTMLElement | null>(
    null,
  );
  const commandsOpen = commandsAnchor !== null;
  const [attachAnchor, setAttachAnchor] = useState<HTMLElement | null>(null);
  const [anchor, setAnchor] = useState<HTMLDivElement | null>(null);
  const inputRef = useRef<HTMLInputElement | HTMLTextAreaElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const elapsed = useElapsed(running ? turnStartedAt : null);

  const commands = [
    BTW_COMMAND,
    ...agentCommands.filter((command) => command.name !== BTW_COMMAND.name),
  ];
  // Attachments need a send that can actually happen.
  const locked = disabled || (running && !canSteer);

  const slashPrefix = SLASH_PREFIX.exec(draft)?.[1];
  const slashMatches =
    slashPrefix === undefined
      ? []
      : commands.filter((command) => command.name.startsWith(slashPrefix));
  const slashOpen =
    !disabled && slashMatches.length > 0 && slashDismissedFor !== draft;
  // The / button lists every command regardless of the draft.
  const listedCommands = commandsOpen ? commands : slashMatches;
  const activeSlash = Math.min(slashIndex, slashMatches.length - 1);
  const hint = commands.find((command) => draft === `/${command.name} `)?.input
    ?.hint;

  // After every render, so a send clearing the draft or a command filling it in also resizes.
  useLayoutEffect(() => fitHeight(inputRef.current));

  const changeDraft = (value: string) => {
    setAttachError(null);
    setSlashIndex(0);
    // Typing "@" at the start of a word opens the file picker instead.
    const typed = value.length === draft.length + 1 && value.endsWith("@");
    if (typed && (value.length === 1 || /\s/.test(value.at(-2) ?? ""))) {
      setDraft(value.slice(0, -1));
      setPickerOpen(true);
      return;
    }
    setDraft(value);
  };

  const insertCommand = (command: acp.AvailableCommand) => {
    setCommandsAnchor(null);
    setDraft(`/${command.name} `);
    inputRef.current?.focus();
  };

  const submit = () => {
    const text = draft.trim();
    if (!text || disabled) return;
    const btw = BTW.exec(text);
    if (btw) {
      if (!canFork) {
        setAttachError("This agent cannot open side questions");
        return;
      }
      setDraft("");
      onBtw(btw[1] ?? "");
      return;
    }
    // A turn the agent cannot steer takes nothing but /btw until it ends.
    if (running && !canSteer) return;
    const display = {
      text,
      attachments: attachments.map(attachmentName),
    };
    const blocks = buildPrompt(text, attachments);
    setDraft("");
    setAttachments([]);
    setPickerOpen(false);
    if (running) onSteer(blocks, display);
    else onSend(blocks, display);
  };

  const addImages = async (files: File[]) => {
    const results = await Promise.allSettled(files.map(imageAttachment));
    setAttachError(null);
    const failed = results.find((r) => r.status === "rejected");
    if (failed) setAttachError(errorText(failed.reason));
    setAttachments((current) => [
      ...current,
      ...results.flatMap((r) => (r.status === "fulfilled" ? [r.value] : [])),
    ]);
  };

  const pickFile = async (path: string) => {
    setPickerOpen(false);
    const added = await fileAttachment(
      path,
      canEmbed ? readText : () => Promise.reject(new Error("no embed")),
    );
    setAttachments((current) => [...current, added]);
  };

  const chooseImages = (event: ChangeEvent<HTMLInputElement>) => {
    const files = imageFiles(event.target.files);
    // Reset so choosing the same file again still fires a change.
    event.target.value = "";
    if (files.length > 0) void addImages(files);
  };

  const onKeyDown = (event: KeyboardEvent) => {
    if (event.key === "Tab" && event.shiftKey && modes) {
      // bypassPermissions stays a deliberate pick from the menu.
      const cycle = modes.availableModes.filter(
        (m) => m.id !== "bypassPermissions",
      );
      if (cycle.length > 1 && !connecting) {
        event.preventDefault();
        const index = cycle.findIndex((m) => m.id === modes.currentModeId);
        onSetMode(cycle[(index + 1) % cycle.length].id);
        return;
      }
    }
    if (slashOpen) {
      if (event.key === "ArrowDown" || event.key === "ArrowUp") {
        event.preventDefault();
        const step = event.key === "ArrowDown" ? 1 : -1;
        setSlashIndex(
          (activeSlash + step + slashMatches.length) % slashMatches.length,
        );
        return;
      }
      if ((event.key === "Enter" && !event.shiftKey) || event.key === "Tab") {
        event.preventDefault();
        insertCommand(slashMatches[activeSlash]);
        return;
      }
      if (event.key === "Escape") {
        event.preventDefault();
        event.stopPropagation();
        setSlashDismissedFor(draft);
        return;
      }
    }
    // Escape while running bubbles to the page, which stops the turn.
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      submit();
    }
  };

  const onPaste = (event: ClipboardEvent) => {
    const files = imageFiles(event.clipboardData.files);
    if (!canImages || files.length === 0) return;
    event.preventDefault();
    void addImages(files);
  };

  const onDragOver = (event: DragEvent) => {
    if (canImages && event.dataTransfer.types.includes("Files")) {
      event.preventDefault();
    }
  };

  const onDrop = (event: DragEvent) => {
    const files = imageFiles(event.dataTransfer.files);
    if (!canImages || files.length === 0) return;
    event.preventDefault();
    void addImages(files);
  };

  const remove = (target: Attachment) =>
    setAttachments((current) =>
      current.filter((item) => item.id !== target.id),
    );

  return (
    <div className="assistant__composer-stack">
      {attachments.length > 0 ? (
        <div className="assistant__attachments">
          {attachments.map((attachment) =>
            attachment.kind === "image" ? (
              <div className="assistant__thumb" key={attachment.id}>
                <img alt={attachment.name} src={attachment.previewUrl} />
                <AppActionIconButton
                  ariaLabel={`Remove ${attachment.name}`}
                  className="assistant__thumb-remove"
                  icon="mdi:close"
                  iconSize={14}
                  onClick={() => remove(attachment)}
                />
              </div>
            ) : (
              <AppChip
                key={attachment.id}
                label={attachmentName(attachment)}
                onDelete={() => remove(attachment)}
                size="small"
                title={attachment.path}
              />
            ),
          )}
        </div>
      ) : null}
      {pickerOpen ? (
        <PathPickerField
          includeFiles
          label="Attach a file"
          onChange={(path) => void pickFile(path)}
          onPickerClose={() => setPickerOpen(false)}
          placeholder="Click to choose a file"
          selectableTypes={["file"]}
          value=""
        />
      ) : null}
      <div className="assistant__composer">
        <div
          className="assistant__composer-box"
          onDragOver={onDragOver}
          onDrop={onDrop}
          onPaste={onPaste}
          ref={setAnchor}
        >
          <AppTextField
            aria-keyshortcuts="Shift+Tab"
            aria-label="Message"
            disabled={disabled}
            fullWidth
            size="small"
            helperText={hint}
            multiline
            onChange={(event) => changeDraft(event.target.value)}
            onKeyDown={onKeyDown}
            placeholder="Ask the agent to look at something on this server"
            ref={inputRef}
            rows={1}
            value={draft}
            variant="standard"
          />
          <div className="assistant__composer-bar">
            <div className="assistant__bar-group">
              <AppActionIconButton
                ariaLabel="Attach"
                disabled={locked}
                icon="mdi:plus"
                iconSize={20}
                label="Attach"
                onClick={(event) => setAttachAnchor(event.currentTarget)}
              />
              <AppActionIconButton
                ariaLabel="Commands"
                disabled={disabled || commands.length === 0}
                icon="mdi:slash-forward-box"
                iconSize={20}
                label="Commands"
                onClick={(event) => setCommandsAnchor(event.currentTarget)}
              />
              {running ? (
                <>
                  <AppCircularProgress size={14} />
                  <span className="assistant__bar-dim assistant__bar-time">
                    <Icon height={14} icon="mdi:clock-outline" width={14} />
                    {elapsed}
                  </span>
                </>
              ) : null}
              <ContextGauge usage={usage} />
            </div>
            <div className="assistant__bar-group assistant__bar-group--config">
              <ConfigControls
                disabled={connecting}
                onChange={onSetConfigOption}
                widthAnchor={anchor}
                // The agent may list the mode both as a config option and in `modes`.
                options={
                  modes
                    ? configOptions.filter(
                        (option) => option.category !== "mode",
                      )
                    : configOptions
                }
              />
            </div>
            <div className="assistant__bar-group">
              {modes?.availableModes.length ? (
                <ModeMenu
                  disabled={connecting}
                  modes={modes}
                  onSetMode={onSetMode}
                />
              ) : null}
              <SendButton
                disabled={running ? false : locked || !draft.trim()}
                onClick={running ? onStop : submit}
                running={running}
              />
            </div>
          </div>
        </div>
      </div>
      {attachError ? (
        <AppTypography color="error" variant="caption">
          {attachError}
        </AppTypography>
      ) : null}
      <input
        accept="image/*"
        aria-label="Add image"
        hidden
        multiple
        onChange={chooseImages}
        ref={fileInputRef}
        type="file"
      />
      <AppMenu
        anchorEl={attachAnchor}
        anchorOrigin={{ vertical: "top", horizontal: "left" }}
        ariaLabel="Attach"
        onClose={() => setAttachAnchor(null)}
        open={attachAnchor !== null}
        transformOrigin={{ vertical: "bottom", horizontal: "left" }}
      >
        {canImages ? (
          <AppMenuItem
            onClick={() => {
              setAttachAnchor(null);
              fileInputRef.current?.click();
            }}
            startAdornment={<Icon icon="mdi:image-outline" />}
          >
            Add image…
          </AppMenuItem>
        ) : null}
        <AppMenuItem
          onClick={() => {
            setAttachAnchor(null);
            setPickerOpen(true);
          }}
          startAdornment={<Icon icon="mdi:paperclip" />}
        >
          Attach file…
        </AppMenuItem>
      </AppMenu>
      <AppMenu
        anchorEl={commandsAnchor ?? anchor}
        anchorOrigin={{ vertical: "top", horizontal: "left" }}
        ariaLabel="Slash commands"
        autoFocus={commandsOpen}
        className="assistant__slash-menu"
        onClose={() => {
          setCommandsAnchor(null);
          setSlashDismissedFor(draft);
        }}
        open={slashOpen || commandsOpen}
        transformOrigin={{ vertical: "bottom", horizontal: "left" }}
      >
        {listedCommands.map((command, index) => (
          <AppMenuItem
            key={command.name}
            onClick={() => insertCommand(command)}
            onMouseDown={(event) => event.preventDefault()}
            selected={!commandsOpen && index === activeSlash}
          >
            <span className="assistant__slash-name">/{command.name}</span>
            {command.description ? (
              <span className="assistant__slash-description">
                {command.description}
              </span>
            ) : null}
          </AppMenuItem>
        ))}
      </AppMenu>
    </div>
  );
}
