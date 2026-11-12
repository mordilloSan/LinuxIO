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
mixed. The client advertises only `auth.terminal` and declines the fs and
terminal client methods, so the agent uses its own tools. Messages are plain
text, images and file context. Permission cards show the agent's own options
and say that nothing has changed yet.
Stop sends `session/cancel`.

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
  chat, loading another one or disconnecting discards it as well.
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
  background" under Settings → Theme). A new configuration starts it at the
  default page background a shade darker; a configuration without the key
  derives it from its own page background (18% darker on dark, 3% on light),
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
  card is open at a time (the chats or a [plugin pane](#plugin-panes)), so
  two cards never run past the bottom of a short window.
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
  running turn, Shift+Tab cycles the mode (never into `bypassPermissions`,
  which is chosen from the mode menu), Ctrl/Cmd+Shift+O starts a new chat. The
  header tooltips show them.
- **Text size.** The transcript and composer use the app body size, and code
  uses a smaller monospace size. The chat starts at the terminal's font size;
  the Text size row at the foot of the composer's `+` menu changes it, and
  the choice is kept in the browser.
- **Titles.** A `session_info_update` title renames the chat on the Chats
  card; untitled chats show their session id.

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
  the page background 30% darker (a new configuration starts at `#131B25`),
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
