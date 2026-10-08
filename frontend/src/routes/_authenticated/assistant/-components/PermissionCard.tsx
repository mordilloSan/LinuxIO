import type * as acp from "@agentclientprotocol/sdk";
import { Icon } from "@iconify/react";
import { type KeyboardEvent, useEffect, useRef } from "react";

import AppButton from "@/components/ui/AppButton";

import DiffView from "./DiffView";

const KIND_COPY: Record<string, { icon: string; question: string }> = {
  execute: { icon: "mdi:console", question: "Run this command?" },
  edit: { icon: "mdi:file-edit-outline", question: "Make this edit?" },
  delete: { icon: "mdi:delete-outline", question: "Delete this?" },
  move: { icon: "mdi:file-move-outline", question: "Move this?" },
  read: { icon: "mdi:file-eye-outline", question: "Read this?" },
  search: { icon: "mdi:magnify", question: "Run this search?" },
  fetch: { icon: "mdi:web", question: "Fetch this?" },
};

const FALLBACK = { icon: "mdi:shield-key-outline", question: "Allow this?" };

const OPTION_SELECTOR = "[data-permission-option]";

const stringField = (value: unknown, key: string) => {
  if (typeof value !== "object" || value === null) return null;
  const field = (value as Record<string, unknown>)[key];
  return typeof field === "string" && field.trim() ? field : null;
};

/**
 * The agent asking before it acts, laid out like the Claude app: what it wants
 * to do as a question, the exact command or diff, then the agent's own choices
 * as a numbered list. Focus lands on the first choice; 1–9 pick a choice, the
 * arrow keys move between them, and Escape takes the agent's "no".
 */
export default function PermissionCard({
  request,
  onAnswer,
}: {
  request: acp.RequestPermissionRequest;
  onAnswer: (optionId: string | null) => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const { toolCall, options } = request;
  const copy = KIND_COPY[toolCall.kind ?? ""] ?? FALLBACK;
  const diffs = (toolCall.content ?? []).flatMap((item) =>
    item.type === "diff" ? [item] : [],
  );
  const command = stringField(toolCall.rawInput, "command");
  const description = stringField(toolCall.rawInput, "description");
  const preview = command ?? toolCall.title ?? toolCall.toolCallId;
  const rejectIndex = options.findIndex(
    (option) => option.kind === "reject_once",
  );

  // A new request takes the keyboard, as a dialog would. Callers key the card
  // by tool call, so each request mounts afresh.
  useEffect(() => {
    ref.current?.querySelector<HTMLElement>(OPTION_SELECTOR)?.focus();
  }, []);

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    const buttons = Array.from(
      ref.current?.querySelectorAll<HTMLElement>(OPTION_SELECTOR) ?? [],
    );
    const digit = Number(event.key);
    if (Number.isInteger(digit) && digit >= 1 && digit <= options.length) {
      event.preventDefault();
      onAnswer(options[digit - 1].optionId);
      return;
    }
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      const current = buttons.indexOf(document.activeElement as HTMLElement);
      const step = event.key === "ArrowDown" ? 1 : -1;
      buttons[(current + step + buttons.length) % buttons.length]?.focus();
      return;
    }
    if (event.key === "Escape") {
      // The page would stop the whole turn; here Escape means "no".
      event.preventDefault();
      event.stopPropagation();
      onAnswer(rejectIndex === -1 ? null : options[rejectIndex].optionId);
    }
  };

  return (
    <div
      aria-label={copy.question}
      className="assistant__permission"
      onKeyDown={onKeyDown}
      ref={ref}
      role="alertdialog"
    >
      <div className="assistant__permission-head">
        <Icon height={16} icon={copy.icon} width={16} />
        <span>{copy.question}</span>
      </div>
      {description ? (
        <div className="assistant__permission-description">{description}</div>
      ) : null}
      {diffs.length > 0 ? (
        diffs.map((diff) => (
          <DiffView
            key={diff.path}
            newText={diff.newText}
            oldText={diff.oldText}
            path={diff.path}
          />
        ))
      ) : (
        <pre className="assistant__permission-command">{preview}</pre>
      )}
      <div className="assistant__permission-options">
        {options.map((option, index) => (
          <AppButton
            aria-keyshortcuts={String(index + 1)}
            className={
              option.kind.startsWith("reject")
                ? "assistant__permission-option assistant__permission-option--reject"
                : "assistant__permission-option"
            }
            data-permission-option=""
            key={option.optionId}
            onClick={() => onAnswer(option.optionId)}
          >
            <span aria-hidden="true" className="assistant__permission-key">
              {index + 1}
            </span>
            <span className="assistant__permission-label">{option.name}</span>
            {index === rejectIndex ? (
              <span aria-hidden="true" className="assistant__permission-esc">
                Esc
              </span>
            ) : null}
          </AppButton>
        ))}
      </div>
    </div>
  );
}
