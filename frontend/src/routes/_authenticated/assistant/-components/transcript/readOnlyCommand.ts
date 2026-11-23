/**
 * Whether a shell command only reads. This decides how a tool call is shown
 * (a line in the log, or a card for something that acted on the host) and
 * nothing else: permissions stay with the agent. It errs towards "acts":
 * anything it does not recognise is not read-only.
 */

// Commands that only read, unless ACTS_WHEN finds an argument that writes.
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
  "dmesg",
  "dig",
  "dirname",
  "du",
  "echo",
  "egrep",
  "env",
  "file",
  "find",
  "findmnt",
  "free",
  "getent",
  "grep",
  "head",
  "host",
  "hostname",
  "id",
  "ip",
  "jq",
  "journalctl",
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
  "sed",
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

const anyArg = (pattern: RegExp) => (args: string[]) =>
  args.some((arg) => pattern.test(arg));

// date sets the clock with -s or with a date given as a plain argument.
const DATE_VALUE = /^(-[uR]*[dfr]|--date|--file|--reference)$/;

// The arguments that make a reader, or a read-only subcommand, act.
const ACTS_WHEN: Record<string, (args: string[]) => boolean> = {
  date: (args) =>
    args.some(
      (arg, index) =>
        /^(-[uR]*s|--s)/.test(arg) ||
        (!arg.startsWith("-") &&
          !arg.startsWith("+") &&
          !DATE_VALUE.test(args[index - 1] ?? "")),
    ),
  dmesg: anyArg(/^(-c|-C|--clear|--read-clear)$/),
  find: anyArg(/^-(delete|exec|execdir|ok|okdir|fprint.*|fls)$/),
  git: anyArg(/^--output(=|$)/),
  // A name, a file or --boot sets the hostname.
  hostname: anyArg(/^([^-]|-[a-zA-Z]*[Fb]|--fi|--b)/),
  ip: anyArg(/^(add|del|delete|set|flush|replace|change|append)$/),
  journalctl: anyArg(/^--(vacuum-.*|rotate|flush|sync|relinquish-var)$/),
  rg: anyArg(/^--pre(=|$)/),
  sed: anyArg(/^(-i.*|--in-place.*)$/),
  sort: anyArg(/^(-[a-zA-Z]*o|--o|--com)/),
  ss: anyArg(/^(-[a-zA-Z]*K|--k)/),
  tree: anyArg(/^-[a-zA-Z]*o/),
  // uniq INPUT OUTPUT writes OUTPUT.
  uniq: (args) => {
    let files = 0;
    for (let index = 0; index < args.length; index++) {
      if (/^-[a-zA-Z]*[fsw]$/.test(args[index])) index++;
      else if (args[index] === "-" || !args[index].startsWith("-")) files++;
    }
    return files > 1;
  },
};

// env runs the command after its options and assignments, and alone lists the
// environment. The text of -S is split into more of env's own arguments.
function envReads(args: string[]): boolean {
  for (let index = 0; index < args.length; index++) {
    const arg = args[index];
    if (arg === "-S" || arg === "--split-string") {
      return splitReads(args[index + 1] ?? "", args.slice(index + 2));
    }
    const split = /^(?:-S|--split-string=)(.*)$/s.exec(arg);
    if (split) return splitReads(split[1], args.slice(index + 1));
    if (arg === "--") return segmentReads(args.slice(index + 1));
    if (/^(-u|--unset|-C|--chdir)$/.test(arg)) {
      index++;
      continue;
    }
    if (
      /^\w+=/.test(arg) ||
      /^(-[i0v]+|-|-u.+|-C.+|--ignore-environment|--null|--debug|--unset=.*|--chdir=.*)$/.test(
        arg,
      )
    ) {
      continue;
    }
    if (arg.startsWith("-")) return false;
    return segmentReads(args.slice(index));
  }
  return true;
}

function splitReads(text: string, after: string[]): boolean {
  const segments = parse(text);
  return (
    segments !== null &&
    segments.length === 1 &&
    envReads([...segments[0], ...after])
  );
}

function segmentReads(words: string[]): boolean {
  let rest = words;
  // Leading VAR=value assignments and sudo do not change what the command is.
  while (rest.length > 0 && (/^\w+=/.test(rest[0]) || rest[0] === "sudo")) {
    rest = rest.slice(1);
  }
  if (rest.length === 0) return true;
  const [name, ...args] = rest;
  if (name === "env") return envReads(args);
  // Object.hasOwn, so a command named like a prototype member is unknown.
  if (Object.hasOwn(ACTS_WHEN, name) && ACTS_WHEN[name](args)) return false;
  if (READERS.has(name)) return true;
  if (!Object.hasOwn(READ_SUBCOMMANDS, name)) return false;
  const subcommands = READ_SUBCOMMANDS[name];
  const plain = args.filter((arg) => !arg.startsWith("-"));
  return subcommands.some((sub) =>
    sub === ""
      ? plain.length === 0
      : sub.split(" ").every((word, index) => plain[index] === word),
  );
}

// Output thrown away or sent to another descriptor: 2>/dev/null, >&2, &>/dev/null.
const DISCARD = /^&?>>?[ \t]*(\/dev\/null|&\d)(?=[\s|;&]|$)/;

/**
 * Splits a command line, in one pass as the shell reads quotes, into the
 * words of each command between |, ||, |&, &, &&, ; and newlines. Returns
 * null where it cannot tell what runs or where output goes: a substitution,
 * an unclosed quote, input from a file or here-document, output to a file.
 */
function parse(command: string): string[][] | null {
  const segments: string[][] = [];
  let words: string[] = [];
  let word = "";
  let inWord = false;
  const endWord = () => {
    if (inWord) words.push(word);
    word = "";
    inWord = false;
  };
  for (let index = 0; index < command.length; index++) {
    const char = command[index];
    const next = command[index + 1];
    if (char === "`" || (char === "$" && next === "(")) return null;
    if (char === "'") {
      const end = command.indexOf("'", index + 1);
      if (end < 0) return null;
      word += command.slice(index + 1, end);
      inWord = true;
      index = end;
    } else if (char === '"') {
      inWord = true;
      for (index++; command[index] !== '"'; index++) {
        if (index >= command.length) return null;
        const inner = command[index];
        if (inner === "`" || (inner === "$" && command[index + 1] === "(")) {
          return null;
        }
        if (inner === "\\" && index + 1 < command.length) index++;
        word += command[index];
      }
    } else if (char === "\\") {
      index++;
      if (next === undefined || next === "\n") continue;
      word += next;
      inWord = true;
    } else if (char === "<") {
      return null;
    } else if (char === ">" || (char === "&" && next === ">")) {
      const discard = DISCARD.exec(command.slice(index));
      if (!discard) return null;
      // The digits written against the > are the descriptor, not a word.
      if (/^\d+$/.test(word)) inWord = false;
      endWord();
      index += discard[0].length - 1;
    } else if ("|;&\n".includes(char)) {
      endWord();
      segments.push(words);
      words = [];
      if ((char === "|" || char === "&") && next === char) index++;
      else if (char === "|" && next === "&") index++;
    } else if (/\s/.test(char)) {
      endWord();
    } else {
      word += char;
      inWord = true;
    }
  }
  endWord();
  segments.push(words);
  return segments;
}

export function isReadOnlyCommand(command: string): boolean {
  const segments = parse(command);
  return segments !== null && segments.every((words) => segmentReads(words));
}
