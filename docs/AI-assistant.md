# Assistant

The Assistant page is a chat with a terminal coding agent (Claude Code, Gemini
CLI or Codex) running on the host as the logged-in user. The agent speaks the
[Agent Client Protocol](https://agentclientprotocol.com) (ACP, JSON-RPC 2.0
over stdin/stdout). The browser is the ACP client and renders the session; the
model runs at the vendor. LinuxIO runs no model, holds no API key and stores
nothing about conversations. The only thing it persists is the chosen agent id
(`assistant.agent` in the per-user config).

## Relay

The bridge package `backend/bridge/handlers/assistant` registers one duplex
route, `assistant.open`, with the request `{agent}`. It spawns the agent as the
session user (privilege drop mirrors the terminal) and relays its stdio to the
yamux stream. The bridge never parses ACP.

The bridge first asks the user's interactive login shell for its PATH
(`bash -i -l -c 'printf %s "$PATH"'`, run as that user with stdin and stderr
detached), then runs the agent's installed executable directly on that PATH.
Interactive matters: `~/.bashrc` returns early for non-interactive shells,
and that is where nvm, fnm and volta add themselves. Executing the agent
directly, rather than through `bash -i`, keeps bash's "no job control"
warnings out of the agent's stderr. Arguments are passed as an argv array and
never interpolated into a script. `NO_BROWSER` is not set on the ACP process.

