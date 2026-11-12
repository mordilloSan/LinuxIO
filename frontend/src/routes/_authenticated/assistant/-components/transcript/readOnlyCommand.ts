/**
 * Whether a shell command only reads. This decides how a tool call is shown
 * (a line in the log, or a card for something that acted on the host) and
 * nothing else: permissions stay with the agent. It errs towards "acts":
 * anything it does not recognise is not read-only.
 */

// Commands that only read whatever their arguments are.
const READERS = new Set([
  "[",
  "basename",
  "blkid",
  "cat",
  "cd",
  "cmp",
  "cut",
  "date",
  "df",
  "diff",
  "dig",
  "dirname",
  "du",
  "echo",
  "egrep",
  "env",
  "file",
  "findmnt",
  "free",
  "getent",
  "grep",
  "head",
  "host",
  "hostname",
  "id",
  "jq",
  "less",
  "ls",
  "lsblk",
  "lscpu",
  "lsmod",
  "lspci",
  "lsusb",
  "md5sum",
  "nproc",
  "nslookup",
  "pgrep",
  "ping",
  "printenv",
  "printf",
  "ps",
  "pwd",
  "readlink",
  "realpath",
  "rg",
  "sensors",
  "sha256sum",
  "sort",
  "ss",
  "stat",
  "tail",
  "test",
  "tr",
  "tree",
  "true",
  "uname",
  "uniq",
  "uptime",
  "wc",
  "which",
  "whoami",
]);

// Commands that read only with one of these subcommands.
const READ_SUBCOMMANDS: Record<string, string[]> = {
  btrfs: [
    "balance status",
    "device stats",
    "device usage",
    "filesystem df",
    "filesystem show",
    "filesystem usage",
    "qgroup show",
    "scrub status",
    "subvolume list",
    "subvolume show",
  ],
  docker: [
    "compose config",
    "compose logs",
    "compose ls",
    "compose ps",
    "container inspect",
    "container ls",
    "history",
    "image inspect",
    "image ls",
    "images",
    "info",
    "inspect",
    "logs",
    "network inspect",
    "network ls",
    "port",
    "ps",
    "stats",
    "top",
    "version",
    "volume inspect",
    "volume ls",
  ],
  git: [
    "blame",
    "describe",
    "diff",
    "log",
    "ls-files",
    "rev-parse",
    "show",
    "status",
  ],
  hostnamectl: ["", "status"],
  loginctl: ["list-sessions", "list-users", "show-session", "show-user"],
  systemctl: [
    "cat",
    "is-active",
    "is-enabled",
    "is-failed",
    "list-dependencies",
    "list-sockets",
    "list-timers",
    "list-unit-files",
    "list-units",
    "show",
    "status",
  ],
  timedatectl: ["", "show", "status"],
};

// Commands that read unless one of these appears among their arguments.
const READ_UNLESS: Record<string, RegExp> = {
  dmesg: /^(-c|-C|--clear|--read-clear)$/,
  find: /^-(delete|exec|execdir|ok|okdir|fprint.*|fls)$/,
  ip: /^(add|del|delete|set|flush|replace|change|append)$/,
  journalctl: /^--(vacuum-.*|rotate|flush|sync|relinquish-var)$/,
  sed: /^(-i.*|--in-place.*)$/,
};

function segmentReads(segment: string): boolean {
  const words = segment.trim().split(/\s+/).filter(Boolean);
  // Leading VAR=value assignments and sudo do not change what the command is.
  while (words.length > 0 && (/^\w+=/.test(words[0]) || words[0] === "sudo")) {
    words.shift();
  }
  if (words.length === 0) return true;
  const [name, ...args] = words;
  if (READERS.has(name)) return true;
  const unless = READ_UNLESS[name];
  if (unless) return !args.some((arg) => unless.test(arg));
  const subcommands = READ_SUBCOMMANDS[name];
  if (!subcommands) return false;
  const plain = args.filter((arg) => !arg.startsWith("-"));
  return subcommands.some((sub) =>
    sub === ""
      ? plain.length === 0
      : sub.split(" ").every((word, index) => plain[index] === word),
  );
}

export function isReadOnlyCommand(command: string): boolean {
  // Sending stderr elsewhere is not a write to a file anyone keeps.
  const stripped = command.replace(/\d*>\s*(\/dev\/null|&\d)/g, "");
  // Redirections write; substitutions hide a command this cannot see.
  if (/[<>`]|\$\(/.test(stripped)) return false;
  return stripped.split(/\|\||&&|[|;\n&]/).every(segmentReads);
}
