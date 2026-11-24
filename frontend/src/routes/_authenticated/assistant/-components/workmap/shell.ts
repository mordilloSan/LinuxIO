/**
 * Where a shell command line belongs on the activity map: Network when any
 * command in it reaches out, Files when every command only handles files, and
 * Shell otherwise. It is read from the line alone; nothing is run.
 *
 * Which commands, wrappers and subcommands count, and how a line is read past
 * loops, `bash -c`, `eval`, `find -exec` and here-documents, follow lot-map by
 * Aleksei Volodin (https://github.com/alexeyw/lot-map), used under the MIT
 * licence recorded in docs/THIRD_PARTY_NOTICES.md. The package managers of
 * the distributions other than Debian's, and podman, are LinuxIO's.
 */

export type ShellStation = "network" | "files" | "shell";

const set = (list: string) => new Set(list.split(", "));

// Commands that work on files and nothing else.
const FILE_WORDS = set(
  "awk, basename, bat, bzip2, cat, chgrp, chmod, chown, cmp, column, comm, cp, cut, diff, dirname, du, fd, file, find, grep, gunzip, gzip, head, hexdump, jq, less, ln, ls, md5sum, mkdir, mktemp, more, mv, nl, od, paste, readlink, realpath, rg, rm, rmdir, sed, sha1sum, sha256sum, sort, stat, tail, tar, tee, touch, tr, tree, truncate, uniq, unzip, wc, xxd, xz, zcat, zip, zstd",
);
// Commands that say nothing either way, the shell's words that only set
// something among them.
const NEUTRAL_WORDS = set(
  ":, ., [, [[, cd, declare, echo, export, false, local, popd, printf, pushd, pwd, read, readonly, set, shopt, sleep, source, test, true, typeset, umask, unset, wait, which",
);
// Commands that reach the network whatever they are asked.
const NETWORK_WORDS = set(
  "aws, az, curl, dig, ftp, gcloud, gh, host, http, https, kubectl, mtr, nc, ncat, nmap, nslookup, ping, rsync, scp, sftp, ssh, ssh-copy-id, ssh-keyscan, telnet, tracepath, traceroute, wget, whois",
);
// Commands that reach the network with some of their subcommands only. A
// subcommand may be two words.
const DNF = set(
  "install, reinstall, update, upgrade, downgrade, distro-sync, makecache, check-update, download, groupinstall, group install",
);
const COMPOSE = set("pull, push");
const CONTAINERS = set("pull, push, login, image pull, image push");
const NETWORK_SUBCOMMANDS = new Map<string, ReadonlySet<string>>([
  [
    "git",
    set("push, pull, fetch, clone, ls-remote, submodule update, remote update"),
  ],
  ["svn", set("checkout, co, update, up, commit, ci, export")],
  ["hg", set("clone, pull, push, incoming, outgoing")],
  ["npm", set("install, i, ci, add, update, publish, view, audit")],
  ["pnpm", set("install, i, add, update, publish, dlx")],
  ["yarn", set("install, add, upgrade, publish, dlx")],
  ["bun", set("install, i, add, update, publish")],
  ["deno", set("install, add, cache")],
  ["pip", set("install, download")],
  ["pipx", set("install, upgrade, upgrade-all, run")],
  ["uv", set("add, sync, lock, publish, tool install")],
  ["poetry", set("install, add, update, lock, publish")],
  ["cargo", set("install, fetch, publish, update")],
  ["go", set("get, install, mod download")],
  ["brew", set("install, update, upgrade, fetch")],
  ["docker", CONTAINERS],
  ["docker compose", COMPOSE],
  ["docker-compose", COMPOSE],
  ["podman", CONTAINERS],
  ["podman compose", COMPOSE],
  ["podman-compose", COMPOSE],
  ["terraform", set("init, plan, apply, destroy, import, refresh")],
  ["helm", set("install, upgrade, pull, push, repo, search, dependency")],
  ["openssl", set("s_client")],
  ["apt", set("install, update, upgrade, full-upgrade, dist-upgrade")],
  ["apt-get", set("install, update, upgrade, dist-upgrade")],
  ["dnf", DNF],
  ["yum", DNF],
  [
    "zypper",
    set(
      "install, in, update, up, dist-upgrade, dup, patch, refresh, ref, source-install, si",
    ),
  ],
  ["snap", set("install, refresh, download, find")],
  ["flatpak", set("install, update, remote-add, remote-ls")],
  ["apk", set("add, update, upgrade, fetch")],
]);
// pacman and its AUR helpers sync with `-S`; with s, i, l, g, c or p beside it
// they search, show, list or clean what is already on the host.
const PACMAN = set("pacman, yay, paru");
const syncs = (word: string) =>
  /^-S[a-z]*$/.test(word) && !/[cgilps]/.test(word.slice(2));
