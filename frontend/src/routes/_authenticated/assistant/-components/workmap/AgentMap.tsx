import { Icon } from "@iconify/react";
import { useEffect, useRef, useState } from "react";

import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";

import type { SubagentState } from "../subagents";
import MarkdownBlock from "../transcript/MarkdownBlock";
import {
  type AgentCall,
  type AgentPart,
  type AgentRun,
  allAgents,
  formatElapsed,
  formatTokens,
  type RunStatus,
  runningAgents,
} from "./agents";
import { formatMs } from "./workMap";

import "../pane/pane.css";
import "./agent-map.css";

const STATE_TEXT: Record<SubagentState, string> = {
  running: "Running",
  waiting: "Waiting for you",
  finished: "Finished",
  stopped: "Stopped",
  failed: "Failed",
  ended: "Ended",
  disconnected: "Disconnected",
};

/** The time now, ticking each second while `ticking` holds. */
function useNow(ticking: boolean) {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    if (!ticking) return;
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [ticking]);
  return now;
}

/**
 * How long a subagent has run, or took, all its runs together; null when
 * none was seen.
 */
function timeOf(agent: AgentRun, now: number): string | null {
  if (agent.runningSince !== null) {
    return formatElapsed((agent.ranMs ?? 0) + now - agent.runningSince);
  }
  return agent.ranMs === null ? null : formatMs(agent.ranMs);
}

function Calls({ calls }: { calls: AgentCall[] }) {
  return (
    <ol className="agent-map__calls">
      {calls.map((call) => {
        const note =
          call.status === "run"
            ? "running"
            : call.status === "err"
              ? "failed"
              : call.note && `(${call.note})`;
        return (
          <li
            className={`agent-map__call agent-map__call--${call.status}`}
            key={call.id}
            title={[
              call.full,
              call.tookMs === null ? null : formatMs(call.tookMs),
            ]
              .filter((part) => part)
              .join(" · ")}
          >
            <span className="agent-map__call-tool">{call.tool}</span>
            <span
              className={`agent-map__call-detail${call.code ? " agent-map__call-detail--code" : ""}`}
            >
              {call.detail}
            </span>
            {note ? <span className="agent-map__call-note">{note}</span> : null}
          </li>
        );
      })}
    </ol>
  );
}

const numbered = (name: string, number: number | null) =>
  number === null ? name : `${name} ${number}`;

/** One stretch of what a subagent did: a prompt, calls, messages, a report. */
function Part({ part }: { part: AgentPart }) {
  switch (part.kind) {
    case "prompt":
      return (
        <details className="agent-map__fold">
          <summary>{numbered("Prompt", part.number)}</summary>
          <p className="assistant-pane-text agent-map__quiet">{part.text}</p>
        </details>
      );
    case "calls":
      return (
        <details className="agent-map__fold" open>
          <summary>Tool calls ({part.calls.length})</summary>
          <Calls calls={part.calls} />
        </details>
      );
    case "messages":
      return (
        <details className="agent-map__fold">
          <summary>Messages ({part.texts.length})</summary>
          <div className="agent-map__quiet">
            {part.texts.map((text, index) => (
              // Messages are only ever added at the end.
              <MarkdownBlock key={index} text={text} />
            ))}
          </div>
        </details>
      );
    case "report":
      return (
        <details className="agent-map__fold" open>
          <summary>{numbered("Report", part.number)}</summary>
          <div className="agent-map__quiet">
            <MarkdownBlock text={part.text} />
          </div>
        </details>
      );
  }
}

function Dot({ status }: { status: RunStatus }) {
  return (
    <span aria-hidden className={`agent-map__dot agent-map__dot--${status}`} />
  );
}

function Node({
  label,
  meta,
  status,
  main = false,
  title,
  onClick,
  nodeRef,
}: {
  label: string;
  meta: string;
  status: RunStatus;
  main?: boolean;
  title: string;
  onClick: () => void;
  nodeRef?: (node: HTMLButtonElement | null) => void;
}) {
  return (
    <AppButton
      className={`agent-map__node${main ? " agent-map__node--main" : ""}`}
      color="inherit"
      onClick={onClick}
      ref={nodeRef}
      title={title}
    >
      <span className="agent-map__node-head">
        <Dot status={status} />
        <span className="agent-map__node-label">{label}</span>
      </span>
      {meta === "" ? null : (
        <span className="agent-map__node-meta">{meta}</span>
      )}
    </AppButton>
  );
}

const plural = (count: number, one: string) =>
  `${count} ${one}${count === 1 ? "" : "s"}`;

/**
 * The chat's agents: the one the chat talks to, with the subagents it sent
 * off beside it and the ones they sent off under them. A subagent opens to
 * everything it was asked, did, said and reported, in the order it happened.
 */
