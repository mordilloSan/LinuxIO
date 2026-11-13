# Native subagents in the Assistant

Plan, not built. It replaces how the Assistant learns about Claude Code's
subagents: today it works them out from tags on the main chat's updates,
and afterwards it would get each subagent as a session of its own from
the adapter. The behaviour as it is today is in
[AI Assistant](../AI-assistant.md) (Agents).

## Why

The Agents view reads subagents out of the main chat's transcript:
`_meta.claudeCode.parentToolUseId` on a subagent's calls and text,
`toolResponse` totals on the launch call, the `SubagentHandback` call as
the report, and the turn being held open as the sign that a background
subagent still runs. That leaves gaps:

- A loaded chat has none of a subagent's own work: no calls, messages or
  report. The adapter replays a subagent's history only to a client that
  declares native subagent support.
- A background subagent's end is guessed from the turn, and its time from
  its last call.
- The model shows only when the launch call names one.
- A subagent cannot be stopped on its own.

The adapter LinuxIO pins (`@agentclientprotocol/claude-agent-acp` 0.102.0,
`backend/bridge/internal/acpagents/acpagents.go`) supports ACP's unstable
subagent sessions, which it calls the RFD form. A client that declares
them gets each subagent as a child session, live and on load:

- its calls, text and thinking;
- its prompt and report as messages between sessions;
- its state (running, waiting for the user, idle and why);
- its working directory;
- the model it actually ran on;
- a way to cancel it.

## What the adapter does in the RFD form

Read from the adapter's source; nothing here has been seen in a running
chat. References are to `claude-agent-acp` 0.102.0 `dist/`. `A` is
`acp-agent.js`, `N` `native-subagents.js`, `R` `subagent-replay.js`.
Types are in `@agentclientprotocol/sdk/unstable` (v1), which the frontend
already imports: `SubagentUpdate`, `StateUpdate`, `SessionMessage`, and
the `subagent_update` and `session_message` kinds of `SessionUpdate`.

The form is unstable, so a new adapter pin can change any of this. Read
this section against the new source when the pin moves.

### Declaring it

- `clientCapabilities.subagents: {}`. Any object counts
  (`acp-subagents.js:9-14`). No `_meta.jetbrains.air`, which would select
  the JetBrains form instead.
- It also makes Claude Code register the SubagentStart, SubagentStop,
  PreToolUse and PostToolUse hooks, and gives Claude Code a per-task stop
  (A:9250-9268).

### Updates