// Programs that Python runs as a module: `python -m pip install` is pip's.
const PY_MODULES = set("pip, uv, pipx, poetry");

// Flags before the subcommand that take a word after them.
const CONTAINER_TAKES = set(
  "-c, --context, -H, --host, --config, -l, --log-level, -f, --file, -p, --project-name, --profile, --env-file, --project-directory, --connection, --url, --root, --runroot, --storage-driver",
);
const DNF_TAKES = set(
  "-c, --config, --repo, --repoid, --enablerepo, --disablerepo, --releasever, --installroot, --setopt",
);
const TAKES = new Map<string, ReadonlySet<string>>([
  [
    "git",
    set(
      "-C, -c, --git-dir, --work-tree, --namespace, --exec-path, --config-env",
    ),
  ],
  ["docker", CONTAINER_TAKES],
  ["docker-compose", CONTAINER_TAKES],
  ["podman", CONTAINER_TAKES],
  ["podman-compose", CONTAINER_TAKES],
  ["apt", set("-o, -c, -t")],
  ["apt-get", set("-o, -c, -t")],
  ["dnf", DNF_TAKES],
  ["yum", DNF_TAKES],
  ["zypper", set("-R, --root, --reposd-path, --cache-dir, --config")],
  ["npm", set("--prefix, -w, --workspace")],
  ["pnpm", set("-C, --dir, -F, --filter")],
  ["yarn", set("--cwd")],
  ["pip", set("--cache-dir, --log, --proxy, --python")],
]);

// Commands that run another command, which is the one that counts. `takes`
// are their flags that take a word after them, `skip` how many words of their
// own follow the flags, and with a flag in `asks` they run nothing.
const WRAPPERS = new Map<
  string,
  { takes?: ReadonlySet<string>; skip?: number; asks?: ReadonlySet<string> }
>([
  ["chroot", { skip: 1 }],
  ["command", { asks: set("-v, -V") }],
  ["doas", { takes: set("-u, -C") }],
  ["env", { takes: set("-u, --unset, -C, --chdir, -a, --argv0, -f, --file") }],
  ["exec", { takes: set("-a") }],
  ["ionice", { takes: set("-c, --class, -n, --classdata") }],
  ["nice", { takes: set("-n, --adjustment") }],
  ["nohup", {}],
  ["stdbuf", { takes: set("-i, --input, -o, --output, -e, --error") }],
  [
    "sudo",
    {
      takes: set(
        "-u, -g, -p, -h, -C, -D, -R, -T, -U, -r, -t, --user, --group, --host, --chdir, --chroot, --prompt, --other-user, --role, --type, --close-from, --command-timeout",
      ),
    },
  ],
  ["time", {}],
  ["timeout", { takes: set("-s, --signal, -k, --kill-after"), skip: 1 }],
  [
    "xargs",
    {
      takes: set(
        "-n, -I, -P, -L, -d, -E, -s, -a, -J, -R, --max-args, --max-procs, --delimiter, --max-chars, --arg-file, --process-slot-var",
      ),
    },
  ],
]);
// watch hands its words to `sh -c`, as one line.
const WATCH_TAKES = set("-n, --interval");
// runuser runs what follows its user, or the line after `-c`, as su does.
const RUNUSER_TAKES = set("-u, -g, -G");

