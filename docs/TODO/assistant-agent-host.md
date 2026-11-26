# Assistant agent host: chats that outlive the page

## Status

Not built. This is the proposed plan; the points under "To settle" need the
owner's decision before step 1. The chat address (`/assistant?chat=<id>`,
[Assistant](../AI-assistant.md#relay), "Chat lifetime") shipped ahead of it
and stays: the id names a chat the host holds just as well as one on disk.

## Problem

- **A chat lives exactly as long as the page's stream.** The bridge starts
  the agent for an `assistant.open` stream and hangs it up when the stream
  closes (`backend/bridge/handlers/assistant/relay.go`, SIGHUP to the process
  group, SIGKILL after 5 s). A reload, a closed tab, a laptop going to sleep
  or a VPN dropping the websocket stops the reply mid-turn, and loses the open
  permission request, the queued prompt and the side chat. On a server the
  admin is remote and the long work (an upgrade, a build, a migration) is what
  gets cut.
- **One chat per page.** The store holds one chat; updates for any other
  session are dropped, and switching chats is refused while a turn runs
  (`assistant-store.ts`).
- **One agent per tab.** Two tabs on one chat run two agent processes on one
  session (the known limit in [Assistant](../AI-assistant.md#relay)).
- **More harnesses are coming** (OpenCode, OpenClaw, others that speak ACP),
  so the answer cannot be specific to one of them.

The chat history is not the problem: each agent writes it to its own profile
folder, and it survives all of the above.

## Why keeping the process alive is not enough

ACP is a JSON-RPC conversation, and the page holds the client half of it: the
answer to `initialize`, the id of the prompt still waiting for its answer, and
the permission requests the agent is waiting on. A page loaded afterwards
cannot take that half over mid-turn, whoever keeps the process running. So the
client half moves to the server, into something that speaks ACP to the agent
for as long as the agent runs, keeps what each chat has sent, and lets pages
attach and leave.

## Decisions

**A per-user agent host, `linuxio-agents@<uid>`, runs the agents.** The
reasons, in the order they were weighed:

- Not the bridge. For a privileged login the bridge runs as root, and the host
  reads everything the agent writes, which the model controls: root would be
  parsing it. The bridge also ends at sign-out and at the webserver's 60 min
  idle expiry, which a closed tab reaches. The bridge keeps never parsing ACP.
- Not the job host. It is built for mutations with progress: it runs with
  `NoNewPrivileges=yes` (which would take `sudo` away from the agent's
  commands, which have it today), one `MemoryMax=2G` covers all of a uid's
  jobs, its 60 s idle exit and its interruption and resume bookkeeping are
  about jobs, and a package update makes it wait for jobs, not chats. A chat
  is not a job.
- A unit of its own, modelled on the job host: a template per uid with
  `User=%i`, a socket `/run/linuxio/agents/<uid>.sock` (`0600`, owned by the
  uid, `Accept=no`, `FileDescriptorName=linuxio-agents`), started on the first
  connection. The program is `linuxio-bridge`, which is the agent host when
  systemd passes it that socket, as it is the job host for `linuxio-jobs`: no
  new program and no verb, and the package keeps its nine programs.

**Agents run as the session user, always.** The host of uid U runs as U, and
a privileged login's bridge (root) connects to U's host, never root's. The
host accepts a peer whose `SO_PEERCRED` uid is U or 0: root can already do
anything U can, so admitting it adds nothing, and no other local user can
connect. A process of U's own can connect and drive U's agents, which gives it
nothing it lacks: it can run `claude` itself.

**ACP in, ACP out.** The page keeps speaking ACP, to the host instead of the
agent, and the host speaks ACP to the agent. The browser's transcript,
permission and subagent code stays. What the host adds are ACP extension
methods (`_linuxio/…`, ACP's convention for methods of one implementation):
attach, the list of what it holds, and starting a harness. A new harness is
still a row in `acpagents` with a pinned version, and the host does not change
for it. There is no LinuxIO protocol of its own between the page and the host.

**One process per harness, many sessions in it.** The `/btw` side chat
already runs a second session beside the main chat on one ACP connection to
one `claude-agent-acp` (which starts a `claude` of its own for each). A
Claude chat and a Codex chat can run at the same time; each chat records its
harness, and `assistant.agent` becomes the harness a new chat starts with.

**The bridge relays bytes.** `assistant.open` becomes an attach: the bridge
connects to the session user's host and relays the stream, as it relays the
agent's stdio today. The plugin pane lines (`{"linuxio":"pane",…}`) ride the
same stream, and the relay socket of plugin panes moves to the host with the
process that uses it.

**What the host keeps for each session it holds**, from the moment it loaded
or created it in that process: every `session/update` (bounded), the open
permission requests, the prompt in flight and the queued one, and the
session's modes, options and usage. A page that attaches gets that record
replayed in order and then the live updates. A permission request goes to
every attached page, the first answer wins, and the others see it withdrawn.
A `session/load` of a session the host holds is answered from its record and
never sent to the agent again; any other goes to the agent as today.

**What the host never does.** It never downloads or installs anything (the
relay's rule stays: the pinned version or a 409), sends a prompt nobody sent,
answers a permission request by itself, or starts an agent without the
variables of `acpagents.Profile`.

## How it runs

```
login (linuxio-auth, after PAM, beside the job socket's line):
    systemctl start --no-block linuxio-agents@U.socket     for every login, privileged too
page → bridge:   assistant.attach  (duplex)
bridge → host:   connect /run/linuxio/agents/U.sock, hello {protocol, uid}, then relay bytes
page → host:     _linuxio/attach → the harnesses running and the sessions held,
                 each with its status (running, waiting for an answer, ready)
                 and when it last changed
page → host:     _linuxio/start {agent} for a harness not running yet; the host
                 starts it (login-shell PATH, pin check, profile variables,
                 its own process group), initializes it once with LinuxIO's
                 client capabilities and keeps the answer for later pages
page → host:     session/load of the chat in ?chat= (or the newest), answered
                 from the record when the host holds it
```

Lifetime:

- A page leaving (reload, closed tab, lost websocket) detaches. Nothing stops.
- The host stays up while an agent runs a turn or a page is attached. A
  harness with no turn running and no page attached is stopped after an idle
  time, and the host exits 60 s after its last agent and page are gone.
- The unit has `MemoryMax=` and `TasksMax=` of its own, and the host refuses a
  harness above a per-user count, so agents stop counting against the login's
  session scope.
- An update that replaces the binary: as with the job host, a host on the old
  binary refuses a newer bridge's hello. What it does with running chats is
  under "To settle".

## The page

- The store holds chats keyed by session id (and harness): the transcript,
  status, permission requests, subagents, usage, queued prompt, side chat,
  modes and options that are single today.
- The chat on screen is the one in `?chat=`. Since switching while a chat runs
  is now allowed, the address can be pushed rather than replaced, so Back
  steps through chats; the reason it is replaced today goes away.
- The Chats card marks each chat as running, waiting for an answer, or
  finished since it was last seen.

## Harnesses

- **CLI harnesses** (Claude Code through `claude-agent-acp`, Gemini CLI, Codex
  through `codex-acp`, later OpenCode's `opencode acp`): the host owns their
  process and so their lifetime.
- **Daemon harnesses** such as OpenClaw: `openclaw acp` is an ACP agent on
  stdio that forwards to a running OpenClaw Gateway and maps ACP session ids
  to Gateway sessions, so the Gateway keeps working when that process ends. It
  fits as a row. Its documentation lists the client filesystem methods and
  per-session MCP servers as unsupported; LinuxIO declares no filesystem
  methods and sends `mcpServers: []`, so neither is lost. Installing and
  running the Gateway itself is a separate feature with its own review (an
  always-on agent taking messages from chat channels with the user's rights,
  never as root), and is not in this plan.
- `session/resume`, stable in ACP since April 2026, reopens a session without
  a replay. Where a harness advertises it, the host uses it to reopen a
  session it holds after that harness's process ended, since its record
  already has the history.

## Steps

### Step 1: the host and attach, one chat at a time

The page behaves as today, except that a reply keeps running when the page
goes away and a page that comes back sees it.

- `linuxio-agents@.service` and `.socket`, the line in `linuxio-auth.c` beside
  the job socket's (and its tests in `linuxio-auth_test.c` and
  `linuxio-auth-pam_test.c`), the package's unit lists.
- The host mode of `linuxio-bridge`; the hello and its protocol number.
- The start of an agent moved from `relay.go` into the host, with the pin
  check, the login-shell PATH, the profile variables and the process-group
  signals.
- The ACP proxy: request ids mapped per page, `initialize` answered from the
  harness's kept answer, the session record and its replay, first answer wins
  for permission requests, the bound on the record.
- The bridge's attach route and its generated contract; the page's connection
  over it.
- Docs: a new doc for the host in `docs/` (or a section of
  `AI-assistant.md`), `process-systemd-architecture.md` (units, the binary
  invocation map, the login sequence), `packaging.md`.
- Tests: Go unit tests of the proxy (id mapping, replay order, first answer
  wins, the bound); a browser spec with a fake host that reloads mid-turn and
  sees the reply finish; in the VM, the unit as the package installs it and
  after a reboot, a reload mid-turn, and a privileged login's agent running as
  the user.

Roughly 30–40 files, across the backend, `linuxio-auth`, packaging, the
frontend and the docs. It adds a socket only its uid and root can connect to,
and a unit; no root route.

### Step 2: several chats in one page

The store keyed by chat, the Chats card's marks, the pushed address.
Roughly 12–15 files, frontend only.

### Step 3: the policies

Sign-out, the limits, the idle stop and how a waiting chat reaches the admin,
as settled below.

### Step 4: more harnesses

OpenCode and OpenClaw (`openclaw acp`) as rows of `acpagents`, with their pins
and the capability installer.

## To settle

- **Sign-out.** Stop the user's agents, keep them running with no one signed
  in, or ask when any are running. The proposal is to ask.
- **sudo.** Today's agents can run `sudo` (the login's process tree has no
  `NoNewPrivileges`). Keep that on the unit, or take it away.
- **Limits.** Harnesses per user, `MemoryMax=` and `TasksMax=`, the size of a
  session's record, the idle time before a harness is stopped.
- **An update while chats run.** The old host keeps its chats until their
  turns end and a new page cannot attach meanwhile, or the update ends them.
- **A chat waiting for an answer with no page open.** Alerts are written by
  root to `/var/lib/linuxio/alerts.json`, which a host running as the user
  cannot write; the page can show it at the next attach, and anything sooner
  needs a path to the alert store.
- **Two pages sending to one chat.** One prompt in flight per session, and a
  second send queues or steers as the composer does today.

## Not planned

- Agents owned by the bridge: root would parse the agent's output for a
  privileged login, and they would still end with the sign-in.
- Agents in the job host: the reasons under Decisions.
- A protocol of LinuxIO's own between the page and the host: every harness
  speaks ACP, and a second protocol would have to be translated for each.
- Installing or running the OpenClaw Gateway.