- **`subagent_update`**, sent on the parent's session with `sessionId` set
  to the child's (N:1405-1418, N:999-1005). A child that starts its own
  subagent is the parent of that one. The id is Claude Code's agent id and
  stays the same across resumes.
  - The announcement carries `title`, `description` (the subagent type
    when it differs from the title), `cwd` when it differs from the
    parent's, `capabilities: {cancel: {}}` and `state: running`.
  - Later patches carry only what changed: `cwd` (`null` once it is back
    in the parent's folder), or `state`. In the schema a field that is
    left out is unchanged and `null` clears it, `capabilities` included.
  - The states are:
    - `running`;
    - `requires_action` while a permission request of the child is open;
    - `idle` with `stopReason` `end_turn`, `cancelled` or `error` (with
      `error.message`), or with none for an end the adapter cannot
      explain, such as a child that still ran when the stream ended
      (N:646-655);
    - `unknown`, meaning disconnected.
  - Idle does not end a child: the parent can message it again, and it
    goes back to `running` in the same session.
- **The child's own work** (its tool calls, text and thinking) comes as
  ordinary `session/update` notifications on the child's session id
  (N:1011-1043, A:5832-5841). Updates that arrive before the announcement
  are held back until it, with caps.
  - Only the session id changes. Each update keeps its
    `_meta.claudeCode.parentToolUseId`, the id of the launch call, live
    and on load (N:1011-1021, A:7507-7530). The launch call itself is not
    in the child's session.
- **`config_option_update` on the child's session**: read-only selects
  for `mode`, `model`, `effort` and `agent` (the subagent type)
  (N:219-247, N:1363-1375). Each one is the child's whole list. The model
  is the one the child's first response came from, and it is sent again
  when it changes (N:151-178). It is not known before that first
  response.
- **`session_message`** links the sessions. Its fields are `messageId`,
  `senderSessionId`, `recipientSessionId`, `senderMessageId`,
  `recipientMessageId` and `content`. It is an upsert by `messageId`: a
  field that is left out keeps its value. The two copies of a pair have
  the same `messageId`, one on each session (N:1521-1539).
  - The launch prompt is a linked pair: the parent's outgoing copy, whose
    `messageId` is the launch call's id, and the child's incoming copy
    (N:1423-1425, N:1521-1539).
  - The report is a pair from child to parent made from the
    `SubagentHandback` call's `message` (N:1113-1142).
  - When a foreground launch ends without a hand-back, the parent gets a
    `<launchId>:reply` message from the child (N:1092-1098). It has no
    copy in the child: the child's answer is its own agent text.
  - A `SendMessage` that reached an announced child becomes a pair as
    well (N:1151-1178). So a child can have several incoming messages
    and several reports.
- **Hidden calls.** The parent's `Agent`/`Task` launch call, a
  `SendMessage` to an announced child and the child's `SubagentHandback`
  never reach the client as tool calls (N:1071-1142). The exceptions:
  - A launch whose child was never announced is shown after all, as an
    ordinary tool call with its final status (N:1099-1105). What the
    child sent before that is dropped, so the call has no calls or text
    under it.
  - A launch call that a permission request already showed keeps getting
    its updates as a tool call, while its child is announced too and the
    prompt pair is still sent (N:1077-1080).
  - A hand-back that failed is shown as a tool call in the child, and a
    `SendMessage` that reached no announced child as a tool call of its
    sender.

### Requests

- **Cancel** a child with `session/cancel` on its session id
  (A:6494-6515). It stops only that child's task. Accepting the cancel
  reports nothing; the child goes `idle` with `cancelled` afterwards.
- **Permission requests** from an announced child carry the child's
  session id (A:8013-8023). An open one keeps the parent's turn waiting
  (A:7884-7887), so a client that refuses or ignores it leaves the chat
  stuck until the user cancels. Requests are not queued: several can be
  open at once, one per child that asks (A:7651-7658).
- A child cannot be prompted or configured: `session/prompt` and
  `set_config_option` on its id fail with "session not found"
  (A:2166-2168, A:7076-7078). The only capability is `cancel`.

### Background subagents

A launch with `run_in_background` ends at once and stays hidden. The child
is announced `running` when its task starts, and goes `idle` when the
task's end is reported (A:4721-4809, N:583-645). The parent's prompt
response is still held until the background subagents it started settle,
as today (A:3446-3507). Not confirmed:

- Whether cancelling the parent spares its background children. The
  adapter's comments disagree (N:683-685 against A:9258-9265), and
  upstream has it open (agent-client-protocol#2308).
- Whether a subagent Claude Code moves to the background by itself (no
  `run_in_background` on the call) gets its launch note sent as a
  `:reply` message.

### Loading a chat

`session/load` replays the parent's transcript and, right after the
assistant message that launched each child, the child's announcement (no
capabilities yet), its model, and its own transcript from
`<session>/subagents/agent-<id>.jsonl`, nested children included. The
launch's result can produce the `:reply` message. After the whole
transcript, each child's end state follows. After the load response, each
child gets a current `subagent_update`: `cancel` only if it still runs
in this process, `cwd: null` if its folder is gone (R:88-335,
A:7548-7561, A:8532-8571). A launch without its `meta.json` replays as a
plain tool call with no history (R:328-335). LinuxIO opens chats with
`session/load` only; `session/resume` is used for the side question's
fork.

- Every child is announced `running` during the replay (R:100-105). Its
  real state is the one that follows the transcript.
- A child with no recorded end is `idle` with no stop reason (R:423-425).
- A loaded child gets only the `model` option (R:138-148), not `mode`,
  `effort` or `agent`.
- A comment at A:7171-7174 says the RFD form leaves a child's text out of
  a replay. The code sends it to the child's session (A:7530).

### What it does not give

- **Tokens and time per subagent.** `usage_update` is for the root
  session only, `IdleStateUpdate.usage` exists in the schema but is never
  set (N:1609-1629), and the launch call that carries the totals today is
  hidden. Every subagent reads `tokens not reported`, and its time is
  what LinuxIO saw, not the subagent's own account.
- **Whether a subagent was sent to the background.** The launch call is
  hidden. Its state takes the place of this label.

## LinuxIO today

- `frontend/src/api/acp/connection.ts`: `initialize` declares
  `auth.terminal` and boolean config options only (around line 186). It
  handles `session/request_permission`, `session/update` and
  `_auth/status_update`, and nothing else. `cancel(sessionId)` takes any
  session id.
- `frontend/src/routes/_authenticated/assistant/-components/assistant-store.ts`:
  - `flushUpdates` splits each batch by session, keeps the updates of
    the open chat and of the side question, and drops every other
    session's (around line 191);
  - `onPermission` answers a request from any other session `cancelled`
    (around line 540);
  - the chat has one `pending` request, and a new one answers the open
    one `cancelled`.

  Under the RFD form, the first two would lose a child's work and refuse
  its prompts, and the third would deny one child's request when another
  child asks.
- `transcript/transcript.ts` keeps a tagged subagent's text on its launch
  call (`said`), its totals (`totals`), the background status
  (`launched`) and the parent of each call (`parentId`).
  `applySessionUpdate` drops tagged text whose launch call is not in the
  list, and all tagged thinking.
  `workmap/agents.ts` builds the agents from those, with the background
  and hand-back rules. `workmap/AgentMap.tsx` draws them.
- A subagent's calls are in the chat's own blocks. Only `agents.ts` reads
  `parentId`, so the transcript shows them among the main agent's calls
  and `summarize` in `workmap/workMap.ts` counts them at every station.
- `workmap/workMap.ts` counts launch calls at the Agents station of the
  map, and the `N agents` tab counts the running ones.
- The bridge passes the agent's stdio through as bytes
  (`backend/bridge/handlers/assistant`), so nothing changes outside the
  frontend.

## Steps

Step 1 comes before any building. Steps 2 to 9 land as one change: with
the capability declared and any of them missing, a chat with subagents
loses work or waits on a request nobody sees. The capability is declared
last.

1. **Capture a live trace.** On a throwaway build, declare the capability
   and log every raw `session/update` and permission request. Run a chat
   with a subagent in the foreground, one in the background, a nested
   one, a `SendMessage` to a finished one, a permission request, a cancel
   and a reload. Correct this plan where the trace differs, settle the
   two points under Background subagents, and keep the trace as the
   fixture for the tests. Nothing of the throwaway build is kept.
2. **Keep the children in the store.** Add the open chat's subagent
   sessions to `AssistantState`, keyed by child session id:
   - title and description;
   - the parent session id (the session the `subagent_update` came on);
   - `cwd`;
   - state and stop reason;
   - whether it can be cancelled (`capabilities: null` takes it away);
   - its config options;
   - its blocks, built with `applySessionUpdate`;
   - its runs: each time it was seen to go `running` and then idle. A
     child that is messaged again has more than one.

   `applySessionUpdate` stops reading the `parentToolUseId` tag: the tag
   stays on a child's updates, and the routing would drop the child's
   text and thinking from its own blocks. Remove `said`, `parentId` and
   the routing here. Nothing else needs them: a launch that is still
   shown has no child text.

   In `flushUpdates`, walk the batch in the order it arrived, not session
   by session: a nested child is announced on its parent child's
   session, and can share a batch with it.
   - a `subagent_update` on the open chat, or on a known child, adds or
     patches a child;
   - an update on a known child's id goes to that child's blocks and
     config options, and never through `statePatch`, which would put the
     child's options or commands on the chat;
   - updates are timed only while the chat's turn runs, as for the chat
     itself.

   Children are cleared with the chat (new chat, switch, delete, the
   agent's exit). Those of the side question's fork are left out.
3. **Answer a child's permission requests.** In `onPermission`, a request
   on a known child's id is shown like the chat's own, labelled with the
   child's title, and is answered, not cancelled. Requests queue: the
   chat's and its children's wait in one list, the oldest is the one
   shown and takes the focus and the keyboard, and a new one answers
   none of the others. `stop()` and the agent's exit answer all of them
   `cancelled`.
4. **Show session messages in the chat.** `session_message` on the open
   chat replaces the launch call that used to stand there:
   - the outgoing prompt is a row "Sent to *title*" with the prompt
     closed;
   - the report or `:reply` is a row "*title* reported" with the text;
   - a click on the title opens the Agents view on that child.

   Add a transcript block kind for it in `transcript.ts`, one block per
   `messageId` in its session, merged when the id comes again. The
   pair's incoming copy in the child is its prompt.

   The child's calls stay in the chat, indented under the "Sent to" row
   that started them, and appear there as they run. A "Sent to" row and
   the child's incoming message have the same `messageId`, so the calls
   of that row are the child's tool blocks from that incoming message to
   its next one. A call the child made to start a subagent of its own
   shows the same way, one level deeper. The child's text and thinking
   stay out of the chat; they are in the Agents view.
5. **Build the Agents view from the children.** `agents.ts` takes the
   child sessions:
   - the label is `title`;
   - the type is the `agent` option, else `description`, else the title;
   - the model is the `model` option;
   - the status follows the state: `running` is running,
     `requires_action` is running and waiting for you, `idle` with
     `end_turn` is finished, `cancelled` is stopped, `error` is failed
     with its message, `idle` with no stop reason or any other one is
     ended, and `unknown` or no state is disconnected;
   - started by: the parent child's title, or the main agent;
   - the time is the sum of its runs, each from when it was seen running
     to when it was seen idle, and is unknown in a loaded chat;
   - tokens are not reported;
   - the `cwd` shows when it is set.

   Under those, the card shows everything the child did, in the order of
   its blocks, so a child that was messaged again reads prompt, calls,
   report, prompt, calls, report:
   - a prompt is an incoming `session_message`, numbered when there is
     more than one;
   - the calls are its tool blocks through `describeCall`;
   - the messages are its agent text blocks;
   - a report is its outgoing `session_message` to its parent, or the
     parent's `:reply` when it handed nothing back, numbered like the
     prompts.

   The status is that of the latest run.

   A launch call that still reaches the chat stays a subagent node built
   from the call alone: label, type and model from its input, status,
   result and time from the call. It has no calls, messages or tokens.
   When that call's id is the `messageId` of an outgoing prompt in the
   same session, the child is announced too (a permission request showed
   the call), so the call draws no node of its own.
6. **Stop a subagent.** A Stop button on a running child that has `cancel`
   sends `session/cancel` with its id through `connection.cancel`. The
   state update that follows is the confirmation.
7. **Feed the map.** The Agents station counts the children and their
   running ones instead of launch calls, and the `N agents` tab counts
   running children. The other stations count a child's calls as they do
   today: `summarize` takes the children's blocks with the chat's, and a
   child's call gives a row at its station. A child's call opens no model
   step, so the steps are the main agent's only. Rows are in the order
   they were seen; in a loaded chat, where nothing is timed, a child's
   rows follow the message that started them.
8. **Remove the rest of what the RFD form replaces**: `totals` and
   `launched` in `transcript.ts`, and the background, hand-back and
   launch-note rules in `agents.ts`. Before deleting a rule, check that
   no other agent LinuxIO runs relies on it. Only Claude's adapter sends
   subagents today.
9. **Declare the capability** in `connection.ts`: `subagents: {}`. Other
   agents ignore it.
10. **Documentation.** Rewrite the Agents part of
    [AI Assistant](../AI-assistant.md) from what the children give, move
    this plan's facts that stay true into it, and delete this file.

## Tests

- Store: a `subagent_update` announcement, patches, a nested child
  announced in the same batch as its parent child, a child's updates
  arriving after its announcement, a child's `config_option_update`
  leaving the chat's options alone, `capabilities: null`, a child of the
  side question left out, children cleared on switching chats.
- Permission: a request from a child is shown and answered, not
  cancelled; two requests open at once are both answered by the user;
  `stop()` answers every open one.
- Transcript: `session_message` blocks for prompt, report and `:reply`;
  the same `messageId` again merges into its block; a child's tagged
  text and thinking land in the child's blocks; a child's calls show
  under the "Sent to" row that started them, and those of a second
  message under the second row.
- Map: a child's calls count at their stations and give rows; they open
  no model step; hidden hand-back and `SendMessage` calls give no row.
- Agents: each state and stop reason, `idle` with none, the model from
  config options, the type falling back to the title, the result from
  the report, a never-announced launch as a fallback node, a launch
  shown by a permission request giving one node, a child messaged again
  showing both prompts and both reports in order with the time of both
  runs.
- Load: announcements and child transcripts during the replay, end states
  after it, and `cancel` arriving after the load response.
- `frontend/src/test/browser/assistant.spec.ts` scripts an agent; add a
  chat that announces a child, runs a call in it, hands back and goes
  idle.
- Then check against the real adapter in a live chat, with the scenarios
  of step 1.

## Open questions

- A cancel of the whole chat while background subagents run: whether they
  stop (see Background subagents). The Stop button covers stopping one.
- Whether to keep a `background` label. The launch call is hidden, so it
  could only come from a `:reply` arriving while the child still runs.
- Tokens per subagent need the adapter to fill `IdleStateUpdate.usage`.
  That is an upstream request, not LinuxIO work.
