// Status bar indicator for the Vite dev server that `make dev` starts.
// State comes from frontend/.vite-dev.pid (written by scripts/dev.sh), so it is
// correct however Vite was started. Start and stop run the existing tasks.
const fs = require("node:fs");
const path = require("node:path");

const vscode = require("vscode");

const START_TASK = "make dev";
const STOP_TASK = "make dev-stop";
const POLL_MS = 2000;

function viteRunning(pidFile) {
  try {
    const pid = Number.parseInt(fs.readFileSync(pidFile, "utf8"), 10);
    if (!Number.isInteger(pid) || pid <= 0) return false;
    process.kill(pid, 0);
    return true;
  } catch (error) {
    // EPERM means the process exists but belongs to someone else.
    return error.code === "EPERM";
  }
}

function runTask(label) {
  return vscode.commands.executeCommand("workbench.action.tasks.runTask", label);
}

function showLogs() {
  const terminal = vscode.window.terminals.find((t) => t.name.includes(START_TASK));
  if (terminal) {
    terminal.show();
  } else {
    vscode.window.showInformationMessage(
      "No 'make dev' terminal in this window. Start the dev server from here to see its logs.",
    );
  }
}

const COMMANDS = {
  "linuxioDevStatus.start": () => runTask(START_TASK),
  "linuxioDevStatus.stop": () => runTask(STOP_TASK),
  "linuxioDevStatus.logs": showLogs,
};

function activate(context) {
  const root = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
  // Installed for every workspace; only act inside a LinuxIO checkout.
  if (!root || !fs.existsSync(path.join(root, "scripts", "dev.sh"))) return;

  const pidFile = path.join(root, "frontend", ".vite-dev.pid");
  const item = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 0);
  let running;

  // The hover carries the actions: command links need isTrusted for those commands.
  const tooltip = () => {
    const md = new vscode.MarkdownString(undefined, true);
    md.isTrusted = { enabledCommands: Object.keys(COMMANDS) };
    md.appendMarkdown(`**Vite dev server ${running ? "running" : "stopped"}**\n\n`);
    md.appendMarkdown(
      running
        ? "[$(debug-stop) Stop dev server](command:linuxioDevStatus.stop)"
        : "[$(play) Start dev server](command:linuxioDevStatus.start)",
    );
    md.appendMarkdown("\n\n[$(output) Show logs](command:linuxioDevStatus.logs)");
    return md;
  };

  const refresh = () => {
    const now = viteRunning(pidFile);
    if (now === running) return;
    running = now;
    item.text = "$(circle-filled) dev";
    item.color = new vscode.ThemeColor(running ? "testing.iconPassed" : "disabledForeground");
    item.tooltip = tooltip();
    item.accessibilityInformation = {
      label: running ? "Dev server running" : "Dev server stopped",
    };
  };

  refresh();
  item.show();
  const timer = setInterval(refresh, POLL_MS);
  context.subscriptions.push(
    item,
    { dispose: () => clearInterval(timer) },
    ...Object.entries(COMMANDS).map(([id, run]) => vscode.commands.registerCommand(id, run)),
  );
}

module.exports = { activate };
