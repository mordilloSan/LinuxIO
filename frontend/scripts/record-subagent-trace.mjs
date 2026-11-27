#!/usr/bin/env node
/**
 * Records src/test/assistantSubagentTrace.json again: a real chat with Claude
 * Code's adapter at the version LinuxIO pins, run as the Assistant runs it
 * (the agent installed under ~/.local/share/linuxio/agents/claude, its
 * profile, `subagents` declared), with every update it sent and the same chat
 * loaded again. The chat is on the owner's Claude login, so the owner runs it:
 * `make record-subagent-trace`. See docs/AI-assistant.md, "Subagent sessions".
 *
 * Messages are read off the wire, before the SDK parses them, so the recording
 * is what the adapter sent. The recording replaces the fixture only when the
 * chat went as the tests replay it; otherwise it is left in
 * .cache/subagent-trace/trace.json with the reasons.
 */
import { spawn } from "node:child_process";
import { createWriteStream } from "node:fs";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

import * as acp from "@agentclientprotocol/sdk/unstable";

const frontend = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
);
const repo = path.dirname(frontend);
const fixture = path.join(frontend, "src/test/assistantSubagentTrace.json");
const testFile = path.join(
  frontend,
  "src/routes/_authenticated/assistant/-components/subagentTrace.test.ts",
);
const pinsFile = path.join(
  repo,
  "backend/bridge/internal/acpagents/acpagents.go",
);
const cacheDir = path.join(repo, ".cache/subagent-trace");

const PACKAGE = "@agentclientprotocol/claude-agent-acp";
const TURN_TIMEOUT_MS = 5 * 60_000;
// The Sleeper is stopped this long after its first call, or after the
// fallback when it makes none, as the Stop button of the Agents view would.
const STOP_AFTER_CALL_MS = 3_000;
const STOP_FALLBACK_MS = 30_000;
// Updates the adapter sends after answering session/load.
const LOAD_SETTLE_MS = 1_500;
const NAMES = ["chat", "reader-a", "reader-b", "nested-c", "sleeper"];
const MARKS = ["prompt", "answered", "prompt", "cancel:sleeper", "answered"];
const WORKDIR = "/srv/work";

const FILES = {
  "a.txt": "alpha\nbeta\ngamma\n",
  "b.txt": "one\ntwo\n",
  "c.txt": "nested file\n",
};

const PROMPTS = [
  "This is a test of subagents. Do exactly this, and nothing more:\n1. In ONE message, start two subagents with the Agent tool (subagent_type general-purpose).\n   - Subagent 'Reader A', in the foreground: read a.txt, run the shell command `wc -l a.txt`, then write the file out-a.txt containing the single line `done A`. It reports the line count.\n   - Subagent 'Reader B', with run_in_background true: read b.txt, run the shell command `wc -l b.txt`, then start a subagent of its own (Agent tool, general-purpose, called 'Nested C') that reads c.txt and reports its content. If it has no Agent tool, it says so in its report. It reports what it found.\n2. When Reader A has finished, use SendMessage to ask Reader A: 'What was the first word of a.txt?' and wait for its answer.\n3. Wait for Reader B to finish, then give me a three-line summary.",
  "Start one subagent in the foreground (Agent tool, general-purpose, called 'Sleeper') that runs the shell command `sleep 40` and then reports done. Then tell me what it reported, in one line.",
];

function fail(message) {
  console.error(`❌ ${message}`);
  process.exit(1);
}

function log(message) {
  console.log(`   ${message}`);
}

async function pinnedVersion() {
  const source = await fs.readFile(pinsFile, "utf8");
  // update-agent-pins reads the same pair: a Package line, then its Version.
  const match = source.match(
    /Package:\s*"@agentclientprotocol\/claude-agent-acp",\s*\n\s*Version:\s*"([^"]+)"/,
  );
  if (!match) fail(`no ${PACKAGE} pin in ${path.relative(repo, pinsFile)}`);
  return match[1];
}

async function onPath(name) {
  for (const dir of (process.env.PATH ?? "").split(":")) {
    if (!dir) continue;
    const file = path.join(dir, name);
    try {
      const info = await fs.stat(file);
      if (info.isFile()) {
        await fs.access(file, fs.constants.X_OK);
        return file;
      }
    } catch {
      // not here
    }
  }
  return null;
}

