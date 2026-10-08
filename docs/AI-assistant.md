# Assistant

The Assistant page is a chat with a terminal coding agent (Claude Code, Gemini
CLI or Codex) running on the host as the logged-in user. The agent speaks the
[Agent Client Protocol](https://agentclientprotocol.com) (ACP, JSON-RPC 2.0
over stdin/stdout). The browser is the ACP client and renders the session; the
model runs at the vendor. LinuxIO runs no model, holds no API key and stores
nothing about conversations. The only thing it persists is the chosen agent id
(`assistant.agent` in the per-user config). Design background lives in the
[spec](./superpowers/specs/2026-10-07-assistant-acp-design.md).

## Relay

The bridge package `backend/bridge/handlers/assistant` registers one duplex
route, `assistant.open`, with the request `{agent}`. It spawns the agent as the
session user (privilege drop mirrors the terminal) and relays its stdio to the
yamux stream. The bridge never parses ACP.

The bridge first asks the user's interactive login shell for its PATH
(`bash -i -l -c 'printf %s "$PATH"'`, run as that user with stdin and stderr
detached), then resolves `npx` on that PATH and executes the agent directly
with it. Interactive matters: `~/.bashrc` returns early for non-interactive
shells, and that is where nvm, fnm and volta add themselves. Executing the
agent directly, rather than through `bash -i`, keeps bash's "no job control"
warnings out of the agent's stderr. Arguments are passed as an argv array and
never interpolated into a script. `NO_BROWSER` is not set on the ACP process.

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

| Id | Command |
|----|---------|
| `claude` | `npx @agentclientprotocol/claude-agent-acp` |
| `gemini` | `npx @google/gemini-cli --acp` |
| `codex` | `npx @agentclientprotocol/codex-acp` |

The table is `agents` in `backend/bridge/handlers/assistant/agents.go`. The
commands come from the ACP registry. The browser only ever sends an agent id;
it never supplies a command, and unknown ids are rejected before anything
spawns. Settings: `config.get` returns `assistant.agent`; `config.set`
`{assistant:{agent}}` stores it (empty clears, unknown ids error).

## Client behavior

The browser uses `@agentclientprotocol/sdk` (`frontend/src/api/acp/`: a
transport over the LinuxIO duplex stream, a connection helper and the login
probe). The client advertises only `auth.terminal` and declines the fs and
terminal client methods, so the agent uses its own tools. Messages are plain
text, images and file context. Permission cards show the agent's own options.
Stop sends `session/cancel`.

## Features

The agent drives most of the surface, so a control only appears when the agent
advertises the matching capability.

- **Composer bar.** Under the text field, one row: `+` (add an image or attach
  a file) and `/` (slash commands) on the left, then the model and effort of
  the agent's config options as compact selects, any other select or boolean
  option, and on the right the mode select and Send (a red Stop button while a
  turn runs). Changing one sends `session/set_config_option` or
  `session/set_mode` and replaces the local options with the agent's reply.
  Pieces the agent does not offer are hidden.
- **Status line.** While a turn runs, a line above the composer shows a spinner,
  a verb that cycles every 4 s (Thinking, Working, Reading, Checking,
  Pondering, Reviewing) and the elapsed time. It, and its timers, exist only
  while the turn runs.
- **Grouped tool calls.** Two or more consecutive tool calls render as one
  collapsed row, `N tool calls`, with a status dot (red if any failed, pulsing
  while any is pending or running, green otherwise) and a chevron that opens
  the individual calls. A lone tool call stays a plain row.
- **Side question.** When the agent advertises `session/fork`, typing
  `/btw <question>` in the composer opens a panel beside the chat (below it on
  narrow screens), also while a turn is running. It forks the
  current chat with `session/fork`, so the side chat has the same context,
  and answers independently while the main chat keeps streaming. Its
  permission requests appear inside the panel. Close hides the panel and
  keeps the side chat so a later `/btw` reopens it; delete discards it and, when the
  agent advertises `session/close`, closes the side session. Starting a new
  chat, loading another one or disconnecting discards it as well.
