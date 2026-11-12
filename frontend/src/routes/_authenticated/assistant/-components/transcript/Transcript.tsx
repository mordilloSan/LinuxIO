import type * as acp from "@agentclientprotocol/sdk/unstable";
import { Icon } from "@iconify/react";
import { type ReactNode, useRef, useState } from "react";

import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppButton from "@/components/ui/AppButton";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import { copyToClipboard } from "@/utils/clipboard";

import type { Attachment } from "../attachments";
import AttachmentChip from "../composer/AttachmentChip";
import DiffView from "./DiffView";
import MarkdownBlock from "./MarkdownBlock";
import { isReadOnlyCommand } from "./readOnlyCommand";
import {
  stringField,
  type ToolBlock,
  type TranscriptBlock,
} from "./transcript";
import { useFollowBottom } from "./useFollowBottom";

const KIND_LABEL: Record<string, string> = {
  read: "Read",
  edit: "Edit",
  delete: "Delete",
  move: "Move",
  search: "Search",
  execute: "Run",
  think: "Think",
  fetch: "Fetch",
  switch_mode: "Mode",
};

type ToolState = "active" | "failed" | "done";

const STATE_LABEL = { active: "running", failed: "failed", done: "done" };

const toolState = (status: string): ToolState =>
  status === "failed"
    ? "failed"
    : status === "pending" || status === "in_progress"
      ? "active"
      : "done";

/** Running spins, failed is an alert, done is a dot: the shape carries it. */
function StateMarker({ state }: { state: ToolState }) {
  return (
    <span
      aria-label={STATE_LABEL[state]}
      className={`assistant-tool-state assistant-tool-state--${state}`}
      role="img"
      title={STATE_LABEL[state]}
    >
      {state === "active" ? <AppCircularProgress size={10} /> : null}
      {state === "failed" ? <Icon icon="mdi:alert-circle" width={12} /> : null}
    </span>
  );
}

// Output longer than this starts clipped behind a fade.
const CLIP_LINES = 8;

function ToolOutput({ text }: { text: string }) {
  const [expanded, setExpanded] = useState(false);
  const lines = text.split("\n").length;
  const long = lines > CLIP_LINES;
  return (
    <div className="assistant-tool__io-row assistant-tool__io-row--out">
      <span className="assistant-tool__io-label">OUT</span>
      <div className="assistant-tool__io-cell">
        <pre
          className={
            long && !expanded
              ? "assistant-tool__out assistant-tool__out--clipped"
              : "assistant-tool__out"
          }
        >
          {text}
        </pre>
        {long ? (
          <AppButton
            className="assistant-tool__more"
            color="inherit"
            onClick={() => setExpanded(!expanded)}
            size="small"
          >
            {expanded ? "Show less" : `Show all ${lines} lines`}
          </AppButton>
        ) : null}
      </div>
    </div>
  );
}

const ACTING_KINDS = new Set(["edit", "delete", "move"]);

/**
 * Whether a tool call acted on the host, as opposed to looking at it. A
 * command counts as acting unless it is recognisably read-only.
 */
const actsOnHost = (block: ToolBlock) => {
  if (ACTING_KINDS.has(block.toolKind)) return true;
  if (block.toolKind !== "execute") return false;
  const command = stringField(block.rawInput, "command");
  return command === null || !isReadOnlyCommand(command);
};