// What the bridge gives the agent (acpagents.Profile and Resolve), without
// what a Claude Code this script may run inside would pass on to it.
function agentEnv(profile, cli, cwd) {
  const env = { ...process.env };
  for (const name of ["CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT"]) delete env[name];
  env.CLAUDE_CONFIG_DIR = profile;
  env.CLAUDE_CODE_EXECUTABLE = cli;
  env.PWD = cwd;
  return env;
}

function lines(onMessage) {
  const decoder = new TextDecoder();
  let buffer = "";
  return (chunk) => {
    buffer += decoder.decode(chunk, { stream: true });
    let end;
    while ((end = buffer.indexOf("\n")) >= 0) {
      const line = buffer.slice(0, end).trim();
      buffer = buffer.slice(end + 1);
      if (!line) continue;
      try {
        onMessage(JSON.parse(line));
      } catch {
        // not a JSON-RPC line
      }
    }
  };
}

/**
 * Starts the adapter and connects to it the way connection.ts does, with a
 * tap on both directions that records the session/update notifications and
 * the marks (prompt sent, prompt answered, subagent stopped, chat loaded) in
 * the order they crossed the wire.
 */
function startAdapter({ bin, env, cwd, logFile, chatId, onUpdate }) {
  const child = spawn(bin, [], { cwd, env, stdio: ["pipe", "pipe", "pipe"] });
  child.stderr.pipe(logFile, { end: false });
  const exited = new Promise((resolve) => child.once("exit", resolve));

  const state = {
    entries: null,
    t0: 0,
    permissions: [],
    sent: new Map(),
  };
  const push = (entry) => {
    if (state.entries)
      state.entries.push({ t: Date.now() - state.t0, ...entry });
  };

  const tapOut = lines((message) => {
    if (message.method && "id" in message) {
      state.sent.set(message.id, message.method);
    }
    if (message.method === "session/prompt") push({ mark: "prompt" });
    if (
      message.method === "session/cancel" &&
      message.params?.sessionId !== chatId()
    ) {
      push({ mark: `cancel:${message.params.sessionId}` });
    }
  });
  const tapIn = lines((message) => {
    if (message.method === "session/update") {
      push({
        sessionId: message.params.sessionId,
        update: message.params.update,
      });
      onUpdate(message.params);
    } else if (message.method === "session/request_permission") {
      state.permissions.push(message.params);
    } else if ("id" in message && !message.method) {
      const method = state.sent.get(message.id);
      state.sent.delete(message.id);
      if (method === "session/prompt") push({ mark: "answered" });
      if (method === "session/load") push({ mark: "loaded" });
    }
  });

  const input = new ReadableStream({
    start(controller) {
      // After the SDK closed the connection the stream is closed too, and
      // what the adapter still writes is dropped.
      child.stdout.on("data", (chunk) => {
        tapIn(chunk);
        try {
          controller.enqueue(new Uint8Array(chunk));
        } catch {
          // closed
        }
      });
      child.stdout.once("end", () => {
        try {
          controller.close();
        } catch {
          // closed
        }
      });
    },
  });
  const output = new WritableStream({
    write(chunk) {
      tapOut(chunk);
      return new Promise((resolve, reject) =>
        child.stdin.write(chunk, (error) =>
          error ? reject(error) : resolve(),
        ),
      );
    },
    close() {
      child.stdin.end();
    },
  });

  const connection = acp
    .client({ name: "linuxio" })
    .onRequest(acp.methods.client.session.requestPermission, (ctx) => {
      // The tests replay a chat that asked nothing; answer so the turn ends,
      // and the recording is rejected afterwards.
      const option =
        ctx.params.options.find((o) => o.kind === "allow_once") ??
        ctx.params.options[0];
      return { outcome: { outcome: "selected", optionId: option.optionId } };
    })
    .onNotification(acp.methods.client.session.update, () => {})
    .connect(acp.ndJsonStream(output, input));

  const initialize = () =>
    connection.agent.request(acp.methods.agent.initialize, {
      protocolVersion: acp.PROTOCOL_VERSION,
      // The same as connection.ts, so the adapter sends what the page gets.
      clientCapabilities: {
        auth: { terminal: true },
        session: { configOptions: { boolean: {} }, promptSuggestions: {} },
        subagents: {},
      },
    });

  const stop = async () => {
    connection.close();
    child.stdin.end();
    const timer = setTimeout(() => child.kill("SIGTERM"), 10_000);
    await exited;
    clearTimeout(timer);
  };

  const record = (entries) => {
    state.entries = entries;
    state.t0 = Date.now();
  };

  return { connection, initialize, stop, record, state, exited };
}

