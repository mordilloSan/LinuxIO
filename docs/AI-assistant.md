# Assistant

The Assistant page is a chat with a terminal coding agent (Claude Code, Gemini
CLI or Codex) running on the host as the logged-in user. The agent speaks the
[Agent Client Protocol](https://agentclientprotocol.com) (ACP, JSON-RPC 2.0
over stdin/stdout). The browser is the ACP client and renders the session; the
model runs at the vendor. LinuxIO runs no model, holds no API key and stores
nothing about conversations. The only thing it persists is the chosen agent id
(`assistant.agent` in the per-user config). What the agent itself keeps
(sign-in, settings, chat history, memory) is in the agent's own folder, one
LinuxIO gives it: see [Profile](#profile).

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

The first data frame is `{"cwd":...,"home":...,"linuxio":"ready"}`: the
folder the agent runs in, where the page starts its sessions, and the user's
home (the same folder unless the agent [works in its profile](#profile)). When the process ends
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

## Profile

Every agent started by LinuxIO keeps its state in a folder of its own,
`~/.local/share/linuxio/profiles/<id>`, which the bridge names in the agent's
variable (`ProfileEnv`, `ProfileDir` and `Profile` in `acpagents`):

| Id | Variable | State in | The user's own copy uses |
|----|----------|----------|--------------------------|
| `claude` | `CLAUDE_CONFIG_DIR` | `profiles/claude` | `~/.claude` |
| `gemini` | `GEMINI_CLI_HOME` | `profiles/gemini/.gemini` | `~/.gemini` |
| `codex` | `CODEX_HOME` | `profiles/codex` | `~/.codex` |

Gemini CLI takes its variable as a home and makes `.gemini` inside it; Codex
runs inside `codex-acp`, which hands its environment on to the Codex
app-server it starts. Without the profile the agent in the Assistant and the
same agent in a terminal or an editor would be one: signing out in the editor
signed the Assistant out, and the model saved there was the Assistant's.

For Claude Code, in the profile, and so the Assistant's alone, are:

- the sign-in (`.credentials.json`), which may be another account than the
  terminal's;
- `settings.json` (model, permissions, hooks) and the user-level `CLAUDE.md`,
  skills and MCP servers;
- plugins and marketplaces, which are the ones the Plugins dialog lists;
- chat history, so `session/list` offers only chats of the Assistant;
- memory (`projects/*/memory/`) and caches.

Every process LinuxIO starts for an agent gets its variable: the chat and the
Settings probe (`assistant.open`), the Terminal sign-in and the
`claude plugin` commands. A start without it would read and write the user's
own folder.
The pane relay hands its environment on to the CLI. The Terminal sign-in runs
through the user's login shell, so there the variables are set by
`/usr/bin/env` after the shell's start-up files: a `~/.bashrc` that exports
`CLAUDE_CONFIG_DIR`, `GEMINI_CLI_HOME` or `CODEX_HOME` for the user's own
agent does not move the sign-in away from the profile the chat reads.

`Profile` makes the folder with `install -d -m 0700` run as the session user,
which also sets that mode again on a folder that exists: it holds a sign-in
and chats, and the bridge, which may be root, creates nothing in a home the
user controls. It is beside the install folder (`agents/<id>`), which an
install replaces, so reinstalling the agent keeps the profile. LinuxIO copies
nothing into it from the user's own folder: the Assistant starts signed out,
with no history, settings, plugins or memory, and what the user's own agent
holds stays where it is.

Each agent reads a folder in the folder it works in as that folder's project
settings: `.claude/`, `.gemini/` or `.codex/`. In the user's home that is the
user's own profile, whose model, hooks and plugins would be the chat's again.

Gemini CLI and Codex have no option to leave project settings out, so their
chats work in the profile folder (`WorkInProfile`): the relay starts the
agent there and sends it as `cwd` in the ready line, and the page starts every
session there. Gemini CLI then takes the folder as its own home and reads no
project settings at all; Codex finds no `.codex/` there. The user's home is
added to the workspace, so the agent's file tools and its write sandbox reach
it as they did when the chat worked in it: Gemini CLI gets
`--include-directories <home>` on its command line (`IncludeDirFlag`), since
its ACP side takes no further folders, and the page sends Codex the home as
`additionalDirectories` on every session request, which it does for any agent
that advertises `sessionCapabilities.additionalDirectories` and works
elsewhere than the home. Settings come only from the folder a session works
in, not from a folder added to it. Gemini CLI is also started with
`--skip-trust`: it keeps its list of trusted folders in the profile, where the
profile folder itself would start untrusted, and in an untrusted folder it
turns off every MCP server and all approval modes but the default. The folder
is the user's own (0700) and is LinuxIO's to name, which is what the trust
check is there to ask about. `codex-acp` marks the folders of every session it
starts as trusted itself.

Claude Code's chat works in the user's home, and Claude Code reads `.claude/` in the
folder it works in as that folder's project settings: there, `~/.claude`, the
other profile, whose model, hooks and plugins would be the chat's again. So
every session request to Claude Code (`session/new`, `load`, `fork`, `resume`)
carries `_meta.claudeCode.options.settingSources: ["user"]` (`SESSION_META` in
`frontend/src/api/acp/connection.ts`), and the CLI loads the profile's
user-level settings alone. A chat therefore reads no project file of the home
folder: not `~/CLAUDE.md`, `~/.claude/CLAUDE.md` or `~/.mcp.json`. The
`claude plugin` commands take no such option, so they run in the profile's
folder, where there is no `.claude/` to read: run in the home they list a
plugin as `~/.claude/settings.json` switches it, and change it there.

Shared with every other `claude` on the host: the managed settings under
`/etc/claude-code`, and the `claude` program itself.

Known limits:

- The adapter resolves a few settings by itself, for the folder the chat works
  in and whatever the setting sources: the default permission mode, the
  switches that turn auto and bypass modes off, the `availableModels` and
  `modelOverrides` lists and the effort level still come from
  `~/.claude/settings.json` when it sets them. A new chat's mode and effort
  are then replaced by the [chat defaults](#features), except that a mode
  switched off there is not offered.
- The Terminal sign-in is an interactive `claude` in the home folder, which
  loads project settings as any terminal session does; it signs in and is
  closed.
- Gemini CLI and Codex chats start in the profile folder, so a relative path
  or "look here" in a prompt starts there, not in the home.
- Gemini CLI looks for `.gemini/.env` in each folder from the one it works in
  up to `/`: when the profile has none, it reads the user's own
  `~/.gemini/.env` (which often holds `GEMINI_API_KEY`, and then signs the
  Assistant in with it). `~/.env` is read in the same walk.
- When the user's home is itself a git repository (dotfiles), both agents take
  it as the project root above the profile folder: Codex may read
  `~/.codex/config.toml` as project settings and `~/AGENTS.md`, and Gemini CLI
  `~/GEMINI.md`.
- Gemini CLI 0.67.0 reads one setting, `advanced.autoConfigureMemory`, from
  `$GEMINI_CLI_HOME/settings.json` rather than from `.gemini` inside it, so
  that setting in the profile's `settings.json` does not take effect.

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
  a file) and `/` (slash commands) on the left, then one chip for the model
  and effort of the agent's config options ("Opus 5.5 High"), any other
  select or boolean option, and on the right the mode select and Send (a red
  Stop button while a turn runs). The chip opens a menu as wide as the prompt:
  the models with their descriptions, then the model settings (Fast mode) as
  rows and effort as a stepped slider, which stay in view while the list
  scrolls. Picking a model closes the menu; the slider leaves it open and
  sends the level once the drag or key press ends. The slider is a stop of
  the menu's Up and Down keys, and Left, Right, Home and End move its level.
  Changing one sends `session/set_config_option` or
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
  the chat: it is in the Agents card. Its report is a row `← <title>
  reported` with the text as Markdown. The title opens the Agents card on
  that subagent. A message whose subagent is not known reads `a subagent`.
- **Read-only commands.** `transcript/readOnlyCommand.ts` decides which
  commands only read: a list of readers (`cat`, `ls`, `ss`, `df`, ...),
  read-only subcommands (`systemctl status`, `docker ps`, `btrfs filesystem
  usage`, ...) and, for either, the arguments that make it act (`find
  -delete`, `sed -i`, `sort -o`, `date -s`, `hostname NAME`, `git diff
  --output`, ...). `env` is read through: the command after its options and
  assignments, and the text of `-S`, is what is judged. Every segment of a
  pipeline must read, and a redirection to a file, input from a file or
  here-document, a command substitution or an unclosed quote makes the whole
  command count as acting; output sent to `/dev/null` or another descriptor
  does not. Quotes are read in one pass as the shell reads them, so a `|` or
  `>` inside a search pattern does not split or redirect, an apostrophe inside
  double quotes does not open a quote, and a substitution inside double
  quotes still counts. This is presentation only and errs towards "acting";
  permissions stay with the agent.
- **Turn footer.** A turn that ran in this page ends in a footer showing how
  long it took (in hours and minutes from an hour on) and, when the agent's
  `session/prompt` response carries `usage`, the output tokens
  (`14 s · 2.1k tokens`), with input and cache counts in the tooltip. The
  store appends a `turn` block when the prompt resolves; a failed prompt
  gets none, and replayed history has no footer, since the agent does not
  replay the numbers.
- **Reading column.** The transcript's scroller, the permission card, the
  status line and the prompt share one centred 860 px column, so the
  scrollbar sits on the edge of the prompt's box and the margins stay free
  for the dock's tabs. Wheel scrolling over a margin does not scroll the
  transcript. The area keeps the width of the tabs clear on both sides, so
  on a narrow window the column shrinks and the tabs never lie over the
  scrollbar. The user's prompt is a bubble sized to its text on
  the right; answers take the column.
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
  Copy read at full text colour. Once the clipboard has the code, Copy reads
  "Copied" for two seconds in the theme's success colour with a tick; when the
  write is refused (a plain-HTTP origin has no Clipboard API) it reads "Copy
  failed" in the error colour, and a screen reader hears either from a status
  line (`useCopyResult` in `components/ui`, which the Copy logs buttons of the
  logs pages share through `AppCopyIconButton`). It is not the theme's code
  block colour
  (`codeBackground`), which is the surface of the log views. Fenced code is
  syntax-highlighted using
  the file editor's languages (blocks over 20,000 characters stay plain). User,
  thought and plan text stay plain. Paragraphs and lists are spaced in `em`,
  so the air between them grows with the chat's text size.
- **Diff view.** Tool calls that carry a diff show it as a line diff with the
  file path as header, additions and removals tinted.
- **Chats.** The page header holds one action, New chat. The previous chats
  are a tab on the page's right edge: it opens a card that floats beside the
  chat, as tall as its list, with a search field and the chats grouped under
  Today, Yesterday and Earlier by when they were last used. The open chat is
  marked, and clicking another loads it. The tab is disabled when the agent
  cannot list sessions. The tabs stay in place while a card is open. Opening a
  card never closes another (the chats, the agents, the activity map, a
  [plugin pane](#plugin-panes)): the open cards stack in one column beside the
  tabs (`dock/DockCard.tsx` places each in `.assistant-dock-cards`), always in
  the same order from the top (chats, agents, activity, then a pane), whichever
  was opened first, and closing a card moves the ones below it up, with no
  gap. All the cards have one width, 340 px to begin with, which the user
  changes by dragging the left edge of any of them or with the arrow keys; the
  browser remembers it (`dock/dockWidth.tsx`). On a short window the cards
  share the height and scroll inside.
- **Activity map.** The Activity tab, under Agents, opens a card that draws
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
    out with a wide ring from the core; a call at You (a question, the plan)
    pulses as any call does. A station is lit for 3.2 s after
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
    the last two parts of a file's path, a web address's host (also when
    it is written without `https://`; the tool's name when it has none,
    never the address), a subagent's
    title, a skill's name. Finished calls in a row at one station, by the
    chat or by one subagent, fold into one row (`+2`) with their times
    added. A call still running is not pushed out by newer rows: it stays
    at the end of the list, in place of the oldest rows that no longer run,
    for as long as it runs, so the list agrees with the station still at
    work. Such calls take eight rows at most, and with more of them the
    last says how many more still run, wherever they are (`+3`). Each row
    and the line above them are one line, cut at the card's edge, with the
    time in a column at the right, so a long label neither wraps nor moves
    the rows while the work goes on. A time is rounded before its unit is
    chosen (`113 ms`, `13.4 s`, `2 min 5 s`, never `1 min 60 s`), and from
    an hour on it reads in hours and minutes (`1 h 5 min`). A row whose
    label is longer than 28 characters has a `▸` that shows it whole and
    closes it again; one row is open at a time.
  - *What it knows.* A call is sorted by Claude Code's tool name when the
    adapter sends one (`_meta.claudeCode.toolName`) and by its ACP kind
    otherwise. A shell command line (`workmap/shell.ts`) goes to Network
    when any command it runs reaches out, to Files when every one is a file
    command (`cat`, `rg`, `ls`…), and to Shell otherwise; `echo`, `cd` and
    the words that only set something (`export`, `shopt`, `umask`,
    `pushd`…) say nothing either way. A command reaches out by its name
    (`curl`, `ssh`, `gh`, `kubectl`, `aws`…) or with its subcommand:
    `git push`, `npm install`, `pip install`, `docker pull` and
    `podman pull`, and the package managers' installs, updates and
    refreshes (`apt`, `dnf`, `yum`, `zypper`, `pacman -S`, `apk`, `snap`,
    `flatpak`). The command that counts is the one run behind `sudo`,
    `env`, `timeout`, `nice`, `xargs`, `chroot`, `runuser` and the like
    (past their flags, a long one that takes a word as its short one:
    `timeout --signal KILL 5 curl x` is `curl`),
    after `do`, `then` or `!` and the assignments and redirections before
    it (`2>/dev/null ls` is `ls`, and the text of `<<<"…"` is never a
    name), inside `$(…)`, backticks (also backticks written inside
    backticks) or a function's body, in the line handed to
    `bash -c`, `su -c`, `eval` or `watch`, and by `find -exec`. A `;` or
    `|` in quotes splits nothing, a comment is not read, a `case` pattern
    on any branch (`a|b)`, `(a|b)`) runs nothing but the commands after
    its `)` do, the list of an array (`a=(…)`, `a+=(…)`, also after
    `declare` or `local`) is values and only what `$(…)` or `<(…)` runs
    inside it is a command (a `(` after `=` in `[[ … ]]` is a regex, not a
    list), the text of a here-document is not run unless a shell reads it
    (`cat <<EOF > f` and a commit message written with `"$(cat <<'EOF'`,
    or with backticks in place of `$(…)`, are text, `sudo bash <<EOF` and
    `bash -s 2>&1 <<EOF` are read as commands). Its mark is a word with
    its quotes taken off (`<<E"O"F`, `<<"END MSG"`, `<<'1'`) that starts
    with a letter, `_`, a quote or a backslash; it ends at the line that
    is its mark, and inside `$(…)` or backticks also where they close, as
    it does in bash: at a `)` after the mark (`EOF)`) and at the first
    backtick, in its text or on the line of its `<<`, so what follows is
    read. Only the first 8,000 characters of
    a line are read, and a longer one that shows nothing reaching out
    goes to Shell, never to Files, since the rest may run anything. A
    line follows no more than 256 commands run inside others (in `$(…)`,
    behind `find -exec`, in the line handed to `bash -c`): one that holds
    more is read no further and goes to Shell. A row with no description is
    named by the first command that says something (`make` for
    `cd /srv && make`). Known limits: a command kept in an array and run
    later (`cmd=(curl …); "${cmd[@]}"`) lands on Shell, where lot-map shows
    Network, a here-document whose bare mark starts with anything but a
    letter or `_` (`<<1`, `<<$X`, `<<!`) is taken for a shift, which it
    far more often is, so its text is read as commands, the line handed
    to `env -S` is not read, an array element whose index holds a space (`m[a b]=1`)
    is read as a command, and so are the patterns of a `case` whose `in`
    stands on a line of its own, while a `case` inside `$(…)` ends the
    `$(…)` at the `)` of its first pattern unless that opens with `(`.
    A line of file commands that names `projects/*/memory/` of the
    [profile](#profile) or of `~/.claude` goes to
    Memory, as does a file under it; an MCP tool goes by its server
    (`Claude_Browser` and `claude-in-chrome` to Browser, `terminal`
    to Shell, a server named for memory to Memory, any other to Network),
    and so does a call that lists or reads an MCP server's resources. A
    cloud session linked to the user's computer reaches the browser as
    `remote-devices` tools named `Claude_Browser__…`, which go to Browser;
    its other tools go to Network. Claude Code's other tools go by what
    they are for: the plan, the task list, a notification and review
    findings to You; code intelligence (`LSP`) to Files; schedules,
    routines and the list of agents to Agents; an artifact's checks,
    comments and data, a design, a shared guide and a published plugin to
    Network. Such a call is labelled with the tool's name, never with what
    it was given. A tool the map does not know goes to Shell. A call starts
    a new step of the model when the model said nothing just before it and no
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
    two subagents at work together mix, but they never fold into one row,
    nor into one of the chat's own. In a chat that was loaded nothing
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
    Claude Code's hook events, so they never reach the browser. A command
    sent to the background reads as finished the moment it is handed off,
    and its station rests: the adapter ends the call then, and tells of the
    command's own life only to a client that declares its `asyncTasks`
    extension, an ACP v2 background list or a terminal, none of which
    LinuxIO does (read from the source of `claude-agent-acp` 0.102.0). A subagent's
    own calls fly from the core, not from the Agents station; a call a hook
    refused is an ordinary failure; and compaction is not a step.
  - *Card.* It shares the plugin pane's box, blending and resizing, is
    drawn only while open, and the browser remembers that it was open and
    reopens it where it fits beside the chat. The picture stays pinned at the
    top of the card at its own proportions; what runs now and the list of
    what happened are under a **Details** button, closed until it is
    clicked, and scroll inside the card. Opening it leaves the other cards
    open; an open side question hides them.
- **Agents.** The tab under Chats counts the subagents at work
  (`0 agents`, `2 agents`); its dot is grey while none runs and pulses while
  one does. It opens a card of its own
  (`-components/workmap/AgentMap.tsx`), which the map's Agents station opens
  too, and which stays open beside the Activity card: each tab opens and
  closes only its card, and the browser remembers that each was open. The
  card draws the chat's agent, with its model and the tokens in its
  context, and the subagents it sent off joined to it by lines: beside it on
  a wide card, under it on a narrow one, and the subagents they sent off
  under them. Each subagent shows how it stands when it is not simply
  finished (`running`, `waiting for you`, `stopped`, `failed`, `ended`,
  `disconnected`), its kind, and how long it has run or took. Choosing the
  chat's agent opens the map; choosing a subagent, here or by its
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
    the first start and the last end. A run still going counts in whole
    seconds, and from an hour on in hours and whole minutes. The agent
    gives no time or tokens of its own for a subagent, so a chat that was
    loaded shows no time.
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
  so until then the terminal waits and says so. Into a shell the page has
  just started, one line waits the same way, for up to 1.5 s: typed before
  the shell reads, the terminal echoes it above the prompt and the shell
  shows it again after it. That holds for the shell's first 1.5 s however
  the terminal comes to it, a second setup or a return to the page included.
  The command is removed from
  history state once taken, so a reload does not type it again, and nothing
  is typed into an agent login PTY. Code in a plugin pane or a side question
  has Copy only.

  The terminal can also sit in a browser tab beside the chat
  (`utils/terminalTab.ts`). A click first asks, on a `BroadcastChannel`,
  whether another tab of this server has its shell terminal up, and waits
  200 ms for an answer. A tab that answers gets the command and the chat
  stays where it is; with several, the first to answer gets it. With none, a
  middle click or Ctrl+click (Cmd on a Mac) opens `/terminal` in a new tab
  and hands the command over once that terminal says it is up, within 30 s
  (a second click in that time replaces the command the tab will get),
  and a plain click navigates as above; so does a middle click when the
  browser refuses the tab. The button's tooltip names the middle click. Each
  browser tab has a shell of its own, so every step of an answer lands in the
  one shell of that tab, where an earlier `cd` still holds. The channel
  carries the command for the same reason history state does: only a page of
  this origin can post to it, never a link. The chat opens its tab under a
  window name of its own, which is the name that tab answers by, so the
  command for a new tab goes to that tab and to no other terminal. The
  terminal passes what it receives through `typedCommand.ts`, and a terminal
  showing an agent login, or one whose shell has ended, does not answer.

  Nobody is looking at the terminal tab when such a command arrives, so it
  is typed only at a prompt: a shell that turns bracketed paste on and off
  (bash 5.1 and later, zsh, fish) has it on exactly while it waits at its
  prompt, and a command that arrives while a program has the terminal (an
  editor, a pager, a running command) waits, with a line in the terminal
  saying so, and is typed when the prompt is back. A shell never seen to
  touch the mode (an older bash) gives no such sign, and one line is typed
  into it as it is on the page's own visit. What the page remembers of a
  shell from its last visit holds only until the shell next sets the mode.

  Known limits: a page can bring to the front only a tab it opened, and not
  in every browser, so the chat shows "Sent to your terminal tab" when it is
  still the tab in view 300 ms later. A terminal tab that has moved to
  another page, or that the browser has put to sleep, does not answer, and
  the click then acts as if there were none. Reloading the chat forgets which
  tab it opened; the command still reaches it, without the tab coming
  forward. The new tab is opened 200 ms after the click, once no terminal
  has answered; a browser that allows a new tab only inside the click itself
  refuses it, and the terminal opens in place of the chat (checked in
  Chromium only). A tab that never brings a terminal up within the 30 s,
  such as one that lands on the sign-in page, drops its command without a
  message.
- **Auto-scroll.** The transcript follows new content while it is scrolled to
  the bottom, pauses when the user scrolls up and follows again once they are
  back at the bottom, so a loaded chat opens at its newest message
  (`useFollowBottom`). It also follows what grows without a render, watched by
  a `ResizeObserver` on the scroller and its column: web fonts swapping in
  after the first paint, code being coloured, the window shrinking. Only a
  move up pauses it, since the scroll event of its own jump can arrive after
  more content landed and read as "not at the bottom".
- **Shortcuts.** Enter sends, Shift+Enter inserts a newline, Escape stops a
  running turn, Shift+Tab cycles the mode, Ctrl/Cmd+Shift+O starts a new chat.
  The header tooltips show them.
- **Text size.** The transcript and composer use the app body size, and code
  uses a smaller monospace size. The chat starts at the app's
  body size (13px); the Text size row at the foot of the composer's `+` menu
  changes it, and the choice is kept in the browser. The dock (its tabs and
  cards) keeps the app's body size whatever the chat's text size is.
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
same day, with two subagents asking at once. No chat has shown whether a
subagent Claude Code moves to the background by itself gets its launch note
as a report.

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
  and the Agents card.

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
`claude plugin ... --json`, in the Assistant's [profile](#profile), and never
edits the agent's settings files itself.

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

- **Who runs it.** Every command runs as the session user, in the
  [profile](#profile)'s folder, with
  the `claude` on their login-shell PATH (409 when there is none), standard
  input closed and arguments passed as an argv array. It writes only under
  that user's profile folder, so no route is privileged: it is what the user
  can already type in the Terminal page with `CLAUDE_CONFIG_DIR` set. Reads have 30 s, writes 10 min, and a
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
  `/usr/libexec/linuxio/linuxio-claude-relay` is missing, the stream is exactly what
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
  left edge (or the left and right arrow keys on it) resizes it between 240 and 720 px, a
  double click restores 340 px, and the width is kept in the browser. The
  pane has a tab on the page's right edge, under the Chats tab: the card's
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

Login state belongs to the agent CLI, not LinuxIO. Each agent keeps it in its
[profile](#profile), so the Assistant is signed in by itself: a sign-in in a
terminal does not sign the Assistant in, and a logout here leaves the
terminal's. The Settings tab probes it:
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