/** A tool call's input, output and diffs, shown once its line is opened. */
function ToolDetail({ block }: { block: ToolBlock }) {
  const command = stringField(block.rawInput, "command");
  const hasInput =
    typeof block.rawInput === "object" &&
    block.rawInput !== null &&
    Object.keys(block.rawInput).length > 0;
  const input =
    command ?? (hasInput ? JSON.stringify(block.rawInput, null, 2) : null);
  const diffs = block.content.flatMap((c) => (c.type === "diff" ? [c] : []));
  const text = block.content
    .map((c) =>
      c.type === "content" && c.content.type === "text" ? c.content.text : "",
    )
    .filter(Boolean)
    .join("\n")
    // Agents often wrap output in a Markdown fence; the box already reads as code.
    .replace(/^```[^\n]*\n([\s\S]*?)\n?```\s*$/, "$1");
  const output =
    text ||
    (block.rawOutput !== undefined && diffs.length === 0
      ? JSON.stringify(block.rawOutput, null, 2)
      : "");
  return (
    <div className="assistant-tool__detail-body">
      {input || output ? (
        <div className="assistant-tool__io">
          {input ? (
            <div className="assistant-tool__io-row">
              <span className="assistant-tool__io-label">IN</span>
              <pre className="assistant-tool__in">{input}</pre>
            </div>
          ) : null}
          {output ? <ToolOutput text={output} /> : null}
        </div>
      ) : null}
      {diffs.map((diff) => (
        <DiffView
          key={diff.path}
          newText={diff.newText}
          oldText={diff.oldText}
          path={diff.path}
        />
      ))}
    </div>
  );
}

/**
 * One tool call as a log line: its state, what kind of step it was, and the
 * command or title. The line opens to the input, output and diffs.
 */
function ToolLine({ block }: { block: ToolBlock }) {
  const [open, setOpen] = useState(false);
  const command = stringField(block.rawInput, "command");
  const description = stringField(block.rawInput, "description");
  const note =
    description ?? (command && block.title !== command ? block.title : null);
  return (
    <div className="assistant-tool">
      <AppButton
        aria-expanded={open}
        className="assistant-tool__line"
        color="inherit"
        onClick={() => setOpen(!open)}
      >
        <StateMarker state={toolState(block.status)} />
        <span className="assistant-tool__kind">
          {KIND_LABEL[block.toolKind] ?? "Tool"}
        </span>
        {command ? (
          <span className="assistant-tool__command">{command}</span>
        ) : (
          <span className="assistant-tool__detail">{block.title}</span>
        )}
        {note ? <span className="assistant-tool__detail">{note}</span> : null}
      </AppButton>
      {open ? <ToolDetail block={block} /> : null}
    </div>
  );
}

// A long run of checks shows its newest lines; a button brings back the rest.
const LOG_VISIBLE = 5;

/** A run of consecutive steps that only looked at the host. */
function CheckLog({ tools }: { tools: ToolBlock[] }) {
  const [showAll, setShowAll] = useState(false);
  const hidden = showAll ? 0 : Math.max(0, tools.length - LOG_VISIBLE);
  return (
    <div className="assistant-log">
      {tools.length > LOG_VISIBLE ? (
        <AppButton
          aria-expanded={showAll}
          className="assistant-log__more"
          color="inherit"
          onClick={() => setShowAll(!showAll)}
          size="small"
        >
          {showAll
            ? "Show fewer steps"
            : `Show ${hidden} earlier ${hidden === 1 ? "step" : "steps"}`}
        </AppButton>
      ) : null}
      {tools.slice(hidden).map((tool) => (
        <ToolLine key={tool.id} block={tool} />
      ))}
    </div>
  );
}

/**
 * A run of consecutive steps that acted on the host, as one card, so what was
 * done to the server never hides in the log, asked for or not.
 */
function ChangeCard({ tools }: { tools: ToolBlock[] }) {
  const failed = tools.filter((tool) => tool.status === "failed").length;
  const active = tools.some((tool) => toolState(tool.status) === "active");
  const title = active
    ? "Running on this server"
    : tools.some((tool) => ACTING_KINDS.has(tool.toolKind))
      ? "Changed on this server"
      : "Ran on this server";
  return (
    <div
      aria-label={title}
      className={
        active
          ? "assistant-change assistant-change--active"
          : "assistant-change"
      }
      role="group"
    >
      <div className="assistant-change__head">
        <span>{title}</span>
        {failed > 0 ? (
          <span className="assistant-change__failed">{failed} failed</span>
        ) : null}
      </div>
      <div className="assistant-change__rows">
        {tools.map((tool) => (
          <ToolLine key={tool.id} block={tool} />
        ))}
      </div>
    </div>
  );
}

type Row =
  | { key: string; block: TranscriptBlock }
  | { key: string; tools: ToolBlock[]; acting: boolean };