export default function AgentMap({
  agents,
  agentLabel,
  model,
  usage,
  running,
  chosenId,
  onChoose,
  onStop,
  onMain,
}: {
  agents: AgentRun[];
  agentLabel: string;
  /** The model the chat is set to, when the agent names one. */
  model: string | null;
  usage: { used: number; size: number } | null;
  /** A turn is running. */
  running: boolean;
  /** The subagent that is open; null shows them all. */
  chosenId: string | null;
  onChoose: (id: string | null) => void;
  /** Stops a subagent that runs and can be stopped. */
  onStop: (id: string) => void;
  /** Shows the activity map, where the main agent's work is drawn. */
  onMain: () => void;
}) {
  const busy = runningAgents(agents);
  const now = useNow(busy > 0);
  const everyone = allAgents(agents);
  // A chat switched under an open subagent goes back to the list.
  const chosen = everyone.find((agent) => agent.id === chosenId) ?? null;
  const shownId = chosen?.id ?? null;
  const backRef = useRef<HTMLButtonElement>(null);
  const nodes = useRef(new Map<string, HTMLButtonElement>());
  const returnTo = useRef<string | null>(null);

  // Focus follows the view: to the way back when a subagent opens, and to
  // that subagent when the list comes back.
  useEffect(() => {
    if (shownId !== null) {
      backRef.current?.focus();
      return;
    }
    const id = returnTo.current;
    returnTo.current = null;
    if (id !== null) nodes.current.get(id)?.focus();
  }, [shownId]);

  const back = () => {
    returnTo.current = chosen?.id ?? null;
    onChoose(null);
  };

  // A subagent named by its role alone says it once.
  const typeOf = (agent: AgentRun) =>
    agent.type === agent.label ? null : agent.type;
  const metaOf = (agent: AgentRun) =>
    [
      agent.state === "finished" ? null : STATE_TEXT[agent.state].toLowerCase(),
      typeOf(agent),
      timeOf(agent, now),
    ]
      .filter((part) => part !== null)
      .join(" · ");

  const tree = (list: AgentRun[]) => (
    <ol className="agent-map__children">
      {list.map((agent) => (
        <li key={agent.id}>
          <Node
            label={agent.label}
            meta={metaOf(agent)}
            nodeRef={(node) => {
              if (node) nodes.current.set(agent.id, node);
              else nodes.current.delete(agent.id);
            }}
            onClick={() => onChoose(agent.id)}
            status={agent.status}
            title="Show what this agent did"
          />
          {agent.agents.length > 0 ? tree(agent.agents) : null}
        </li>
      ))}
    </ol>
  );

  if (chosen) {
    const time = timeOf(chosen, now);
    // No agent says what a subagent used.
    const tokens = chosen.status === "run" ? null : "tokens not reported";
    // What it said last, while it works; all of it is under Messages.
    const last = chosen.parts.at(-1);
    const latest =
      chosen.status === "run" && last?.kind === "messages"
        ? (last.texts.at(-1) ?? null)
        : null;
    return (
      <div className="agent-map agent-map--detail">
        <AppButton
          className="agent-map__back"
          keepTextOnMobile
          onClick={back}
          ref={backRef}
          size="small"
          startIcon={<Icon icon="mdi:arrow-left" width={16} />}
        >
          All agents
        </AppButton>
        <div className="agent-map__heading">
          <Dot status={chosen.status} />
          <AppTypography component="h3" fontWeight={600}>
            {chosen.label}
          </AppTypography>
          {chosen.canStop ? (
            <AppButton
              className="agent-map__stop"
              keepTextOnMobile
              onClick={() => onStop(chosen.id)}
              size="small"
              startIcon={<Icon icon="mdi:stop" width={16} />}
            >
              Stop
            </AppButton>
          ) : null}
        </div>
        <div className="agent-map__about assistant-pane-text--secondary">
          <p>
            {[STATE_TEXT[chosen.state], typeOf(chosen), chosen.model]
              .filter((part) => part !== null)
              .join(" · ")}
          </p>
          {chosen.error ? <p>{chosen.error}</p> : null}
          <p>Started by {chosen.startedBy ?? "the main agent"}</p>
          {time || tokens ? (
            <p>{[time, tokens].filter((part) => part !== null).join(" · ")}</p>
          ) : null}
          {chosen.cwd ? (
            <p>
              In <code>{chosen.cwd}</code>
            </p>
          ) : null}
        </div>
        {latest ? <p className="agent-map__latest">{latest}</p> : null}
        {chosen.parts.length === 0 ? (
          <p className="agent-map__quiet">
            {chosen.status === "run" ? "Nothing yet." : "Nothing seen."}
          </p>
        ) : (
          chosen.parts.map((part) => <Part key={part.id} part={part} />)
        )}
        {chosen.agents.length > 0 ? (
          <section className="agent-map__section">
            <AppTypography component="h4" fontWeight={600} variant="body2">
              Subagents
            </AppTypography>
            {tree(chosen.agents)}
          </section>
        ) : null}
      </div>
    );
  }

  const count = everyone.length;
  return (
    <div className="agent-map">
      <p className="assistant-pane-text assistant-pane-text--dim">
        {count === 0
          ? "No subagents in this chat yet."
          : `${plural(count, "agent")}${busy > 0 ? ` · ${busy} running` : ""} · select one for details`}
      </p>
      <div className="agent-map__tree">
        <Node
          label={agentLabel}
          main
          meta={[
            model?.toLowerCase(),
            usage ? `${formatTokens(usage.used)} tokens in context` : null,
          ]
            .filter(Boolean)
            .join(" · ")}
          onClick={onMain}
          status={running ? "run" : "ok"}
          title="Show the activity map"
        />
        {count > 0 ? tree(agents) : null}
      </div>
    </div>
  );
}
