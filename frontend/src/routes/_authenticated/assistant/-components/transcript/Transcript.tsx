import type * as acp from "@agentclientprotocol/sdk/unstable";
import { Icon } from "@iconify/react";
import { type ReactNode, useRef, useState } from "react";

import AppButton from "@/components/ui/AppButton";
import AppCircularProgress from "@/components/ui/AppCircularProgress";

import type { Attachment } from "../attachments";
import AttachmentChip from "../composer/AttachmentChip";
import { fmtTokens } from "../composer/ContextGauge";
import { sentIn, type Subagent, workFor } from "../subagents";
import DiffView from "./DiffView";
import MarkdownBlock from "./MarkdownBlock";
import { isReadOnlyCommand } from "./readOnlyCommand";
import {
  type MessageBlock,
  stringField,
  type ToolBlock,
  type TranscriptBlock,
  type TurnBlock,
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
  const diffs = block.content.flatMap((c) => (c.type === "diff" ? [c] : []));
  // A diff already shows the file and its text; the raw input repeats both.
  const input =
    command ??
    (hasInput && diffs.length === 0
      ? JSON.stringify(block.rawInput, null, 2)
      : null);
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

const lineCount = (text: string) =>
  text === "" ? 0 : text.replace(/\n$/, "").split("\n").length;

/**
 * What a file tool did, in words: "Wrote /etc/motd" says more than the
 * tool's own "Edit · Write motd". Null until the agent sends the diff.
 */
function fileChange(block: ToolBlock) {
  if (block.toolKind !== "edit") return null;
  const diffs = block.content.flatMap((c) => (c.type === "diff" ? [c] : []));
  if (diffs.length === 0) return null;
  const path = [...new Set(diffs.map((diff) => diff.path))].join(", ");
  // A diff with nothing before it is a whole file written. The agent sends
  // the same for a new file and an overwrite, so this cannot say "created".
  if (diffs.some((diff) => diff.oldText != null)) {
    return { verb: "Edited", path, note: null };
  }
  const lines = diffs.reduce((n, diff) => n + lineCount(diff.newText), 0);
  return {
    verb: "Wrote",
    path,
    note: `${lines} ${lines === 1 ? "line" : "lines"}`,
  };
}

/**
 * One tool call as a log line: its state, what kind of step it was, and the
 * command or title. The line opens to the input, output and diffs.
 */
function ToolLine({ block }: { block: ToolBlock }) {
  const [open, setOpen] = useState(false);
  const command = stringField(block.rawInput, "command");
  const change = fileChange(block);
  // What the step is for, in words: the agent's description, or the title
  // when there is no command to show in its place.
  const what =
    stringField(block.rawInput, "description") ??
    (command ? null : block.title);
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
          {/* A command that only reads is a read, whatever ran it. */}
          {change
            ? change.verb
            : block.toolKind === "execute" && !actsOnHost(block)
              ? KIND_LABEL.read
              : (KIND_LABEL[block.toolKind] ?? "Tool")}
        </span>
        {change ? (
          <>
            <span className="assistant-tool__command">{change.path}</span>
            {change.note ? (
              <span className="assistant-tool__detail">{change.note}</span>
            ) : null}
          </>
        ) : (
          <>
            {what ? <span className="assistant-tool__what">{what}</span> : null}
            {command ? (
              <span
                className={
                  what
                    ? "assistant-tool__command assistant-tool__command--aside"
                    : "assistant-tool__command"
                }
              >
                {command}
              </span>
            ) : null}
          </>
        )}
      </AppButton>
      {open ? <ToolDetail block={block} /> : null}
    </div>
  );
}

const NO_SUBAGENTS: readonly Subagent[] = [];

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
          : failed > 0
            ? "assistant-change assistant-change--failed"
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
  | { key: string; tools: ToolBlock[]; acting: boolean }
  | { key: string; turn: TurnBlock }
  | { key: string; message: MessageBlock };

/**
 * Consecutive tool blocks become runs: checks in a log, actions in a card,
 * each keyed by its first call. The call waiting for an answer is left out;
 * the permission card shows it. So is a subagent call with the id of a
 * message the session sent (`sent`): the message stands for it. A turn that
 * ran in this page ends in a footer.
 */
function groupRows(
  blocks: readonly TranscriptBlock[],
  pendingToolCallId: string | null,
  sent: ReadonlySet<string>,
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
    if (block.kind === "turn") {
      flush();
      rows.push({ key: block.id, turn: block });
      continue;
    }
    if (block.kind === "message") {
      flush();
      rows.push({ key: block.id, message: block });
      continue;
    }
    if (block.kind !== "tool") {
      flush();
      rows.push({ key: block.id, block });
      continue;
    }
    if (block.toolCallId === pendingToolCallId) continue;
    if (sent.has(block.toolCallId)) continue;
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
    <div className="assistant-block">
      <MarkdownBlock text={text} />
    </div>
  );
}

const duration = (ms: number) => {
  const seconds = Math.max(1, Math.round(ms / 1000));
  return seconds < 60
    ? `${seconds} s`
    : `${Math.floor(seconds / 60)} min ${seconds % 60} s`;
};