function withTimeout(promise, what) {
  let timer;
  return Promise.race([
    promise,
    new Promise((_, reject) => {
      timer = setTimeout(
        () =>
          reject(
            new Error(`${what} took longer than ${TURN_TIMEOUT_MS / 1000}s`),
          ),
        TURN_TIMEOUT_MS,
      );
    }),
  ]).finally(() => clearTimeout(timer));
}

const slug = (title) =>
  title
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "");

/** Session ids become names, and paths on this machine the fixture's. */
function sanitize(trace, { chatId, workdir, realWorkdir, home }) {
  const ids = new Map([[chatId, "chat"]]);
  for (const entry of [...trace.live, ...trace.load]) {
    const update = entry.update;
    if (update?.sessionUpdate === "subagent_update" && update.title) {
      ids.set(update.sessionId, slug(update.title));
    }
  }
  let text = JSON.stringify(trace);
  // Claude Code's task output files: <tmp>/claude-<uid>/<cwd>/<session>/tasks/<id>.output
  const tmp = [...new Set(["/tmp", os.tmpdir()])]
    .map((dir) => dir.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"))
    .join("|");
  text = text.replace(
    new RegExp(`(?:${tmp})/claude-\\d+/[^\\s"'\`)\\\\]+`, "g"),
    "/tmp/claude/task-output",
  );
  const replacements = [
    [realWorkdir, WORKDIR],
    [workdir, WORKDIR],
    [
      realWorkdir.replace(/[^A-Za-z0-9]/g, "-"),
      WORKDIR.replace(/[^A-Za-z0-9]/g, "-"),
    ],
    [home, "/home/user"],
    ...ids,
  ];
  for (const [from, to] of replacements) text = text.split(from).join(to);
  return { trace: JSON.parse(text), names: [...new Set(ids.values())] };
}

function problems(trace, names, permissions) {
  const found = [];
  const expected = [...NAMES].sort((a, b) => a.localeCompare(b)).join(", ");
  const got = [...names].sort((a, b) => a.localeCompare(b)).join(", ");
  if (got !== expected) {
    found.push(
      `the sessions are ${got}, not ${expected} (a subagent was titled otherwise)`,
    );
  }
  const marks = trace.live.filter((e) => e.mark).map((e) => e.mark);
  if (marks.join(" ") !== MARKS.join(" ")) {
    found.push(
      `the live marks are ${marks.join(", ")}, not ${MARKS.join(", ")}`,
    );
  }
  if (!trace.load.some((e) => e.mark === "loaded")) {
    found.push("session/load never answered");
  }
  if (permissions.length > 0) {
    found.push(
      `the adapter asked ${permissions.length} permission(s) (${permissions
        .map((p) => p.toolCall?.title ?? "?")
        .join(", ")}); the tests replay a chat that asked none`,
    );
  }
  const updates = trace.live.map((e) => e.update).filter(Boolean);
  const subagent = (id) =>
    updates.filter(
      (u) => u.sessionUpdate === "subagent_update" && u.sessionId === id,
    );
  if (!subagent("sleeper").some((u) => u.state?.stopReason === "cancelled")) {
    found.push("the Sleeper did not end cancelled");
  }
  const nested = trace.live.find(
    (e) =>
      e.update?.sessionUpdate === "subagent_update" &&
      e.update.sessionId === "nested-c",
  );
  if (nested && nested.sessionId !== "reader-b") {
    found.push(`Nested C was started by ${nested.sessionId}, not reader-b`);
  }
  const blob = JSON.stringify(trace);
  const user = os.userInfo().username;
  if (user.length > 2 && blob.includes(user)) {
    found.push(`the recording still names "${user}"; extend sanitize()`);
  }
  return found;
}

// One entry per line, as the fixture has always been laid out.
function dumps(value) {
  if (Array.isArray(value)) return `[${value.map(dumps).join(", ")}]`;
  if (value && typeof value === "object") {
    return `{${Object.entries(value)
      .map(([key, item]) => `${JSON.stringify(key)}: ${dumps(item)}`)
      .join(", ")}}`;
  }
  return JSON.stringify(value);
}

function layout(trace) {
  const list = (entries) =>
    entries.map((entry) => `  ${dumps(entry)}`).join(",\n");
  return `{\n "live": [\n${list(trace.live)}\n ],\n "load": [\n${list(trace.load)}\n ]\n}\n`;
}

async function main() {
  const version = await pinnedVersion();
  const home = os.homedir();
  const agentDir = path.join(home, ".local/share/linuxio/agents/claude");
  const profile = path.join(home, ".local/share/linuxio/profiles/claude");
  const manifest = path.join(agentDir, "node_modules", PACKAGE, "package.json");

  let installed;
  try {
    installed = JSON.parse(await fs.readFile(manifest, "utf8")).version;
  } catch {
    installed = null;
  }
  if (installed !== version) {
    fail(
      `${PACKAGE} ${installed ?? "is not installed"}${installed ? " is installed" : ""}, the pin is ${version}.\n` +
        `   Install the pin from the Capability Manager of a LinuxIO built with it, or:\n` +
        `   rm -rf ${agentDir}.new && npm install --prefix ${agentDir}.new --no-audit --no-fund --no-progress --omit=optional ${PACKAGE}@${version} && rm -rf ${agentDir} && mv ${agentDir}.new ${agentDir}`,
    );
  }
  try {
    await fs.access(profile);
  } catch {
    fail(
      `no Claude profile at ${profile}: sign in on the Assistant page first`,
    );
  }
  const cli = await onPath("claude");
  if (!cli) fail("claude is not on PATH");
  const bin = path.join(agentDir, "node_modules/.bin/claude-agent-acp");

  await fs.mkdir(cacheDir, { recursive: true });
  const logPath = path.join(cacheDir, "adapter.log");
  const logFile = createWriteStream(logPath);
  const workdir = await fs.mkdtemp(
    path.join(os.tmpdir(), "linuxio-subagent-trace-"),
  );
  const realWorkdir = await fs.realpath(workdir);
  for (const [name, text] of Object.entries(FILES)) {
    await fs.writeFile(path.join(workdir, name), text);
  }

  console.log(`🎙️  Recording a chat with ${PACKAGE} ${version} in ${workdir}`);
  log(`adapter stderr: ${path.relative(repo, logPath)}`);
  const env = agentEnv(profile, cli, realWorkdir);
  const trace = { live: [], load: [] };
  let chatId = null;
  let permissions = [];

  // ---- live chat -----------------------------------------------------------
  let sleeperId = null;
  let stopTimer = null;
  let stopped = false;
  let live;
  const stopSleeper = () => {
    if (stopped || !sleeperId) return;
    stopped = true;
    clearTimeout(stopTimer);
    log("stopping the Sleeper");
    live.connection.agent
      .notify(acp.methods.agent.session.cancel, { sessionId: sleeperId })
      .catch((error) => log(`stopping the Sleeper failed: ${error}`));
  };
  live = startAdapter({
    bin,
    env,
    cwd: realWorkdir,
    logFile,
    chatId: () => chatId,
    onUpdate: ({ sessionId, update }) => {
      if (update.sessionUpdate === "subagent_update" && update.title) {
        log(`subagent "${update.title}" started`);
        if (update.title === "Sleeper" && !sleeperId) {
          sleeperId = update.sessionId;
          stopTimer = setTimeout(stopSleeper, STOP_FALLBACK_MS);
        }
      }
      if (
        sessionId === sleeperId &&
        update.sessionUpdate === "tool_call" &&
        !stopped
      ) {
        clearTimeout(stopTimer);
        stopTimer = setTimeout(stopSleeper, STOP_AFTER_CALL_MS);
      }
    },
  });
  try {
    await withTimeout(live.initialize(), "initialize");
    // The test's times count from the chat's creation; what comes before the
    // first prompt (commands, modes) is not part of the recording.
    const scratch = [];
    live.record(scratch);
    const session = await live.connection.agent.request(
      acp.methods.agent.session.new,
      {
        cwd: realWorkdir,
        mcpServers: [],
      },
    );
    chatId = session.sessionId;
    // The recording ran in Auto and asked nothing.
    await live.connection.agent.request(acp.methods.agent.session.setMode, {
      sessionId: chatId,
      modeId: "auto",
    });
    for (const [index, text] of PROMPTS.entries()) {
      log(`prompt ${index + 1} of ${PROMPTS.length}`);
      const answer = await withTimeout(
        live.connection.agent.request(acp.methods.agent.session.prompt, {
          sessionId: chatId,
          prompt: [{ type: "text", text }],
        }),
        `prompt ${index + 1}`,
      );
      log(`answered: ${answer.stopReason}`);
    }
    const first = scratch.findIndex((entry) => entry.mark === "prompt");
    trace.live = scratch.slice(first);
    permissions = [...live.state.permissions];
  } finally {
    clearTimeout(stopTimer);
    live.state.entries = null;
    await live.stop();
  }

  // ---- the same chat loaded again ------------------------------------------
  log("loading the chat again");
  const load = startAdapter({
    bin,
    env,
    cwd: realWorkdir,
    logFile,
    chatId: () => chatId,
    onUpdate: () => {},
  });
  try {
    await withTimeout(load.initialize(), "initialize");
    load.record(trace.load);
    await withTimeout(
      load.connection.agent.request(acp.methods.agent.session.load, {
        sessionId: chatId,
        cwd: realWorkdir,
        mcpServers: [],
      }),
      "session/load",
    );
    await new Promise((resolve) => setTimeout(resolve, LOAD_SETTLE_MS));
    permissions.push(...load.state.permissions);
  } finally {
    load.state.entries = null;
    await load.stop();
    logFile.end();
    await fs.rm(workdir, { recursive: true, force: true });
  }

  // ---- write ---------------------------------------------------------------
  const { trace: clean, names } = sanitize(trace, {
    chatId,
    workdir,
    realWorkdir,
    home,
  });
  const found = problems(clean, names, permissions);
  if (found.length > 0) {
    const rejected = path.join(cacheDir, "trace.json");
    await fs.writeFile(rejected, layout(clean));
    console.error(
      "\n❌ The chat did not go as the tests replay it; the fixture is unchanged:",
    );
    for (const problem of found) console.error(`   - ${problem}`);
    console.error(
      `   The recording is in ${path.relative(repo, rejected)}. The model varies: run it again.`,
    );
    process.exit(1);
  }

  await fs.writeFile(fixture, layout(clean));
  const today = new Date().toLocaleDateString("sv-SE");
  const test = await fs.readFile(testFile, "utf8");
  const header =
    /claude-agent-acp \d+\.\d+\.\d+\), recorded\n \* on \d{4}-\d{2}-\d{2}/;
  if (header.test(test)) {
    await fs.writeFile(
      testFile,
      test.replace(
        header,
        `claude-agent-acp ${version}), recorded\n * on ${today}`,
      ),
    );
  } else {
    console.warn(
      `⚠️  ${path.relative(repo, testFile)} has no version header to move; update it by hand`,
    );
  }
  console.log(
    `\n✅ Recorded ${clean.live.length} live and ${clean.load.length} load entries`,
  );
  log(`wrote ${path.relative(repo, fixture)}`);
  log(
    `moved the header of ${path.relative(repo, testFile)} to ${version}, ${today}`,
  );
  log("the chat stays in the Claude profile's history; its folder is gone");
  log("next: make test-frontend-quiet, then the assistant browser spec");
}

main().catch((error) => {
  console.error(`❌ ${error instanceof Error ? error.message : String(error)}`);
  process.exit(1);
});
