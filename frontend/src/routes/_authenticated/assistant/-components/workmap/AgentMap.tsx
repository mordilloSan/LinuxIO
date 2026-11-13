import { Icon } from "@iconify/react";
import { useEffect, useRef, useState } from "react";

import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";

import MarkdownBlock from "../transcript/MarkdownBlock";
import {
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

const STATUS_TEXT: Record<RunStatus, string> = {
  run: "Running",
  ok: "Finished",
  err: "Failed",
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

/** How long a subagent has run, or took; null when that was not seen. */
function timeOf(agent: AgentRun, now: number): string | null {
  if (agent.status === "run") {
    return agent.startedAt === null
      ? null
      : formatElapsed(now - agent.startedAt);
  }
  return agent.tookMs === null ? null : formatMs(agent.tookMs);
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
 * what it was asked, what it did and what it handed back.
 */
export default function AgentMap({
  agents,
  agentLabel,
  model,
  usage,
  running,
  onMain,
}: {
  agents: AgentRun[];
  agentLabel: string;
  /** The model the chat is set to, when the agent names one. */
  model: string | null;
  usage: { used: number; size: number } | null;
  /** A turn is running. */
  running: boolean;
  /** Shows the activity map, where the main agent's work is drawn. */
  onMain: () => void;
}) {
  const busy = runningAgents(agents);
  const now = useNow(busy > 0);
  const [chosenId, setChosenId] = useState<string | null>(null);
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

  const choose = (id: string) => setChosenId(id);
  const back = () => {
    returnTo.current = chosen?.id ?? null;
    setChosenId(null);
  };

  const metaOf = (agent: AgentRun) =>
    [
      agent.status === "run"
        ? "running"
        : agent.status === "err"
          ? "failed"
          : null,
      agent.type,
      agent.background ? "background" : null,
      timeOf(agent, now),
      agent.tokens === null ? null : `${formatTokens(agent.tokens)} tokens`,
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
            onClick={() => choose(agent.id)}
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
    const tokens =
      chosen.tokens !== null
        ? `${formatTokens(chosen.tokens)} tokens`
        : chosen.status === "run"
          ? null
          : "tokens not reported";
    const latest = chosen.said.at(-1) ?? null;
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
        </div>
        <div className="agent-map__about assistant-pane-text--secondary">
          <p>
            {[
              STATUS_TEXT[chosen.status],
              chosen.type,
              chosen.model,
              chosen.background ? "background" : null,
            ]
              .filter((part) => part !== null)
              .join(" · ")}
          </p>
          <p>Started by {chosen.startedBy ?? "the main agent"}</p>
          {time || tokens ? (
            <p>{[time, tokens].filter((part) => part !== null).join(" · ")}</p>
          ) : null}
        </div>
        {latest ? <p className="agent-map__latest">{latest}</p> : null}
        {chosen.prompt ? (
          <details className="agent-map__fold">
            <summary>Prompt</summary>
            <p className="assistant-pane-text agent-map__quiet">
              {chosen.prompt}
            </p>
          </details>
        ) : null}
        <details className="agent-map__fold" open>
          <summary>Tool calls ({chosen.calls.length})</summary>
          {chosen.calls.length === 0 ? (
            <p className="agent-map__quiet">
              {chosen.status === "run" ? "Nothing yet." : "No calls seen."}
            </p>
          ) : (
            <ol className="agent-map__calls">
              {chosen.calls.map((call) => {
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
                    {note ? (
                      <span className="agent-map__call-note">{note}</span>
                    ) : null}
                  </li>
                );
              })}
            </ol>
          )}
        </details>
        {chosen.said.length > 0 ? (
          <details className="agent-map__fold">
            <summary>Messages ({chosen.said.length})</summary>
            <div className="agent-map__quiet">
              {chosen.said.map((text, index) => (
                // Messages are only ever added at the end.
                <MarkdownBlock key={index} text={text} />
              ))}
            </div>
          </details>
        ) : null}
        {chosen.agents.length > 0 ? (
          <section className="agent-map__section">
            <AppTypography component="h4" fontWeight={600} variant="body2">
              Subagents
            </AppTypography>
            {tree(chosen.agents)}
          </section>
        ) : null}
        {chosen.result ? (
          <section className="agent-map__section">
            <AppTypography component="h4" fontWeight={600} variant="body2">
              Result
            </AppTypography>
            <div className="agent-map__quiet">
              <MarkdownBlock text={chosen.result} />
            </div>
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