/**
 * Under a finished answer: what the turn cost. The count shown is what the
 * model wrote; the tooltip has what it read, cached or not.
 */
function TurnFooter({ turn }: { turn: TurnBlock }) {
  const usage = turn.usage;
  const breakdown = usage
    ? [
        `Output ${usage.outputTokens.toLocaleString()}`,
        `input ${usage.inputTokens.toLocaleString()}`,
        usage.cachedReadTokens
          ? `cache read ${usage.cachedReadTokens.toLocaleString()}`
          : null,
        usage.cachedWriteTokens
          ? `cache write ${usage.cachedWriteTokens.toLocaleString()}`
          : null,
      ]
        .filter(Boolean)
        .join(", ")
    : undefined;
  return (
    <div className="assistant-turn">
      <span className="assistant-turn__stats" title={breakdown}>
        {duration(turn.durationMs)}
        {usage ? ` · ${fmtTokens(usage.outputTokens)} tokens` : ""}
      </span>
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

/** What the rows of a session and of its subagents are drawn with. */
type Scope = {
  subagents: readonly Subagent[];
  pendingToolCallId: string | null;
  onAgent?: (sessionId: string) => void;
};

/**
 * A message between an agent and a subagent. One the session sent reads
 * "Sent to" with the prompt closed, and the calls the subagent made for it
 * stand under it as they run; what the subagent said stays in the Agents
 * view. One it got is the subagent's report.
 */
function MessageRow({
  message,
  sent,
  scope,
}: {
  message: MessageBlock;
  /** The ids of the messages the session sent. */
  sent: ReadonlySet<string>;
  scope: Scope;
}) {
  const { subagents, onAgent } = scope;
  const other = message.outgoing
    ? message.recipientSessionId
    : message.senderSessionId;
  const child = subagents.find((entry) => entry.sessionId === other) ?? null;
  const name = child ? (
    <AppButton
      className="assistant-message__agent"
      color="inherit"
      onClick={() => onAgent?.(child.sessionId)}
      title="Show what this agent did"
    >
      {child.title ?? child.description ?? "subagent"}
    </AppButton>
  ) : (
    <span>a subagent</span>
  );
  if (!message.outgoing) {
    return (
      <div className="assistant-message">
        <div className="assistant-message__head">
          <Icon aria-hidden icon="mdi:arrow-left-thin" width={16} />
          {name}
          <span>reported</span>
        </div>
        {message.text ? (
          <div className="assistant-block assistant-message__report">
            <MarkdownBlock text={message.text} />
          </div>
        ) : null}
      </div>
    );
  }
  // Its calls, and the messages to the subagents it started in turn.
  const work = child
    ? workFor(child, message.messageId, sent).filter(
        (block) =>
          block.kind === "tool" ||
          (block.kind === "message" &&
            block.outgoing &&
            subagents.some(
              (entry) =>
                entry.sessionId === block.recipientSessionId &&
                entry.parentSessionId === child.sessionId,
            )),
      )
    : [];
  return (
    <div className="assistant-message">
      <div className="assistant-message__head">
        <Icon aria-hidden icon="mdi:arrow-right-thin" width={16} />
        <span>Sent to</span>
        {name}
      </div>
      {message.text ? (
        <details className="assistant-block assistant-block--thought">
          <summary>Prompt</summary>
          {message.text}
        </details>
      ) : null}
      {child && work.length > 0 ? (
        <div className="assistant-message__work">
          <Rows blocks={work} scope={scope} sent={sentIn(child.blocks)} />
        </div>
      ) : null}
    </div>
  );
}

function Rows({
  blocks,
  sent,
  scope,
}: {
  blocks: readonly TranscriptBlock[];
  sent: ReadonlySet<string>;
  scope: Scope;
}) {
  return groupRows(blocks, scope.pendingToolCallId, sent).map((row) =>
    "tools" in row ? (
      row.acting ? (
        <ChangeCard key={row.key} tools={row.tools} />
      ) : (
        <CheckLog key={row.key} tools={row.tools} />
      )
    ) : "turn" in row ? (
      <TurnFooter key={row.key} turn={row.turn} />
    ) : "message" in row ? (
      <MessageRow
        key={row.key}
        message={row.message}
        scope={scope}
        sent={sent}
      />
    ) : (
      <TranscriptRow key={row.key} block={row.block} />
    ),
  );
}

export default function Transcript({
  blocks,
  sessionId,
  pendingToolCallId = null,
  subagents = NO_SUBAGENTS,
  onAgent,
  label = "Conversation",
  children,
}: {
  blocks: TranscriptBlock[];
  sessionId?: string | null;
  /** The tool call a permission card is asking about, if any. */
  pendingToolCallId?: string | null;
  /** The chat's subagents, whose calls stand under the messages to them. */
  subagents?: readonly Subagent[];
  /** Shows one subagent, by its session id. */
  onAgent?: (sessionId: string) => void;
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
        {blocks.length === 0 ? (
          children
        ) : (
          <Rows
            blocks={blocks}
            scope={{ subagents, pendingToolCallId, onAgent }}
            sent={sentIn(blocks)}
          />
        )}
      </div>
    </div>
  );
}