Starting an agent never downloads, installs or updates anything. The relay
runs the copy [installed for the user](#installing-agents) and refuses, with a
409, when that copy is missing, is another version than the one LinuxIO pins,
or needs a CLI that is not on the user's PATH. `npx` is not involved: it would
ask the registry for the newest version on every start and reinstall inside
the chat's or the login check's wait.

The first data frame is `{"cwd":...,"linuxio":"ready"}`. When the process ends
the last data frame is `{"code":N,"linuxio":"exit","stderr":<4 KiB tail>}`,
followed by a close frame. Closing the stream sends SIGHUP to the agent's
process group, with SIGKILL after a 5 s grace if it is still running. Unlike
this, the terminal sends SIGHUP only, with no SIGKILL grace. No signal is sent
once the agent has been reaped.

Chat lifetime: the stream is not cached by the multiplexer (so the settings
probe never shares the page's stream); the chat and its connection live in a
module-level store, so it survives navigation but is lost on page reload. After a reconnect the frontend loads the newest session from
`session/list` with `session/load` when the agent advertises it, otherwise it
starts `session/new`. There is no bridge-side session registry, so a lost
websocket ends the agent process.

## Agents

| Id | Package (pinned version) | Executable | Runs |
|----|--------------------------|------------|------|
| `claude` | `@agentclientprotocol/claude-agent-acp` | `claude-agent-acp` | the `claude` on the user's PATH, through `CLAUDE_CODE_EXECUTABLE` |
| `gemini` | `@google/gemini-cli` | `gemini --acp` | itself |
| `codex` | `@agentclientprotocol/codex-acp` | `codex-acp` | its bundled Codex |

The table is `agents` in `backend/bridge/internal/acpagents`, which the
capability installer, the config handler and the Terminal agent login also
use. Packages and versions come from the ACP registry; a new version reaches
users only when a LinuxIO release changes the pin. The browser only ever sends
an agent id;
it never supplies a command, and unknown ids are rejected before anything
spawns. Settings: `config.get` returns `assistant.agent`; `config.set`
`{assistant:{agent}}` stores it (empty clears, unknown ids error).
`assistant.defaults` maps an agent id to the `model`, `effort` and `mode` a
new chat starts with; `config.set {assistant:{defaults}}` replaces the whole
map (unknown ids and values with control characters or over 128 bytes error).
An agent with no entry gets LinuxIO's starting point, which exists only for
`claude`: `opus`, `high`, `auto`.

## Installing agents

Each agent is a capability, `assistant_claude`, `assistant_gemini` and
`assistant_codex`, installed from the Capability Manager and never by opening
a chat. Install runs, as the session user and with the npm on that user's
login-shell PATH:

```text
npm install --prefix ~/.local/share/linuxio/agents/<id>.new --no-audit --no-fund --no-progress [--omit=optional] <package>@<version>
```

and swaps the new directory in for `~/.local/share/linuxio/agents/<id>` only
after npm succeeded, so a failed or interrupted install leaves the working
copy alone. Agents are per user because the Node.js that runs them is. The
capability is available when the installed package is exactly the pinned
version; after a LinuxIO update that moves a pin it reports
`<package> <installed> is installed, LinuxIO needs <pinned>` and the agent
stays off until Install is pressed again. The install route is privileged, so
a user without administrator rights runs the command above from the Terminal
page instead.

Claude's adapter is installed with `--omit=optional`, which leaves out the
copy of Claude Code its SDK would otherwise bring (about 330 MB of a 400 MB
install; without it the install is about 50 MB). It runs the `claude` on the
user's PATH instead, which is the separate `claude_code` capability. That one
installs Anthropic's `claude-code` package from Anthropic's own apt or dnf
repository on the `latest` channel, so Claude Code is updated with the host's
package updates on the Updates page rather than updating itself; a `claude`
the user installed another way satisfies it as well. See
[Capabilities](./capabilities.md#claude-code-and-the-assistant-agents).

`make test-assistant-agent-integration` installs all three agents with the
real npm into a temporary home and starts each far enough to answer
`initialize`. It needs Node.js, network access and `claude` on the PATH.

## Client behavior

The browser uses `@agentclientprotocol/sdk` (`frontend/src/api/acp/`: a
transport over the LinuxIO duplex stream, a connection helper and the login
probe). Every import comes from the `/unstable` entrypoint, the full ACP v1
surface: the default entrypoint is stable-only since SDK 2.0 and leaves out
`session/fork`, which the side-question panel needs, and the two cannot be
mixed. The client advertises `auth.terminal`, boolean config options and
`subagents` (see [Subagent sessions](#subagent-sessions)), and declines the fs
and terminal client methods, so the agent uses its own tools. Messages are
plain text, images and file context. Permission cards show the agent's own
options and say that nothing has changed yet. Requests queue: the chat's and
its subagents' wait in one list, the oldest is the one shown and has the
keyboard, and a new one answers none of the others. A subagent's card says
`Asked by` with its title. A request the agent takes back
(`$/cancel_request`, which aborts the request's signal) is dropped, shown or
waiting, and the next one comes up: Claude Code takes back what a subagent
still asks when it stops that subagent, which it does when the user says No
to a request of a subagent in the foreground.
Stop sends `session/cancel` and answers every open request `cancelled`, as the
agent's exit does.

When the agent advertises `sessionCapabilities.systemPrompt`, every
`session/new`, `session/load`, `session/fork` and `session/resume` carries a
`systemPrompt` in `append` mode (`LINUXIO_SYSTEM_PROMPT` in `connection.ts`),
so the agent keeps its own instructions and gains LinuxIO's. It is sent on
reopening too because the agent rebuilds its instructions each time. Today it
says one thing: a shell command meant for the user goes in a fence tagged
`bash`, alone, with output in a separate block, which is what gives the block
its [Open in terminal](#features) button. An agent that does not advertise the
capability gets no instructions, and its untagged commands can only be copied.

## Features

The agent drives most of the surface, so a control only appears when the agent
advertises the matching capability.

- **Composer bar.** Under the text field, one row: `+` (add an image or attach
  a file) and `/` (slash commands) on the left, then the model and effort of
  the agent's config options as compact selects, any other select or boolean
  option, and on the right the mode select and Send (a red Stop button while a
  turn runs). Changing one sends `session/set_config_option` or
  `session/set_mode` and replaces the local options with the agent's reply.
  Pieces the agent does not offer are hidden, and so is the `agent` option
  (Claude Code's main-thread persona picker): it lists subagent definitions
  such as `web-fetch`, which fail when they run a whole conversation.
- **Chat defaults.** Settings → Assistant lists the models, effort levels and
  permission modes from a chat the probe opens, so a new model shows up as
  soon as the agent offers it, with no LinuxIO release. The values stored are
  the agent's own ids (`opus` stays the newest Opus). Each new chat (not a
  reopened one) is switched to them with `session/set_config_option` (model
  before effort, whose levels depend on the model) and `session/set_mode`
  before the prompt is enabled. A value the agent does not offer, or refuses,
  leaves the agent's own choice; empty means the agent's own choice.
- **Status line.** While a turn runs, a line above the composer shows a spinner,
  a verb that cycles every 4 s (Thinking, Working, Reading, Checking,
  Pondering, Reviewing) and the elapsed time. It, and its timers, exist only
  while the turn runs.
- **Run log and change cards.** Consecutive tool calls are split by whether
  they acted on the host. Steps that only looked (read, search, fetch, think,
  and commands recognised as read-only) are lines in a log on a rail: the
  kind of step (a read-only command is labelled Read, not Run), coloured by
  state (green done, amber with a spinner while
  pending or running, red with an alert on failure), then the agent's
  description of the step with the command after it in grey, truncated when
  long (the command alone when there is no description, the title when there
  is no command). A log longer than five lines
  shows the newest five behind a "Show N earlier steps" button. Steps that
  acted (edit, delete, move, and any command not recognised as read-only) are
  grouped in one card, titled "Running on this server" while one is in
  progress, then "Changed on this server" when a file was edited, deleted or
  moved, or "Ran on this server" for commands alone, with a failed count. The
  card appears whether or not the agent asked first, so a change made under an
  automatic mode is as visible as an approved one. A file tool's line says what it did to which
  file: "Wrote /path · N lines" for a whole file (the agent reports a new file
  and an overwrite alike, so it cannot say "created") and "Edited /path" for a
  change inside one. Any line opens to the call's input, output and diff; a
  call with a diff shows the diff alone, without the raw input that repeats
  it. The call a permission card is asking about
  is left out of the transcript until it is answered.
- **Messages to subagents.** Where the agent starts a subagent or messages
  one again, the transcript has a row `→ Sent to <title>` with the prompt
  closed under it, in place of the `Agent` or `SendMessage` call, which the
  agent does not send. The calls the subagent made for that message stand
  under the row, one step in on a rail, in the same logs and cards as the
  chat's own, and appear there as they run; the calls after a second message
  stand under the second row. A subagent that the subagent started shows the
  same way one level deeper. What a subagent said and thought stays out of
  the chat: it is in the Agents view. Its report is a row `← <title>
  reported` with the text as Markdown. The title opens the Agents view on
  that subagent. A message whose subagent is not known reads `a subagent`.
- **Read-only commands.** `transcript/readOnlyCommand.ts` decides which
  commands only read: a list of readers (`cat`, `ls`, `ss`, `df`, ...),
  read-only subcommands (`systemctl status`, `docker ps`, `btrfs filesystem
  usage`, ...) and commands that read unless a mutating flag is present
  (`find -delete`, `sed -i`, `journalctl --vacuum-*`). Every segment of a
  pipeline must read, and a redirection or command substitution makes the
  whole command count as acting. Quoted text is literal, so a `|` or `>`
  inside a search pattern does not split or redirect; a substitution inside
  double quotes still counts. This is presentation only and errs towards
  "acting"; permissions stay with the agent.
- **Turn footer.** A turn that ran in this page ends in a footer showing how
  long it took and, when the agent's `session/prompt` response carries
  `usage`, the output tokens (`14 s · 2.1k tokens`), with input and cache
  counts in the tooltip. The store appends a `turn` block when the prompt
  resolves; a failed prompt gets none, and replayed history has no footer,
  since the agent does not replay the numbers.
- **Reading column.** The transcript scrolls across the page, but its rows,
  the permission card, the status line and the prompt share one centred
  860 px column. The user's prompt is a bubble sized to its text on the
  right; answers take the column.
- **Side question.** When the agent advertises `session/fork`, typing
  `/btw <question>` in the composer opens a panel beside the chat (below it on
  narrow screens), also while a turn is running. It forks the
  current chat with `session/fork`, so the side chat has the same context,
  and answers independently while the main chat keeps streaming. Its
  permission requests appear inside the panel. Close hides the panel and
  keeps the side chat so a later `/btw` reopens it; delete discards it and, when the
  agent advertises `session/close`, closes the side session. Starting a new
  chat, loading another one or disconnecting discards it as well. Subagents
  the side chat starts are left out: nothing they send is shown, and a
  permission request of theirs is answered `cancelled`.
- **Images.** Paste or drop an image into the composer when the agent accepts
  image prompts. Thumbnails with a remove button appear above the text, and
  the image blocks are sent before the text.
- **Slash commands.** Typing `/` at the start of the composer lists the
  agent's `availableCommands`, filtered by what was typed. Enter or a click
  inserts `/name `, and the message is sent as ordinary text. Two entries are
  LinuxIO's own and are never sent to the agent: `/btw` and, for an agent
  whose plugins LinuxIO manages, `/plugins` (see [Plugins](#plugins)).
- **@-mentions.** The `@` button, or typing `@` at a word start, opens the file
  picker. The file is read through `filebrowser.read_text` and attached as an
  embedded resource (capped at 200 KB), or as a link when it is larger, cannot
  be read, or the agent does not take embedded context. Chips in the top
  row of the composer box show the attachments; a line divides that row from
  the text field, and another divides the text field from the composer bar.
- **Usage.** A context gauge in the composer bar shows the share of the
  context window used, from the latest `usage_update`; it is always there,
  as an empty ring until the agent reports usage, which Claude Code does
  only once a reply starts, also in a chat loaded from history. Its tooltip
  gives the tokens used and, when reported, the cost. Clicking it sends the
  agent's `/compact` command as a prompt of its own; the click does nothing
  before usage is reported, while a turn runs, or when the agent does not
  list `compact` in `availableCommands`.
- **Steering.** When the agent supports `_session/steering`, the composer stays
  enabled during a turn and Enter injects the message into the running turn.
  If the agent answers `promptRequired`, the text is sent as a normal prompt
  once the turn ends.
- **Markdown.** Agent replies render as Markdown with GitHub tables,
  strikethrough, task lists and autolinks, no raw HTML. Links open in a new tab
  and each code block is one surface with a bar naming the fence language and
  a Copy button, as wide as the column. The block's background is the theme
  colour `codeInsetBackground` (`--app-code-inset-background`, "Assistant code
  background" under Settings → Theme). The backend gives it no default, so
  until the user picks a colour the key stays unset and the UI derives it
  from the page background (18% darker on dark, 3% on light),
  so the block is set into the page and keeps the page's hue. The code and
  Copy read at full text colour. It is not the theme's code block colour
  (`codeBackground`), which is the surface of the log views. Fenced code is
  syntax-highlighted using
  the file editor's languages (blocks over 20,000 characters stay plain). User,
  thought and plan text stay plain. Paragraphs and lists are spaced in `em`,
  so the air between them grows with the chat's text size.
- **Diff view.** Tool calls that carry a diff show it as a line diff with the
  file path as header, additions and removals tinted.
- **Chats.** The page header holds one action, New chat. The previous chats
  are a tab on the chat's left edge: it opens a card that floats beside the
  chat, as tall as its list, with a search field and the chats grouped under
  Today, Yesterday and Earlier by when they were last used. The open chat is
  marked, and clicking another loads it. The tab is disabled when the agent
  cannot list sessions. The tabs stay in place while a card is open, and one
  card is open at a time (the chats, the activity map or a
  [plugin pane](#plugin-panes)), so two cards never run past the bottom of a
  short window.
- **Activity map.** The Activity tab, under Chats, opens a card that draws
  where the chat's work went. It is LinuxIO's own (`-components/workmap`),
  computed from the transcript and `usage_update` the page already holds: it
  asks the agent for nothing, needs no plugin, and works for any agent that
  reports tool calls. Its look, wording and motion are those of the
  [lot-map](https://github.com/alexeyw/lot-map) plugin by Aleksei Volodin,
  used under its MIT licence ([Third-Party Notices](THIRD_PARTY_NOTICES.md)),
  so it reads the same as that plugin's pane, but it is drawn in the page:
  nothing in it restarts when something happens, where a plugin pane's
  picture is replaced whole on every redraw.

  - *Picture.* The agent's name is in the middle with the model the chat is
    set to and a ring for how full the context window is (grey, the accent
    from 70%, red from 90%). Ten stations stand around it: You, Model,
    Memory, Files, Shell, Network, Browser, Agents, Skills, Hooks, each with
    a count. A spoke thickens and takes colour with use. A line traces the
    route through the last seven stations the work went to; a leg stays 40 s
    and fades over 20 s. The core breathes, faster while a turn runs.
  - *Motion.* A call that starts sends a pulse out along its spoke with a
    ripple where it lands; a call that ends sends one back, red if it
    failed. A prompt comes in from You as a hollow pulse, and an answer goes
    out with a wide ring from the core. A station is lit for 3.2 s after
    something happened at it (6 s in red after a failure). While one of its
    calls runs it keeps a ring spreading from it and two clock hands around
    it (a minute a lap, and ten minutes), with `×N` when several run at once;
    a red badge counts what failed there since the last prompt, and each
    subagent still at work circles the Agents station.
  - *Rows.* Under the picture one line says what is running (the context
    and the model are left to the picture), over the latest sixteen rows,
    newest first: `new message`, each step of the model (`opus 5.5 · step
    2`, numbered per prompt), each tool call, `answer ready`, and a change
    of model between prompts (`fable 5.2 → opus 5.5`). A call is named in a
    few words: the agent's description of a command (or the command's name),
    the last two parts of a file's path, a web address's host, a subagent's
    title, a skill's name. Finished calls in a row at one station fold
    into one row (`+2`) with their times added. Each row and the line above
    them are one line, cut at the card's edge, with the time in a column at
    the right, so a long label neither wraps nor moves the rows while the
    work goes on. A row whose label is longer than 28 characters has a `▸`
    that shows it whole and closes it again; one row is open at a time.
  - *What it knows.* A call is sorted by Claude Code's tool name when the
    adapter sends one (`_meta.claudeCode.toolName`) and by its ACP kind
    otherwise. A shell command goes to Network when any part of it is a
    network command (`curl`, `ssh`, `gh`…), to Files when every part is a
    file command (`cat`, `rg`, `ls`…), and to Shell otherwise; a file under
    `.claude/projects/*/memory/` goes to Memory; an MCP tool goes by its
    server (the browser servers to Browser, `terminal` to Shell, a server
    named for memory to Memory, any other to Network). A call starts a new
    step of the model when the model said nothing just before it and no
    earlier call of the turn was still open; between its calls the model is
    shown at work on its next step.
  - *Subagents.* Each subagent is one row at Agents, under its title,
    running until its state says otherwise, with the time of all its runs;
    the station counts the subagents, not the messages to them. A subagent's
    calls count at their own stations and give rows like the chat's. They
    open no step: the steps are those of the chat's model only. A message
    the agent sends a subagent is the model's doing and opens a step, as a
    call does; messages sent one after the other (several subagents started
    at once) are one step, and a report handed back is what the next step
    starts from. The rows are in the order they were seen, so the calls of
    two subagents at work together mix. In a chat that was loaded nothing
    carries a time, and a subagent's rows follow the message that started
    them. While a subagent runs the model counts as waiting for it, so no
    step of its own is shown going.
  - *Times.* Only what LinuxIO saw happen is timed and animated; a chat that
    was loaded has its work counted and nothing else. The agent says when a
    call ends but not when it starts to run, so a call is timed from the
    last update that wrote its title or input while it was pending (the
    model has finished writing its input by then) to its end. Claude Code's
    adapter sends one more update as a call ends, with the tool's response
    and no status; it writes neither, so it does not move the start. A step
    is timed from when the model was last handed something (the prompt, or
    its calls' results) to its first call or the turn's end. The answer's row shows how long the whole turn took,
    the time under the answer in the transcript. What takes no time (a
    message arriving, a change of model) shows `—` in the time column, and a
    row whose time was not seen shows nothing there. A step carries the model
    that was set when its prompt was sent.
  - *What it lacks beside the plugin.* Hooks stays quiet: the adapter drops
    Claude Code's hook events, so they never reach the browser. A subagent's
    own calls fly from the core, not from the Agents station; a call a hook
    refused is an ordinary failure; and compaction is not a step.
  - *Card.* It shares the plugin pane's box, blending, width and resizing,
    is drawn only while open, and the browser remembers that it was open and
    reopens it where it fits beside the chat. Opening it closes a plugin's
    pane and the reverse; an open side question hides both.
- **Agents.** Under the Activity tab, a tab counts the subagents at work
  (`0 agents`, `2 agents`); its dot is grey while none runs and pulses while
  one does. It opens the activity card on its other view
  (`-components/workmap/AgentMap.tsx`), which the map's Agents station opens
  too. The view draws the chat's agent, with its model and the tokens in its
  context, and the subagents it sent off joined to it by lines: beside it on
  a wide card, under it on a narrow one, and the subagents they sent off
  under them. Each subagent shows how it stands when it is not simply
  finished (`running`, `waiting for you`, `stopped`, `failed`, `ended`,
  `disconnected`), its kind, and how long it has run or took. Choosing the
  chat's agent goes back to the map; choosing a subagent, here or by its
  title in the transcript, shows under its name:
  - how it stands, its kind and the model it ran on; why it failed, when
    the agent says; which agent started it; its time and `tokens not
    reported` once it has ended; its working directory when that is not the
    one of what started it;
  - a **Stop** button while it runs and the agent takes a stop for it,
    which is also beside its row in the list. It sends `session/cancel`
    with the subagent's session id and answers that subagent's open
    permission requests `cancelled`; the state the agent sends next
    (`stopped`) is the confirmation. Only that subagent stops;
  - the last thing it said, cut to three lines, while it works;
  - everything it was told, did, said and reported, in the order it
    happened: *Prompt*, closed, in grey; *Tool calls (N)*, open, oldest
    first; *Messages (N)*, closed, in grey; *Report*, open, in grey. A subagent
    that was messaged again reads *Prompt 1*, its calls, *Report 1*, *Prompt
    2*, its calls, *Report 2*. A call is the tool (`Read`, `Bash`,
    `Web Fetch`, `Search tools`, an MCP tool's name), what it worked on in
    grey (a file's name, a command's description or the command, the
    address, the pattern or query; in blue code type where it is code) and a
    note: the lines a read asked for (`lines 421-465`), where a search
    looked, `running` or `failed`. The whole path or command and the time
    are the row's tooltip;
  - the subagents it started.

  The tab of the view the card shows reads as open, and the browser
  remembers the view.
  - *What it knows.* All of it comes from the subagent's own session; see
    [Subagent sessions](#subagent-sessions). The label is the session's
    title. The kind is its `agent` option, else its description, else the
    title, and is left out where it only repeats the label. The model is its
    `model` option, which the agent sends once the subagent's first response
    names it.
  - *How it stands* follows the session's state: `running`;
    `requires_action` is waiting for you; `idle` with `end_turn` is
    finished, with `cancelled` stopped, with `error` failed; `idle` with any
    other stop reason or none is ended; `unknown`, or no state at all, is
    disconnected. The state is that of its latest run: a subagent that is
    messaged again runs again in the same session.
  - *Time.* Each run is timed from when LinuxIO saw it start to when it saw
    it end, and the time shown is the sum of the runs, not the time between
    the first start and the last end. The agent gives no time or tokens of
    its own for a subagent, so a chat that was loaded shows no time.
  - *Reports.* A report is the subagent's own message to its parent. One
    that handed nothing back answered with its last text, which the parent
    got as a reply; that reply is then its report, and the same text is not
    listed a second time under Messages. What a subagent gets from one it
    started is that one's report, shown on that one's card, and not a prompt
    of its own.
  - *A loaded chat* has each subagent's prompts, calls, messages and
    reports, and how it ended. One that still runs in the agent's process
    can be stopped.
  - *When the agent stops*, the subagents stay to be read like the chat:
    whatever ran is disconnected and none can be stopped.
  - *A subagent call that reaches the chat as a tool call* (`Agent`, or the
    older `Task`) is shown from the call alone: label, kind, model and
    prompt from its input, status, report and time from the call, with no
    calls or messages. The adapter sends one for a launch whose subagent it
    never announced. A launch that a permission request showed stays a tool
    call while its subagent is announced too; the prompt then has the call's
    id, and the call is drawn nowhere: not in the transcript, the map or
    this view.
  - An agent that reports no subagent sessions and sends no tool names has
    no subagents here.
- **Delete.** When the agent advertises session deletion, each chat on the
  Chats card has a delete button behind a confirmation. Deleting the open
  chat starts a new one, and a running chat cannot be deleted.
- **Welcome screen.** A new chat shows up to four prompts, one click each.
  The host's open alerts that need a look come first (errors, then warnings,
  newest first; `info` alerts are left out), with a line saying how many
  there are, and standing prompts fill the rest (a health check, CPU and
  memory, failed services, recent errors). An alert's title and message can
  carry text from outside the host (a login name someone tried, a script's
  name), so the message is only displayed and the prompt quotes the title
  alone, on one line and capped at 200 characters; the agent looks up the
  rest. The alerts come from `alerts.list`, which only a privileged session
  reads; any other session gets the standing prompts.
- **Open in terminal.** A fenced block labelled `bash`, `sh`, `shell` or `zsh`
  in an answer has an Open in terminal button beside Copy, in the theme's
  primary colour. It navigates to
  `/terminal` with the command in router history state (never the URL, so a
  crafted link cannot choose it), and the terminal types it into the user's
  shell without running it. The command is agent-written text on its way into
  a shell, so `typedCommand.ts` first reduces it to printable characters and
  newlines (no Enter, no interrupt, no escape that could end a paste early)
  and refuses more than 8000 characters. One line is typed as it is. Several
  lines go as one bracketed paste, and only once the shell has turned
  bracketed paste on; a shell without it would run every line but the last,
  so until then the terminal waits and says so. The command is removed from
  history state once taken, so a reload does not type it again, and nothing
  is typed into an agent login PTY. Code in a plugin pane or a side question
  has Copy only.
- **Auto-scroll.** The transcript follows new content while it is scrolled to
  the bottom and pauses when the user scrolls up.
- **Shortcuts.** Enter sends, Shift+Enter inserts a newline, Escape stops a
  running turn, Shift+Tab cycles the mode, Ctrl/Cmd+Shift+O starts a new chat.
  The header tooltips show them.
- **Text size.** The transcript and composer use the app body size, and code
  uses a smaller monospace size. The chat starts at the terminal's font size;
  the Text size row at the foot of the composer's `+` menu changes it, and
  the choice is kept in the browser.
- **Titles.** A `session_info_update` title renames the chat on the Chats
  card; untitled chats show their session id.

## Subagent sessions

Claude Code's adapter supports ACP's unstable subagent sessions (its "RFD
form"): a client that declares `clientCapabilities.subagents` gets each
subagent as a session of its own. LinuxIO declares it (`connection.ts`); the
other agents ignore it. Without it the adapter sends a subagent's work as
tool calls of the main chat tagged `_meta.claudeCode.parentToolUseId`, and
replays none of it in a loaded chat. The types are `SubagentUpdate`,
`StateUpdate` and `SessionMessage` in `@agentclientprotocol/sdk/unstable`.

The form is unstable, so a new adapter pin can change it. What follows was
read from the source of `claude-agent-acp` 0.102.0 (`dist/acp-agent.js`,
`native-subagents.js`, `subagent-replay.js`) and checked against a chat
with the real adapter, recorded on 2026-11-13 with every update it sent:
two subagents started at once, one of them in the background and starting
one of its own, a `SendMessage` to a finished one, a subagent stopped with
`session/cancel`, and the same chat loaded again. The recording is
`frontend/src/test/assistantSubagentTrace.json`, with paths and session ids
replaced. A unit test replays it through the store
(`-components/subagentTrace.test.ts`) and the browser spec through the page
(`test/browser/assistant.spec.ts`), so a new pin is checked by recording the
chat again. The recording ran in the `auto` mode and asked nothing; a
permission request from a subagent was seen in a chat set to Manual on the
same day, with two subagents asking at once. What is still open is in the
[backlog](TODO/backlog.md).

What the adapter sends:

- **`subagent_update`** on the session that started the subagent, with the
  subagent's own session id. A subagent that starts one is the parent of
  that one. The first carries `title`, `description` (the subagent type
  when it differs from the title), `cwd` when it differs from the parent's,
  `capabilities: {cancel: {}}` and `state: running`. Later ones carry only
  what changed; a field left out is unchanged and `null` clears it.
- **The subagent's work** (calls, text, thinking) as ordinary
  `session/update` notifications on its session id. They keep the
  `parentToolUseId` tag, which LinuxIO does not read.
- **`config_option_update` on the subagent's session**: read-only `mode`,
  `model`, `effort` and `agent`. A subagent cannot be prompted or
  configured; the one thing a client may do is cancel it.
- **`session_message`**, an upsert by `messageId`, for what goes between
  sessions. The launch prompt, a `SendMessage` that reached the subagent and
  the report from `SubagentHandback` each come twice, once in each session,
  under one id. A launch that ends without a hand-back gives the parent
  alone a `<launch id>:reply`.
- **No tool call** for the `Agent`/`Task` launch, for a `SendMessage` that
  reached an announced subagent, or for `SubagentHandback`. The exceptions
  are a launch whose subagent was never announced, a launch a permission
  request showed, a hand-back that failed, and a `SendMessage` that reached
  no announced subagent.
- **Permission requests** of a subagent under its session id. An open one
  holds the parent's turn, so one that nobody answers leaves the chat
  waiting. Several can be open at once, one per subagent that asks.
- **On `session/load`**, each subagent's announcement (as `running`, with
  no capabilities), its model and its own transcript, right after the
  message that launched it; its end state after the whole transcript; and,
  after the load response, what it is now, with `cancel` only if it still
  runs in this process. A subagent with no recorded end is `idle` with no
  stop reason. Two things in the replay differ from the live chat. A
  subagent's whole transcript comes at its launch, later prompts and reports
  included, so the parent gets the report for a second message before its
  own copy of that message. And a call can come without its end: the
  recording has one, a call that was open when its subagent started a
  subagent of its own.
- **On `session/cancel` of a subagent**, nothing in reply; the subagent goes
  `idle` with `cancelled`, and the parent's turn runs on to its end.

How LinuxIO keeps it (`-components/subagents.ts`, `assistant-store.ts`):

- The open chat's subagents are a list in the store, each with its title,
  parent, state, working directory, options, blocks and runs. They are
  cleared with the chat: a new chat, another chat, a delete.
- Updates are applied in the order they came, not session by session: a
  subagent can be announced on another subagent's session in the same batch.
  An update for a session that is neither the chat's, the side question's
  nor a known subagent's is dropped.
- A subagent's options and commands stay its own and never replace the
  chat's.
- A session message is one transcript block per `messageId` in its session
  (`MessageBlock` in `transcript/transcript.ts`), merged when the id comes
  again. The calls a subagent made for a message are its tool blocks from
  its copy of that message to the next message it got.
- Runs are timed only while the chat's turn runs, like the chat's calls.
- When a session gets its copy of a message it sent a subagent, the reports
  that subagent made after getting it are put behind it (`reportsInOrder`),
  so a loaded chat reads message, then report, as the live one did.
- A subagent's call with no end on record counts as running only while the
  subagent does; after that it reads as done, in the transcript, the map
  and the Agents view.

What the form does not give:

- **Tokens and time per subagent.** `usage_update` is for the main session
  only and the schema's `IdleStateUpdate.usage` is never filled, so every
  subagent reads `tokens not reported`. That needs a change upstream.
- **Whether a subagent was sent to the background.** The launch call is not
  sent, so there is no `background` label; its state says whether it runs.
- **A stop of the whole chat that stops every subagent.** With this form
  declared the adapter makes the interrupt spare background subagents
  (agent-client-protocol#2308), and a live chat showed one going on. That
  is kept, as Claude Code's own apps do it: the chat's Stop ends the turn,
  and a subagent that goes on is stopped by its own Stop button.

## Plugins

`/plugins` opens a dialog over the chat with three tabs: Installed (enable
switch, uninstall), Browse (search the marketplaces' catalogue, install) and
Marketplaces (add, refresh, remove). The alert shown when the agent stopped or
failed to start has a Manage plugins button for the same dialog, because the
composer is disabled then, and a plugin can be the reason. It exists for Claude Code only
(`plugins` in `frontend/src/api/acp/agents.ts`, `pluginAgent` in the bridge);
the other agents' ids are refused with 400. Claude Code's own `/plugin` is a
terminal screen the ACP adapter does not advertise, so LinuxIO manages plugins
the way Claude Code's editor extensions do: the bridge runs
`claude plugin ... --json` and never edits `~/.claude` itself.

| Route | Kind | Runs |
|-------|------|------|
| `assistant.list_plugins` | Call | `plugin list --json`, `plugin marketplace list --json` |
| `assistant.search_plugins` | Call | `plugin list --json --available`, filtered in the bridge |
| `assistant.install_plugin` | Mutation | `plugin install <id> --json --scope user [--accept-command <sha256>]` |
| `assistant.uninstall_plugin` | Mutation | `plugin uninstall <id> --json --scope <scope>` |
| `assistant.set_plugin_enabled` | Mutation | `plugin enable\|disable <id> --json --scope <scope>` |
| `assistant.add_marketplace` | Mutation | `plugin marketplace add <source> --json --scope user` |
| `assistant.remove_marketplace` | Mutation | `plugin marketplace remove <name> --json` |
| `assistant.update_marketplace` | Mutation | `plugin marketplace update <name> --json` |

- **Who runs it.** Every command runs as the session user, in their home, with
  the `claude` on their login-shell PATH (409 when there is none), standard
  input closed and arguments passed as an argv array. It writes only under
  that user's `~/.claude`, so no route is privileged: it is what the user can
  already type in the Terminal page. Reads have 30 s, writes 10 min, and a
  cancelled job kills the CLI's whole process group.
- **Search.** The catalogue is a few megabytes (about 4,000 entries), so the
  browser never receives it. The bridge drops installed plugins, keeps the
  entries where every word of the query appears in the name, description or
  marketplace, and returns the 50 most installed with the total count.
- **Validation.** A plugin id is `<name>@<marketplace>` and a marketplace name
  one word, both starting with a letter or digit so neither can read as a
  flag. A marketplace source is a GitHub `owner/repo` or an `https` URL
  without credentials; local paths and other schemes are refused and stay a
  Terminal task. The log line for an added marketplace leaves out the URL's
  query and fragment, where a private catalogue may carry a token.
- **Installed plugins only.** `claude plugin enable` accepts an id it has never
  seen and writes it to the user's settings. Enable, disable and uninstall
  therefore look the plugin up in `plugin list` first, answer 404 when it is
  not installed, and use the scope the list reports. A plugin an
  organization's managed settings install is shown but not changeable (409).
- **Marketplace-declared commands.** Some marketplaces install a plugin by
  running a command. The CLI refuses those without a terminal and reports the
  command with its sha256; the bridge never passes `-y`. The install result
  then carries the command with `installed: false`, the dialog shows it
  ("Nothing has run yet"), and only a confirmation repeats the install with
  `--accept-command <sha256>`, which the CLI honours for exactly that command.
- **Errors.** A failed write returns the `message` of the CLI's JSON result
  line, or the stderr tail when there is none; `not_found` and `not_installed`
  map to 404.
- **When a change applies.** Claude Code loads plugins when a chat starts
  (the adapter passes the user, project and local settings) and the pinned
  adapter has no way to reload them, so the dialog says that changes apply to
  new chats and offers one unless a turn is running.

A plugin's skills, commands, agents, hooks and MCP servers work in a LinuxIO
chat because Claude Code loads them itself. A plugin's pane is drawn by the
interface in front of Claude Code; see [Plugin panes](#plugin-panes).

## Plugin panes

A plugin can draw a pane beside the chat (a live map of the work, for
example). Claude Code serves panes to its interfaces over control requests on
its stream-JSON stdin and stdout (`ui_attach`, `ui_panes`, `ui_render`, pushed
`ui_panes` and `ui_invalidate` messages), advertised by the `ui_surface_v1`
capability. The ACP adapter starts Claude Code itself and carries none of
this, so LinuxIO reaches it beside the adapter:

```text
browser ── assistant.open ── bridge ── claude-agent-acp ── linuxio-claude-relay ── claude
                               └──── unix socket (pane lines) ────┘
```

Nothing below is a published interface of Claude Code. It was read from what
Claude Code 2.1.322 does, it accepts only the surface names `desktop`,
`mobile` and `vscode` (LinuxIO attaches as `vscode`), and it can change with a
Claude Code update, which arrives with the host's packages and not with a
LinuxIO release. Every step is therefore optional: when a capability is
missing or a message does not parse, the pane is not shown and the chat is
untouched.

- **Opt-in.** `assistant.open` takes `panes`. The Assistant page's chat sets
  it; the Settings login probe does not. Without it, or for an agent that is
  not marked `Panes` in `acpagents` (only `claude` is), or when
  `/usr/local/bin/linuxio-claude-relay` is missing, the stream is exactly what
  it was.
- **The relay.** With panes on, the bridge points the adapter's
  `CLAUDE_CODE_EXECUTABLE` at `linuxio-claude-relay` and passes the real CLI
  in `LINUXIO_CLAUDE_BIN` and a socket path in `LINUXIO_PANE_SOCKET`. The
  relay starts the real `claude` with the same arguments and copies the
  adapter's stdin and Claude Code's stdout byte for byte. It adds its own
  `ui_*` control requests between the adapter's lines, removes their answers
  from what the adapter reads, and copies the pushed `ui_*` messages to the
  socket (the adapter still receives them and ignores them). It refuses any
  request whose subtype does not start with `ui_`. Claude Code's children
  never see the three variables: the relay removes them and puts the real
  path back. An invocation that is not stream-JSON on both sides, or one
  without a reachable socket, is `exec`ed straight into `claude`.
- **The socket.** One private directory per stream (0700) with one unix
  socket (0600), both owned by the session user, removed when the stream
  ends. The bridge accepts a connection only when its `SO_PEERCRED` uid is the
  session user's, at most 16 at a time (a chat, a `/btw` fork and a resumed
  chat each start their own `claude`). It carries no root action: the bridge
  relays lines between that user's Claude Code and that user's browser.
- **In-band lines.** Pane traffic rides the `assistant.open` stream as whole
  lines between the agent's lines: `{"conn":1,"event":"open","linuxio":"pane"}`,
  `{"conn":1,"linuxio":"pane","message":{...}}` and a matching `close`. The
  browser sends `{"linuxio":"pane","conn":1,"id":"p7","request":{...}}`; a
  line that starts with exactly `{"linuxio":"pane",` goes to that connection
  and never to the agent's stdin. Lines over 64 KiB from the browser and over
  4 MiB from a relay are dropped. The bridge still does not parse ACP.
- **The browser.** `frontend/src/api/acp/panes.ts` attaches once the relay
  reports `ui_surface_v1`, reads the roster, renders the shown pane and
  renders it again on each `ui_invalidate`, with one render in flight and one
  queued. The pane of the Claude Code process whose `session_id` equals the
  chat's ACP session id is drawn on a card that floats in the margin beside
  the chat, so the conversation keeps its centred column. Dragging the card's
  right edge (or the arrow keys on it) resizes it between 240 and 720 px, a
  double click restores 340 px, and the width is kept in the browser. The
  pane has a tab on the chat's left edge, under the Chats tab: the card's
  one header button closes it to that tab, and the tab opens it again. Where
  the margin cannot hold the card beside the tabs it starts closed and opens
  over the chat on request. What the user chose holds for that pane; another
  plugin's pane starts from the margin again. The tab's dot ripples on each
  redraw and pulses while a turn runs, and the tab glows when a turn ends
  with the card closed. An open side question replaces the pane and its tab. A refused attach or roster, a message that does
  not parse, or three renders failing in a row hides the pane until that
  process ends; one failed render keeps the last drawing.
- **Drawing.** Read-only: `Box`, `Text`, `Svg`, `Markdown`, `Code` and `Link`.
  Plugin content is untrusted: an `Svg` is shown as an image from a `data:`
  URI and never inserted into the page, a link opens only `http` and `https`
  in a new tab, and depth, node-count and size limits bound the tree.
  `Button`, `Input` and `Select` are left out, with a line under the pane
  saying it has controls LinuxIO cannot use yet. A picture follows LinuxIO's
  theme for its own light and dark rules, because an image takes its colour
  scheme from the page. It sits in a box coloured by the theme colour
  `panePictureBackground` (`--app-pane-picture-background`, "Assistant pane
  picture background" under Settings → Theme) and is blended onto that box,
  not onto the card: screen on dark, multiply on light. The box's colour is
  therefore added to everything the plugin drew, its background included;
  the nearer it is to black on dark (white on light), the nearer the picture
  is to how the plugin drew it. Until the colour is set, a dark theme uses
  the page background 30% darker (the backend gives the key no default),
  so a picture drawn on a near-black ground comes out with its ground about
  on the page background and its raised parts about on the paper colour and
  reads as part of the page. A light theme uses the paper colour: multiply
  can only darken, so no box colour brings a plugin's light ground up to the
  page background, and white leaves the picture as drawn. Browsers without
  CSS style queries show the plugin's background as drawn.

## Login

Login state belongs to the agent CLI, not LinuxIO. The Settings tab probes it:
it sends `initialize`, waits up to 2 s for an `_auth/status_update`, and
otherwise tries `session/new`, mapping JSON-RPC error `-32000` to "not logged
in". When logged in it keeps (or, after a pushed status, opens) that session to
read the chat defaults' choices. Agent-handled auth methods call `authenticate`; `logout` is offered when
the agent advertises it.

Neither the probe nor the chat starts an agent until the capability scan
confirms what the agent needs (`requires` in `frontend/src/api/acp/agents.ts`):
Settings and the Assistant page list what is missing, with the scan's own
reason, and link to the Capability Manager. Every wait is bounded: the agent
has 30 s to start and answer `initialize` (`AGENT_START_TIMEOUT_MS`, which the
chat shares) and the probe's chat has 30 s to open (`CHAT_OPEN_TIMEOUT_MS`).
Past either, the stream is closed and Settings shows the reason with a retry
instead of "Checking login…".

Terminal-type auth methods navigate to `/terminal` with the agent and args in
router history state (never the URL, so a crafted link cannot choose the
arguments). The login PTY is opened uncached beside the user's shell and closed
when the page is left. In that mode
`terminal.open` accepts `agent` and `args` and runs the agent's installed
executable plus those args in the PTY with `NO_BROWSER=1`, resolved the same
way as the relay and refused the same way when it is not installed. The CLI therefore prints a login URL
and asks for the code instead of trying to open a browser on the server. The
browser never supplies a command. The API-key path is absent because LinuxIO
deliberately holds no vendor credentials; the CLI keeps its own login.

## Capabilities

- `node` resolves `npx` on the PATH the session user's interactive login shell
  reports, the same PATH agents are installed and executed with, so detection
  and execution agree. Per-user installs such as nvm, fnm or volta are
  therefore detected. It gates the Assistant route, the sidebar entry and the
  settings tab.
- `assistant_claude`, `assistant_gemini` and `assistant_codex` report whether
  the pinned version of that agent is installed for the session user.
- `claude_code` reports whether `claude` is on the session user's login-shell
  PATH.

The chosen agent starts only when its own capability is available, and
`claude` also needs `claude_code`. See [Capabilities](./capabilities.md).

## Out of scope in v1

- API keys.
- Terminal panes for commands the agent runs; the agent keeps its own
  terminal tools.
- Persisted "allow always" permission choices.
- A bridge-side session registry that survives websocket loss.
- Proactive alerts.
- Automatic agent updates: a newer agent arrives with a LinuxIO release and
  is installed when the user presses Install.
- Typed LinuxIO tools for the agent.
- Pane controls (buttons, inputs, selects), and the status lines and toasts
  a plugin draws (see [Plugin panes](#plugin-panes)).
- Updating an installed plugin (`claude plugin update`), plugins for Gemini
  CLI and Codex, and reloading plugins into a running chat.