// Shells that run a line handed to them with `-c`.
const SHELLS = set("bash, sh, zsh, dash, ksh");
// A shell option that takes the next word: `-o pipefail`, `-euo pipefail`.
const TAKES_OPTION = /^[-+][a-zA-Z]*[oO]$/;
// A here-document a shell reads is a script, not text: `bash <<EOF`,
// `bash -euo pipefail <<EOF`, `bash -s 2>&1 <<EOF`, and from the `|` after
// it, `cat <<EOF | sudo -u app bash`. Where the shell's output goes says
// nothing of what it runs.
// The word an option takes never starts with `-` and holds no `>`, so a run
// of options and redirections reads one way only; read both ways, a long run
// takes exponential time.
const SHELL_BEFORE =
  /(?:^|[\s;&|(/`])(?:bash|sh|zsh|dash|ksh)(?:\s+(?:[-+][abd-zA-Z]*[oO]\s+\w[\w-]*|[-+][abd-zA-Z]+|(?:\d*(?:>>?|>[|&]|<&)|&>>?)\s*[^\s<>;&|()]+))*\s*$/;
const PIPED_TO_SHELL =
  /\|\s*(?:sudo(?:\s+-\S+(?:\s+[\w.][\w.-]*)?)*\s+)?(?:[^\s|]*\/)?(?:bash|sh|zsh|dash|ksh)\b/y;
// What ends a word that stands outside quotes.
const ENDS_A_WORD = /[\s;&|<>()`]/;
// Words of a construct that stand before a command: `do curl x` is curl.
const BEFORE_A_COMMAND = set("do, then, else, elif, if, while, until, !, {");
// Words that open or close a construct and run nothing: `for u in a b`, `done`.
const NO_COMMAND = set("for, select, case, done, fi, esac, }");
// `a=1`, `a+=1`, and an array's element, `a[0]=1`.
const ASSIGNMENT = /^[A-Za-z_]\w*(?:\[.*\])?\+?=/;
// The word a list stands behind: the name and its `=` or `+=`, no more.
const BEFORE_A_LIST = /^[A-Za-z_]\w*(?:\[.*\])?\+?=$/;
// A redirection, as it may stand before a command: `2>/dev/null`, `>out`,
// `&>log`, `<<<"text"`. Its target is in the same word or the next one.
const REDIRECTION = /^(?:\d+|&)?(?:>>|>\||<<<|<<-|<<|<>|>&|<&|>|<)/;
// What ends the command `find -exec` runs.
const FIND_ENDS = new Set([";", "\\;", "';'", '";"', "+"]);
// How much of a line is read: a longer one is a script written inline, and
// what counts stands at its start.
const READ = 8000;
// How many commands run inside others one line follows: in `$(…)`, behind
// `find -exec`, in the line handed to `bash -c`. A line that holds more is
// read no further and stays at Shell, so that no nesting costs more than a
// few hundred readings of the line, nor runs the stack out.
const FOLLOWED = 256;

/** What a line may still follow of `FOLLOWED`; below zero, it held more. */
interface Budget {
  left: number;
}

/** Whether one more command run inside another is followed. */
const follows = (budget: Budget) => --budget.left >= 0;

/** A command's own name: no quotes and no folder before it. */
const name = (word = "") => word.replace(/["']/g, "").split("/").pop() ?? word;

/** A word without the one pair of quotes around it. */
function unquoted(word: string) {
  const quote = word[0];
  if (quote !== '"' && quote !== "'") return word;
  return word.length > 1 && word.endsWith(quote)
    ? word.slice(1, -1)
    : word.slice(1);
}

/** The words after the flags; a flag in `takes` has a word of its own. */
function pastFlags(words: string[], takes?: ReadonlySet<string>) {
  let i = 0;
  while (i < words.length && words[i].startsWith("-")) {
    i += takes?.has(words[i]) ? 2 : 1;
  }
  return words.slice(i);
}

interface HereDocument {
  word: string;
  tabs: boolean;
  /** A shell reads it: its lines are commands. */
  runs: boolean;
  /** Its word was bare: the `$(…)` and backticks in its text run. */
  expands: boolean;
  /** What closes the `$(…)` or backticks it was opened in, if it was. */
  inside: "" | ")" | "`";
}

/**
 * The mark after `<<`, from `start`: `-` to strip tabs, then a word as the
 * shell reads one, with its quotes and backslashes taken off, so `E"O"F` is
 * EOF and `"END MSG"` holds a space. Only a word that had none is bare.
 * Nothing for no word at all, a quote that does not close on the line or a
 * backslash inside double quotes, and nothing for a word that starts with
 * anything but a letter, `_`, a quote or a backslash: `<<2`, `<<$n` and
 * `<<(x)` are far more often a shift, in arithmetic this reader does not
 * follow (over lines, in `${…}`, in an index), and a mark seen where there
 * is none hides every line after it.
 */
function markAt(line: string, start: number) {
  let i = start;
  const tabs = line[i] === "-";
  if (tabs) i++;
  while (line[i] === " " || line[i] === "\t") i++;
  const from = i;
  if (!/[A-Za-z_'"\\]/.test(line[i] ?? "")) return undefined;
  let word = "";
  let bare = true;
  for (; i < line.length; i++) {
    const c = line[i];
    if (c === "'" || c === '"') {
      const end = line.indexOf(c, i + 1);
      if (end < 0) return undefined;
      if (c === '"' && line.slice(i, end).includes("\\")) return undefined;
      word += line.slice(i + 1, end);
      bare = false;
      i = end;
    } else if (c === "\\") {
      word += line[++i] ?? "";
      bare = false;
    } else if (ENDS_A_WORD.test(c)) {
      break;
    } else {
      word += c;
    }
  }
  return i > from ? { word, tabs, bare, end: i } : undefined;
}

/**
 * What stands in backticks as the shell inside them reads it: without the
 * backslash before a backtick, a `$` or a backslash, which is how backticks
 * stand inside backticks.
 */
const unticked = (text: string) => text.replace(/\\([\\`$])/g, "$1");

/**
 * What `$(…)` or backticks hold, from the bracket or backtick at `open` to
 * `end`.
 */
function held(line: string, open: number, end: number) {
  const text = line.slice(open + 1, end);
  return line[open] === "`" ? unticked(text) : text;
}

/**
 * Where on a line of a here-document's text the `$(…)` or backticks it was
 * opened in end it, or -1: backticks at the first backtick, wherever it
 * stands, and `$(…)` at a `)` after the mark at the start of the line.
 */
function closedAt(open: HereDocument, text: string) {
  if (open.inside === "`") {
    for (let i = 0; i < text.length; i++) {
      if (text[i] === "\\") i++;
      else if (text[i] === "`") return i;
    }
    return -1;
  }
  if (open.inside !== ")" || !text.startsWith(open.word)) return -1;
  let i = open.word.length;
  while (text[i] === " " || text[i] === "\t") i++;
  return text[i] === ")" ? i : -1;
}

/** What the `$(…)` and backticks of a line of text run, one per line. */
function substitutionsIn(line: string) {
  const found: string[] = [];
  for (let i = 0; i < line.length; i++) {
    if (line[i] === "\\") {
      i++;
    } else if (line[i] === "`" || line.startsWith("$(", i)) {
      const open = line[i] === "`" ? i : i + 1;
      const end = closing(line, open);
      if (line[i] === "`" || line[open + 1] !== "(") {
        found.push(held(line, open, end));
      }
      i = end;
    }
  }
  return found.join("\n");
}

/**
 * The here-documents a line opens, read past quotes, `$(…)`, backticks and
 * comments. `frames` holds those open at the end of the line before and
 * is left as they are at the end of this one, so a string over several lines
 * opens nothing. A frame is `ticked` when it is backticks (`tick`) or stands
 * in them: the next backtick closes those, whatever is open inside. `<<<` is a here-string, and `((a << b))` and `$[a << b]` are
 * arithmetic. The line
 * is read once, however many marks it holds: a mark is none of a shell's
 * options, so the text that may name the shell before a mark starts where the
 * mark before it ends, and each `|` after a mark is looked at once.
 */
function opened(
  line: string,
  frames: {
    quote: string;
    brackets: number;
    tick?: boolean;
    ticked?: boolean;
  }[],
): HereDocument[] {
  const found: HereDocument[] = [];
  // Where the last `((` and `))` stand: after an open `((`, `<<` shifts.
  let shifts = -1;
  let closes = -1;
  // How many `[` stand open from a `$[` on.
  let squares = 0;
  // Where the last mark ends.
  let from = 0;
  // The first `|` after the last mark, and whether a shell reads from it.
  let pipe = -1;
  let piped = false;
  for (let i = 0; i < line.length; i++) {
    const c = line[i];
    if (c === "(" && line[i - 1] === "(") shifts = i - 1;
    else if (c === ")" && line[i - 1] === ")") closes = i - 1;
    const top = frames[frames.length - 1];
    if (top.quote === "'") {
      if (c === "'") top.quote = "";
    } else if (c === "\\") {
      i++;
    } else if (c === "$" && line[i + 1] === "(") {
      // Inside `$(…)` quotes start over, also within double quotes.
      frames.push({ quote: "", brackets: 0, ticked: top.ticked });
      i++;
    } else if (c === "`") {
      // Backticks do the same, and the next one closes them, and with them
      // every here-document opened in them: none has any text.
      if (!top.ticked) {
        frames.push({ quote: "", brackets: 0, tick: true, ticked: true });
      } else {
        while (frames.length > 1 && !frames.pop()?.tick) {
          // A `$(…)` still open in the backticks closes with them.
        }
        while (found[found.length - 1]?.inside === "`") found.pop();
      }
    } else if (top.quote === '"') {
      if (c === '"') top.quote = "";
    } else if (c === "'" || c === '"') {
      top.quote = c;
    } else if (c === "[" && squares > 0) {
      squares++;
    } else if (c === "$" && line[i + 1] === "[") {
      squares++;
      i++;
    } else if (c === "]") {
      if (squares > 0) squares--;
    } else if (c === "(") {
      top.brackets++;
    } else if (c === ")") {
      if (top.brackets > 0) top.brackets--;
      else if (frames.length > 1 && !top.tick) frames.pop();
    } else if (c === "#" && (i === 0 || /[\s;&|(]/.test(line[i - 1]))) {
      // A comment runs to the end of the line, or to the backtick that
      // closes the backticks it stands in.
      const end = top.ticked ? line.indexOf("`", i) : -1;
      if (end < 0) break;
      i = end - 1;
    } else if (
      c === "<" &&
      line[i + 1] === "<" &&
      line[i + 2] !== "<" &&
      line[i - 1] !== "<"
    ) {
      const mark = shifts <= closes && squares === 0 && markAt(line, i + 2);
      if (mark) {
        const end = mark.end;
        if (pipe < end) {
          pipe = line.indexOf("|", end);
          if (pipe < 0) pipe = line.length;
          PIPED_TO_SHELL.lastIndex = pipe;
          piped = PIPED_TO_SHELL.test(line);
        }
        found.push({
          word: mark.word,
          tabs: mark.tabs,
          runs: piped || SHELL_BEFORE.test(line.slice(from, i)),
          expands: mark.bare,
          inside: top.ticked ? "`" : frames.length > 1 ? ")" : "",
        });
        from = end;
        i = end - 1;
      } else {
        i++;
      }
    }
  }
  return found;
}

/**
 * A line without the text of its here-documents: what stands between `<<WORD`
 * and the line that is `WORD` is written somewhere, not run, unless a shell
 * reads it; then its lines are read as commands, and a here-document one of
 * them opens is read before the rest. One that never ends is text to the end,
 * or to where the `$(…)` or backticks it was opened in close (`closedAt`);
 * what follows on that line is read again. What is kept is read only to
 * `READ`, and the line no further than that: `whole` says nothing was left.
 */
function withoutHereDocuments(command: string) {
  if (!command.includes("<<")) {
    return { text: command, whole: command.length <= READ };
  }
  const kept: string[] = [];
  const waiting: HereDocument[] = [];
  const frames = [{ quote: "", brackets: 0 }];
  // How long what is kept is.
  let size = 0;
  let whole = true;
  for (const line of command.split("\n")) {
    let rest = line;
    const open = waiting[0];
    if (open) {
      const text = (open.tabs ? line.replace(/^\t+/, "") : line).replace(
        /\r$/,
        "",
      );
      if (text === open.word) {
        waiting.shift();
        continue;
      }
      const cut = closedAt(open, text);
      if (cut >= 0) waiting.shift();
      // Backticks end every here-document opened in them, `$(…)` this one.
      if (cut >= 0 && open.inside === "`") {
        while (waiting[0]?.inside === "`") waiting.shift();
      }
      // What of the line is still its text: none of the mark before a `)`.
      const body =
        cut < 0 ? line : open.inside === ")" ? "" : text.slice(0, cut);
      rest = cut < 0 ? "" : text.slice(cut);
      if (open.runs) {
        rest = body + rest;
      } else {
        const runs = open.expands
          ? substitutionsIn(open.inside === "`" ? unticked(body) : body)
          : "";
        if (runs && size >= READ) {
          whole = false;
          break;
        }
        if (runs) {
          kept.push(runs);
          size += runs.length + 1;
        }
        if (cut < 0) continue;
      }
    }
    // Past what is read only the text of a here-document is passed over,
    // which is not kept anyway.
    if (size >= READ) {
      whole = false;
      break;
    }
    kept.push(rest);
    // A mark past what is read opens nothing that is read.
    waiting.unshift(...opened(rest.slice(0, READ - size), frames));
    size += rest.length + 1;
  }
  const text = kept.join("\n");
  return { text, whole: whole && text.length <= READ };
}

/** Where what the backtick or bracket at `start` opens ends. */
function closing(line: string, start: number) {
  if (line[start] === "`") {
    for (let i = start + 1; i < line.length; i++) {
      if (line[i] === "\\") i++;
      else if (line[i] === "`") return i;
    }
    return line.length;
  }
  let depth = 0;
  for (let i = start; i < line.length; i++) {
    if (line[i] === "(") depth++;
    else if (line[i] === ")" && --depth === 0) return i;
  }
  return line.length;
}

/**
 * The list of an array that opens at `start`: where it closes, and what the
 * `$(…)`, backticks and `<(…)` in it run. It is read past its quotes, its
 * comments and `${…}`, over as many lines as it takes. Nothing for what only
 * looks like a list, one cut by `;`, `|` or `&` or never closed, so that what
 * it holds is split by the caller alone, once.
 */
function listAt(line: string, start: number) {
  const runs: string[] = [];
  let quote = "";
  let brackets = 0;
  let braces = 0;
  // Where the character a backslash escaped last stands.
  let escaped = -1;
  for (let i = start; i < line.length; i++) {
    const c = line[i];
    if (quote === "'") {
      if (c === "'") quote = "";
    } else if (c === "\\") {
      escaped = ++i;
    } else if (quote === "$") {
      if (c === "'") quote = "";
    } else if (
      c === "`" ||
      (c === "$" && line[i + 1] === "(") ||
      (/[<>]/.test(c) && line[i + 1] === "(" && quote === "")
    ) {
      const open = c === "`" ? i : i + 1;
      const end = closing(line, open);
      if (c === "`" || line[open + 1] !== "(") {
        runs.push(held(line, open, end));
      }
      i = end;
    } else if (quote === '"') {
      if (c === '"') quote = "";
    } else if (c === "'" || c === '"') {
      quote = c;
    } else if (c === "$" && line[i + 1] === "'") {
      quote = "$";
      i++;
    } else if (c === "$" && line[i + 1] === "{") {
      braces++;
      i++;
    } else if (braces > 0) {
      if (c === "}") braces--;
    } else if (c === "#" && escaped !== i - 1 && /[\s(]/.test(line[i - 1])) {
      while (i + 1 < line.length && line[i + 1] !== "\n") i++;
    } else if (c === "(") {
      brackets++;
    } else if (c === ")") {
      if (--brackets === 0) return { end: i, runs };
    } else if (brackets === 1 && ";|&".includes(c)) {
      return undefined;
    }
  }
  return undefined;
}

/**
 * The commands of a line, each as its words with their quotes kept. The line
 * is cut at `;`, `&`, `|`, new lines and brackets outside quotes, so a quoted
 * `;` cuts nothing, `2>&1` is a redirection and a `#` comment is skipped. What
 * `$(…)` or backticks run comes after the command they stand in; `$((…))`
 * and `((…))` run nothing, and the list of an array, `a=(…)` or `a+=(…)`, is
 * values read to its closing bracket: only what `$(…)` or backticks run
 * inside it is a command (`listAt`). A `case` pattern is read to its own `)`
 * and is no command either, `a|b)` and `(a|b)` too; what each branch runs is.
 */
function split(line: string, budget: Budget): string[][] {
  const commands: string[][] = [];
  let inner: string[][] = [];
  let words: string[] = [];
  let word = "";
  // `'`, `"`, or `$` for `$'…'`, in which a backslash escapes.
  let quote = "";
  // How many `case` stand open, and whether a pattern comes next: after `in`
  // and after each `;;`, `;&` or `;;&`. `nested` counts the brackets open in
  // a pattern, `@(a|b)`.
  let cases = 0;
  let pattern = false;
  let nested = 0;
  const endWord = () => {
    if (!word) return;
    if (
      word === "in" &&
      words[words.length - 2] === "case" &&
      words.slice(0, -2).every((each) => BEFORE_A_COMMAND.has(each))
    ) {
      // `case WORD in` runs only what its word runs.
      commands.push(...inner);
      inner = [];
      words = [];
      cases++;
      pattern = true;
      nested = 0;
    } else {
      if (word === "esac" && cases > 0 && words.length === 0) {
        cases--;
        pattern = false;
      }
      words.push(word);
    }
    word = "";
  };
  const endCommand = () => {
    endWord();
    // A pattern cut before its `)` is a pattern still.
    if (words.length > 0 && !pattern) commands.push(words);
    commands.push(...inner);
    words = [];
    inner = [];
  };
  // Brackets open outside quotes: a `)` with none open ends a `case` pattern.
  let brackets = 0;
  for (let i = 0; i < line.length; i++) {
    const c = line[i];
    if (quote === "'") {
      word += c;
      if (c === "'") quote = "";
    } else if (quote === "$" && c === "\\") {
      word += line.slice(i, i + 2);
      i++;
    } else if (quote === "$") {
      word += c;
      if (c === "'") quote = "";
    } else if (c === "\\") {
      // A backslash before a new line joins two lines.
      if (line[i + 1] === "\n") endWord();
      else word += line.slice(i, i + 2);
      i++;
    } else if (c === "`" || (c === "$" && line[i + 1] === "(")) {
      const open = c === "`" ? i : i + 1;
      const end = closing(line, open);
      if ((c === "`" || line[open + 1] !== "(") && follows(budget)) {
        inner.push(...split(held(line, open, end), budget));
      }
      word += line.slice(i, end + 1);
      i = end;
    } else if (quote === '"') {
      word += c;
      if (c === '"') quote = "";
    } else if (c === "'" || c === '"') {
      word += c;
      quote = c;
    } else if (c === "$" && line[i + 1] === "'") {
      word += "$'";
      quote = "$";
      i++;
    } else if (
      pattern &&
      "|()".includes(c) &&
      (word !== "esac" || words.length > 0)
    ) {
      // A pattern runs to its own `)`, past its `|` and its `@(…)`; the `(`
      // that may open it is no subshell. In `(case …; esac)` the `)` is
      // the subshell's.
      if (c === ")" && nested === 0) {
        words = [];
        word = "";
        pattern = false;
      } else if (c !== "(" || word || words.length > 0) {
        if (c === "(") nested++;
        else if (c === ")") nested--;
        word += c;
      }
    } else if (c === "(" && (word.endsWith("=") || line[i + 1] === "(")) {
      const list =
        BEFORE_A_LIST.test(word) && !words.includes("[[")
          ? listAt(line, i)
          : undefined;
      for (const run of list?.runs ?? []) {
        if (follows(budget)) inner.push(...split(run, budget));
      }
      const end = list?.end ?? closing(line, i);
      word += line.slice(i, end + 1);
      i = end;
    } else if (c === "#" && !word) {
      while (i + 1 < line.length && line[i + 1] !== "\n") i++;
    } else if (c === ")" && brackets === 0) {
      endWord();
      words = [];
    } else if (
      c === "&"
        ? !/[<>]/.test(line[i - 1] ?? "") && line[i + 1] !== ">"
        : c === "|"
          ? line[i - 1] !== ">"
          : ";\n()".includes(c)
    ) {
      if (c === "(") brackets++;
      else if (c === ")") brackets--;
      endCommand();
      if (c === ";" && cases > 0 && /[;&]/.test(line[i + 1] ?? "")) {
        // `;;`, `;&` and `;;&` end a branch, and a pattern comes next.
        i += line.startsWith(";;&", i) ? 2 : 1;
        pattern = true;
        nested = 0;
      }
    } else if (/\s/.test(c)) {
      endWord();
    } else {
      word += c;
    }
  }
  endCommand();
  return commands;
}

/** Where a command begins: past the assignments and redirections from `i`. */
function begins(words: string[], i: number) {
  for (;;) {
    const word = words[i] ?? "";
    const sent = REDIRECTION.exec(word);
    if (ASSIGNMENT.test(word)) i++;
    // `2> file` has its target in the next word, `2>file` in its own.
    else if (sent) i += sent[0].length === word.length ? 2 : 1;
    else return i;
  }
}

/**
 * A command without the assignments, redirections, construct words and
 * `function NAME` before it. What a redirection names, a file or the text of
 * `<<<`, is no command, so a file read with `$(< file)` or the `<` before
 * `<(…)` runs nothing. Arithmetic, `((…))`, runs nothing either, and a
 * command named by what `$(…)` prints is known only by what that runs, which
 * is read already.
 */
function own(words: string[]) {
  let i = 0;
  for (;;) {
    i = begins(words, i);
    const word = words[i] ?? "";
    if (BEFORE_A_COMMAND.has(word)) i++;
    else if (word === "function") i += 2;
    else break;
  }
  const head = words[i] ?? "";
  return NO_COMMAND.has(head) || /^(?:\(\(|\$\(|`)/.test(head)
    ? []
    : words.slice(i);
}

/** `sudo curl` is curl, and so is the curl behind `env A=1` or `timeout 5`. */
function unwrapped(words: string[]) {
  let rest = words;
  for (let depth = 0; depth < 6; depth++) {
    const wrapper = WRAPPERS.get(name(rest[0]));
    if (!wrapper || wrapper.asks?.has(rest[1] ?? "")) break;
    rest = pastFlags(rest.slice(1), wrapper.takes);
    rest = rest.slice(begins(rest, 0) + (wrapper.skip ?? 0));
  }
  return rest;
}

/**
 * The commands one command runs: itself, each one `find` runs with `-exec`,
 * and in place of `bash -c "…"`, `su -c "…"`, `eval "…"` or `watch "…"` the
 * commands of the line handed over. A script's name is not a line:
 * `bash deploy.sh` is read no further. Shells inside shells are followed
 * three deep, and each command followed spends one of `budget`.
 */
function ran(words: string[], depth: number, budget: Budget): string[][] {
  const outer = unwrapped(words);
  const head = name(outer[0]);
  if (depth < 3) {
    let line: string | undefined;
    if (head === "eval") {
      line = outer.slice(1).map(unquoted).join(" ");
    } else if (head === "watch") {
      line = pastFlags(outer.slice(1), WATCH_TAKES).map(unquoted).join(" ");
    } else if (SHELLS.has(head)) {
      // The line follows the first flag that ends in c: `bash -lc "…"`,
      // `bash -o pipefail -c "…"`, and never comes after a script's name.
      let i = 1;
      while (/^[-+]/.test(outer[i] ?? "") && !/^-[a-z]*c$/.test(outer[i])) {
        i += TAKES_OPTION.test(outer[i]) ? 2 : 1;
      }
      if (/^-[a-z]*c$/.test(outer[i] ?? "") && i + 1 < outer.length) {
        line = unquoted(outer[i + 1]);
      }
    } else if (head === "su" || head === "runuser") {
      // Their own `-c`, never one of the command after `--`.
      const end = outer.indexOf("--");
      const options = end < 0 ? outer : outer.slice(0, end);
      const flag = options.findIndex((w) => w === "-c" || w === "--command");
      if (flag > 0 && flag + 1 < outer.length) {
        line = unquoted(outer[flag + 1]);
      } else if (head === "runuser" && follows(budget)) {
        return ran(pastFlags(outer.slice(1), RUNUSER_TAKES), depth + 1, budget);
      }
    }
    if (line !== undefined && follows(budget)) {
      return commandsIn(withoutHereDocuments(line).text, depth + 1, budget);
    }
  }
  const all = [outer];
  if (head === "find") {
    // Each `-exec` once, to its `;` or `+`. One that never ends runs the
    // rest, a later `-exec` with it, which is read there.
    for (let i = 1; i < outer.length; i++) {
      if (!/^-(?:exec|execdir|ok|okdir)$/.test(outer[i])) continue;
      let end = i + 1;
      while (end < outer.length && !FIND_ENDS.has(outer[end])) end++;
      if (!follows(budget)) break;
      all.push(...ran(outer.slice(i + 1, end), depth + 1, budget));
      i = end;
    }
  }
  return all.filter((command) => command.length > 0);
}

function commandsIn(line: string, depth: number, budget: Budget): string[][] {
  return split(line, budget)
    .map(own)
    .filter((words) => words.length > 0)
    .flatMap((words) => ran(words, depth, budget));
}

/**
 * Every command a line runs, here-documents left out, read to `READ`;
 * whether all of it was read, and whether it held more commands inside
 * others than `FOLLOWED`.
 */
function commandsOf(command: string) {
  const budget = { left: FOLLOWED };
  const { text, whole } = withoutHereDocuments(command);
  const commands = commandsIn(text.slice(0, READ), 0, budget);
  return { commands, whole, spent: budget.left < 0 };
}

/** Whether a command reaches the network: by its name, or with its subcommand. */
function reachesOut(words: string[]) {
  let head = name(words[0]);
  let rest = words.slice(1);
  if (NETWORK_WORDS.has(head)) return true;
  if (
    /^python[\d.]*$/.test(head) &&
    rest[0] === "-m" &&
    PY_MODULES.has(rest[1] ?? "")
  ) {
    [head, rest] = [rest[1], rest.slice(2)];
  }
  if (head === "uv" && rest[0] === "pip") [head, rest] = ["pip", rest.slice(1)];
  // pip3 and pip3.12 are pip.
  if (/^pip[\d.]+$/.test(head)) head = "pip";
  // yay or paru with no operation upgrades.
  if (PACMAN.has(head)) {
    return rest.some(syncs) || (head !== "pacman" && rest.length === 0);
  }
  // npx runs what is installed as readily as it downloads; only its own
  // "yes, install" says which.
  if (head === "npx") return rest.some((w) => w === "-y" || w === "--yes");
  rest = pastFlags(rest, TAKES.get(head));
  // yarn with no subcommand installs.
  if (head === "yarn" && rest.length === 0) return true;
  if ((head === "docker" || head === "podman") && rest[0] === "compose") {
    [head, rest] = [
      `${head} compose`,
      pastFlags(rest.slice(1), TAKES.get(head)),
    ];
  }
  const subcommands = NETWORK_SUBCOMMANDS.get(head);
  return (
    subcommands !== undefined &&
    (subcommands.has(rest[0] ?? "") ||
      subcommands.has(`${rest[0] ?? ""} ${rest[1] ?? ""}`))
  );
}

/**
 * A line belongs to the network when any command it runs reaches out, to
 * files when every one only handles files, and to the shell otherwise, or
 * when it holds more commands inside others than are followed. A line longer
 * than what is read is never one of files alone: what was not read may be
 * anything.
 */
export function shellStation(command: string | null): ShellStation {
  if (command === null) return "shell";
  const { commands, whole, spent } = commandsOf(command);
  if (spent) return "shell";
  if (commands.some(reachesOut)) return "network";
  if (!whole) return "shell";
  const real = commands.filter((words) => !NEUTRAL_WORDS.has(name(words[0])));
  return real.length > 0 &&
    real.every((words) => FILE_WORDS.has(name(words[0])))
    ? "files"
    : "shell";
}

/**
 * The name of the first command a line runs that says something: `lsblk` for
 * `sudo lsblk -d`, `make` for `cd /srv && make`.
 */
export function commandName(command: string): string | null {
  const names = commandsOf(command).commands.map((words) => name(words[0]));
  return names.find((each) => !NEUTRAL_WORDS.has(each)) ?? names[0] ?? null;
}