- **Images.** Paste or drop an image into the composer when the agent accepts
  image prompts. Thumbnails with a remove button appear above the text, and
  the image blocks are sent before the text.
- **Slash commands.** Typing `/` at the start of the composer lists the
  agent's `availableCommands`, filtered by what was typed. Enter or a click
  inserts `/name `, and the message is sent as ordinary text.
- **@-mentions.** The `@` button, or typing `@` at a word start, opens the file
  picker. The file is read through `filebrowser.read_text` and attached as an
  embedded resource (capped at 200 KB), or as a link when it is larger, cannot
  be read, or the agent does not take embedded context. Chips above the
  composer show the attachments.
- **Usage.** A context gauge in the composer bar shows the share of the
  context window used, from the latest `usage_update`. Its tooltip gives the
  tokens used and, when reported, the cost.
- **Steering.** When the agent supports `_session/steering`, the composer stays
  enabled during a turn and Enter injects the message into the running turn.
  If the agent answers `promptRequired`, the text is sent as a normal prompt
  once the turn ends.
- **Markdown.** Agent replies render as Markdown with GitHub tables,
  strikethrough, task lists and autolinks, no raw HTML. Links open in a new tab
  and code blocks have a Copy button. Fenced code is syntax-highlighted using
  the file editor's languages (blocks over 20,000 characters stay plain). User,
  thought and plan text stay plain.
- **Diff view.** Tool calls that carry a diff show it as a line diff with the
  file path as header, additions and removals tinted.
- **Delete.** When the agent advertises session deletion, each entry in the
  previous-chats menu has a delete button behind a confirmation. Deleting the
  open chat starts a new one, and a running chat cannot be deleted.
- **Auto-scroll.** The transcript follows new content while it is scrolled to
  the bottom and pauses when the user scrolls up.
- **Shortcuts.** Enter sends, Shift+Enter inserts a newline, Escape stops a
  running turn, Shift+Tab cycles the mode (never into `bypassPermissions`,
  which is chosen from the mode menu), Ctrl/Cmd+Shift+O starts a new chat. The
  header tooltips show them.
- **Text size.** The transcript and composer use the app body size, and code
  uses a smaller monospace size.
- **Titles.** A `session_info_update` title renames the chat in the history
  menu; untitled chats show their session id.

## Login

Login state belongs to the agent CLI, not LinuxIO. The Settings tab probes it:
it sends `initialize`, waits up to 2 s for an `_auth/status_update`, and
otherwise tries `session/new`, mapping JSON-RPC error `-32000` to "not logged
in". Agent-handled auth methods call `authenticate`; `logout` is offered when
the agent advertises it.

Terminal-type auth methods navigate to `/terminal` with the agent and args in
router history state (never the URL, so a crafted link cannot choose the
arguments). The login PTY is opened uncached beside the user's shell and closed
when the page is left. In that mode
`terminal.open` accepts `agent` and `args` and runs the agent's command plus
those args in the PTY with `NO_BROWSER=1`. The CLI therefore prints a login URL
and asks for the code instead of trying to open a browser on the server. The
browser never supplies a command. The API-key path is absent because LinuxIO
deliberately holds no vendor credentials; the CLI keeps its own login.

## The `node` capability

The capability `node` resolves `npx` on the PATH the session user's
interactive login shell reports, the same PATH the agent is executed with, so
detection and execution agree. Per-user
installs such as nvm, fnm or volta are therefore detected. It gates the
Assistant route, the sidebar entry and the settings tab, and it cannot be
installed from the UI. See [Capabilities](./capabilities.md).

## Out of scope in v1

- API keys.
- Terminal panes for commands the agent runs; the agent keeps its own
  terminal tools.
- Persisted "allow always" permission choices.
- A bridge-side session registry that survives websocket loss.
- Proactive alerts.
- Agent installation by LinuxIO.
- Typed LinuxIO tools for the agent.