/**
 * Consecutive tool blocks become runs: checks in a log, actions in a card,
 * each keyed by its first call. The call waiting for an answer is left out;
 * the permission card shows it.
 */
function groupRows(
  blocks: TranscriptBlock[],
  pendingToolCallId: string | null,
): Row[] {
  const rows: Row[] = [];
  let run: ToolBlock[] = [];
  let acting = false;
  const flush = () => {
    if (run.length > 0) {
      rows.push({ key: `group:${run[0].toolCallId}`, tools: run, acting });
    }
    run = [];
  };
  for (const block of blocks) {
    if (block.kind !== "tool") {
      flush();
      rows.push({ key: block.id, block });
      continue;
    }
    if (block.toolCallId === pendingToolCallId) continue;
    const blockActs = actsOnHost(block);
    if (blockActs !== acting) flush();
    acting = blockActs;
    run.push(block);
  }
  flush();
  return rows;
}

function UserBlock({
  text,
  attachments,
}: {
  text: string;
  attachments: Attachment[];
}) {
  return (
    <div className="assistant-block assistant-block--user">
      {attachments.length > 0 ? (
        <div className="assistant__attachments">
          {attachments.map((attachment) => (
            <AttachmentChip attachment={attachment} key={attachment.id} />
          ))}
        </div>
      ) : null}
      {text}
    </div>
  );
}

function AgentBlock({ text }: { text: string }) {
  return (
    <div className="assistant-block assistant-block--agent">
      <MarkdownBlock text={text} />
      <div className="assistant-block__actions">
        <AppActionIconButton
          ariaLabel="Copy answer"
          icon="mdi:content-copy"
          iconSize={14}
          label="Copy answer"
          onClick={() => {
            copyToClipboard(text).catch(() => {});
          }}
          size="small"
        />
      </div>
    </div>
  );
}

function ThoughtBlock({ text }: { text: string }) {
  return (
    <details className="assistant-block assistant-block--thought">
      <summary>Thinking</summary>
      {text}
    </details>
  );
}

function PlanBlock({ entries }: { entries: acp.PlanEntry[] }) {
  return (
    <details className="assistant-block assistant-block--thought">
      <summary>Plan</summary>
      <ul>
        {entries.map((entry, index) => (
          <li key={index}>
            [{entry.status}] {entry.content}
          </li>
        ))}
      </ul>
    </details>
  );
}

/**
 * One component per block so React only re-renders the block that changed:
 * the reducer keeps untouched blocks by reference, and the compiler memoises
 * each row on its props. Rendering the whole conversation per token is what
 * made streaming stutter.
 */
function TranscriptRow({ block }: { block: TranscriptBlock }) {
  switch (block.kind) {
    case "user":
      return <UserBlock attachments={block.attachments} text={block.text} />;
    case "agent":
      return <AgentBlock text={block.text} />;
    case "thought":
      return <ThoughtBlock text={block.text} />;
    case "plan":
      return <PlanBlock entries={block.entries} />;
    default:
      return null;
  }
}

export default function Transcript({
  blocks,
  sessionId,
  pendingToolCallId = null,
  label = "Conversation",
  children,
}: {
  blocks: TranscriptBlock[];
  sessionId?: string | null;
  /** The tool call a permission card is asking about, if any. */
  pendingToolCallId?: string | null;
  label?: string;
  /** Shown in place of the rows while the conversation is empty. */
  children?: ReactNode;
}) {
  const ref = useRef<HTMLDivElement>(null);
  useFollowBottom(ref, sessionId);
  return (
    <div
      className="assistant__transcript"
      role="log"
      aria-label={label}
      ref={ref}
    >
      {/* The scroller spans the page; the column keeps lines readable. */}
      <div className="assistant__transcript-column">
        {blocks.length === 0
          ? children
          : groupRows(blocks, pendingToolCallId).map((row) =>
              "tools" in row ? (
                row.acting ? (
                  <ChangeCard key={row.key} tools={row.tools} />
                ) : (
                  <CheckLog key={row.key} tools={row.tools} />
                )
              ) : (
                <TranscriptRow key={row.key} block={row.block} />
              ),
            )}
      </div>
    </div>
  );
}
