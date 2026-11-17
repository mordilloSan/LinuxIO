import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import type {
  FullConfig,
  FullResult,
  Reporter,
  Suite,
  TestCase,
  TestError,
  TestResult,
} from "@playwright/test/reporter";

// The reporter of the VM specs (make test-vm-browser, make
// test-vm-browser-rerun), which run one Playwright worker for each VM. Every result names its VM, and what a spec
// prints is held until its test ends and printed under it, so lines of two VMs
// never mix. In a terminal a board under the results shows what each VM runs
// now; written to a file or a pipe, the results alone. The run ends with a
// table for each VM, a timeline of the files each one ran and the failures in
// full. Every line, without colour, also goes to
// LINUXIO_VM_LOG_DIR/test-vm-browser.log.

const repositoryRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../..",
);
const fromRoot = (file: string) => path.relative(repositoryRoot, file);
const logDirectory =
  process.env.LINUXIO_VM_LOG_DIR ||
  path.join(repositoryRoot, ".cache/test-logs");
const logPath = path.join(logDirectory, "test-vm-browser.log");

const tty = process.stdout.isTTY;
const useColor = tty && !process.env.NO_COLOR;
const ansi = new RegExp(`${String.fromCharCode(27)}\\[[0-9;]*[A-Za-z]`, "g");
const plain = (text: string) => text.replace(ansi, "");
const sgr = (code: string, text: string) =>
  useColor ? `\u001b[${code}m${text}\u001b[0m` : plain(text);
const dim = (text: string) => sgr("2", text);
const bold = (text: string) => sgr("1", text);
const green = (text: string) => sgr("32", text);
const red = (text: string) => sgr("31", text);
// Teal, amber, violet, blue: the same order the setup lines use.
const vmColors = [80, 215, 141, 75];
const inVmColor = (vm: number, text: string) =>
  vm > 0 ? sgr(`38;5;${vmColors[(vm - 1) % vmColors.length]}`, text) : text;
// 0 is a worker with no VM of its own (--workers past the VMs).
const vmTag = (vm: number) =>
  vm > 0 ? inVmColor(vm, bold(`vm${vm}`)) : red(bold("vm?"));

const seconds = (ms: number) =>
  ms < 60_000 ? `${(ms / 1000).toFixed(1)}s` : `${(ms / 60_000).toFixed(1)}m`;
const minutes = (ms: number) => {
  const total = Math.max(0, Math.round(ms / 1000));
  const m = Math.floor(total / 60);
  const s = total % 60;
  return m ? `${m}m ${String(s).padStart(2, "0")}s` : `${s}s`;
};
const clip = (text: string, width: number) =>
  text.length > width ? `${text.slice(0, Math.max(0, width - 1))}…` : text;

function titleOf(test: TestCase) {
  const parts = [test.title];
  let suite: Suite | undefined = test.parent;
  while (suite?.type === "describe") {
    parts.unshift(suite.title);
    suite = suite.parent;
  }
  return parts.join(" › ");
}

const whereOf = (test: TestCase) =>
  `${path.basename(test.location.file)}:${test.location.line}`;

interface Running {
  vm: number;
  number: number;
  test: TestCase;
  begun: number;
  printed: string[];
}

interface VmTally {
  tests: number;
  passed: number;
  failed: number;
  skipped: number;
  lastEnd: number;
}

interface FileSpan {
  vm: number;
  file: string;
  begun: number;
  ended: number;
}

export default class VmReporter implements Reporter {
  private readonly vmNumbers = (process.env.LINUXIO_VM_NUMBERS ?? "")
    .trim()
    .split(/\s+/)
    .filter(Boolean)
    .map(Number);
  private begun = Date.now();
  private started = 0;
  private finished = 0;
  private total = 0;
  private readonly running = new Map<string, Running>();
  private readonly onVm = new Map<number, Running>();
  private readonly tallies = new Map<number, VmTally>();
  private readonly spans: FileSpan[] = [];
  private readonly failures: { run: Running; result: TestResult }[] = [];
  private readonly runErrors: TestError[] = [];
  private workers = 1;
  private boardRows = 0;
  private ended = false;
  private timer: ReturnType<typeof setInterval> | undefined;
  private log: fs.WriteStream | undefined;

  printsToStdio() {
    return true;
  }

  onBegin(config: FullConfig, suite: Suite) {
    this.begun = Date.now();
    const tests = suite.allTests();
    this.total = tests.length;
    const files = new Set(tests.map((test) => test.location.file)).size;
    fs.mkdirSync(logDirectory, { recursive: true });
    this.log = fs.createWriteStream(logPath);
    this.workers = config.workers;
    const vms = this.vms();
    this.print([
      `${bold(`${vms.length} VM${vms.length === 1 ? "" : "s"}`)} ${dim("·")} ${this.total} tests in ${files} files ${dim("· a VM takes the next spec file when it is free")}`,
      "",
    ]);
    if (tty) {
      this.timer = setInterval(() => this.drawBoard(), 1000);
      this.timer.unref();
    }
  }

  onTestBegin(test: TestCase, result: TestResult) {
    const vm = this.vmOf(result);
    const run: Running = {
      vm,
      number: ++this.started,
      test,
      begun: Date.now(),
      printed: [],
    };
    this.running.set(`${test.id}#${result.retry}`, run);
    this.onVm.set(vm, run);
    const file = path.basename(test.location.file);
    const at = run.begun - this.begun;
    const last = this.spans.findLast((span) => span.vm === vm);
    if (last?.file === file) last.ended = at;
    else this.spans.push({ vm, file, begun: at, ended: at });
    this.drawBoard();
  }

  onStdOut(chunk: string | Buffer, test?: TestCase, result?: TestResult) {
    this.takeOutput(chunk, test, result);
  }

  onStdErr(chunk: string | Buffer, test?: TestCase, result?: TestResult) {
    this.takeOutput(chunk, test, result);
  }

  onTestEnd(test: TestCase, result: TestResult) {
    const key = `${test.id}#${result.retry}`;
    const run = this.running.get(key) ?? {
      vm: this.vmOf(result),
      number: ++this.started,
      test,
      begun: Date.now() - result.duration,
      printed: [],
    };
    this.running.delete(key);
    if (this.onVm.get(run.vm) === run) this.onVm.delete(run.vm);
    // A failed try that will be retried is not the test's last word.
    const final =
      result.status === "passed" ||
      result.status === "skipped" ||
      result.retry >= test.retries;
    if (final) this.finished++;

    const at = Date.now() - this.begun;
    const span = this.spans.findLast((s) => s.vm === run.vm);
    if (span) span.ended = at;
    const tally = this.tallyOf(run.vm);
    tally.tests++;
    tally.lastEnd = at;

    let mark: string;
    let time: string;
    const failed = result.status !== "passed" && result.status !== "skipped";
    if (result.status === "passed") {
      tally.passed++;
      mark = green("✓");
      time = dim(seconds(result.duration));
    } else if (result.status === "skipped") {
      tally.skipped++;
      mark = dim("-");
      time = dim("skipped");
    } else {
      tally.failed++;
      mark = red("✘");
      time = red(
        result.status === "timedOut"
          ? `timed out after ${seconds(result.duration)}`
          : `${result.status} after ${seconds(result.duration)}`,
      );
      this.failures.push({ run, result });
    }
    if (result.retry > 0) time += dim(` · retry ${result.retry}`);

    const bar = inVmColor(run.vm, "│");
    const lines = [
      `${vmTag(run.vm)}  ${mark} ${String(run.number).padStart(2)}  ${dim(whereOf(test).padEnd(26))} ${titleOf(test)}  ${time}`,
    ];
    if (run.printed.length > 0) {
      for (const line of run.printed.join("").replace(/\n+$/, "").split("\n")) {
        lines.push(`${bar}          ${dim(line)}`);
      }
    }
    const firstError = plain(result.error?.message ?? "").split("\n")[0];
    if (failed && firstError) {
      lines.push(`${bar}          ${red(firstError)}`);
    }
    this.print(lines);
  }

  onError(error: TestError) {
    this.runErrors.push(error);
    this.print([
      `${red("error:")} ${plain(error.message ?? String(error.value))}`,
    ]);
  }

  onEnd(result: FullResult) {
    this.ended = true;
    if (this.timer) clearInterval(this.timer);
    this.eraseBoard();
    const out: string[] = ["", ...this.table(), "", ...this.timeline()];

    if (this.failures.length > 0) {
      out.push("", red(bold(`${this.failures.length} failed`)));
      for (const { run, result } of this.failures) {
        out.push(
          "",
          `${vmTag(run.vm)}  ${red("✘")} ${run.number}  ${whereOf(run.test)} › ${titleOf(run.test)}`,
        );
        for (const error of result.errors) out.push(...this.describe(error));
        for (const attachment of result.attachments) {
          if (attachment.path)
            out.push(`    ${dim(`${attachment.name}: ${attachment.path}`)}`);
        }
      }
    }
    for (const error of this.runErrors) out.push("", ...this.describe(error));

    const passed = this.sum("passed");
    const failed = this.sum("failed");
    const skipped = this.sum("skipped");
    const ending =
      result.status === "interrupted"
        ? red(" · interrupted")
        : result.status === "timedout"
          ? red(" · the run timed out")
          : "";
    out.push(
      "",
      `${green(`${passed} passed`)}, ${skipped ? dim(`${skipped} skipped`) : "0 skipped"}, ${failed ? red(`${failed} failed`) : "0 failed"} in ${minutes(result.duration)}${ending}`,
      dim(`every line in full: ${fromRoot(logPath)}`),
    );
    this.print(out);
  }

  async onExit() {
    const log = this.log;
    if (log) await new Promise<void>((resolve) => log.end(resolve));
  }

  private vms() {
    if (this.vmNumbers.length > 0) return this.vmNumbers;
    return Array.from({ length: Math.max(1, this.workers) }, (_, i) => i + 1);
  }

  private vmOf(result: TestResult) {
    if (result.parallelIndex < 0) return 0;
    if (this.vmNumbers.length === 0) return result.parallelIndex + 1;
    return this.vmNumbers[result.parallelIndex] ?? 0;
  }

  private tallyOf(vm: number) {
    let tally = this.tallies.get(vm);
    if (!tally) {
      tally = { tests: 0, passed: 0, failed: 0, skipped: 0, lastEnd: 0 };
      this.tallies.set(vm, tally);
    }
    return tally;
  }

  private sum(key: "passed" | "failed" | "skipped") {
    let n = 0;
    for (const tally of this.tallies.values()) n += tally[key];
    return n;
  }

  private takeOutput(
    chunk: string | Buffer,
    test?: TestCase,
    result?: TestResult,
  ) {
    const text = chunk.toString();
    const run =
      test && result
        ? this.running.get(`${test.id}#${result.retry}`)
        : undefined;
    if (run) {
      run.printed.push(text);
      return;
    }
    this.print(
      text
        .replace(/\n+$/, "")
        .split("\n")
        .map((line) => dim(line)),
    );
  }

  private describe(error: TestError) {
    const lines: string[] = [];
    const indent = (text: string) =>
      plain(text)
        .split("\n")
        .map((line) => `    ${line}`);
    if (error.message)
      lines.push(...indent(error.message).map((line) => red(line)));
    if (error.snippet) lines.push("", ...indent(error.snippet));
    if (error.location) {
      lines.push(
        dim(
          `    at ${fromRoot(error.location.file)}:${error.location.line}:${error.location.column}`,
        ),
      );
    }
    return lines;
  }

  private table() {
    const end = Date.now() - this.begun;
    const rows = [
      bold("VM    tests  passed  failed  skipped  done after  idle at the end"),
    ];
    for (const vm of [...this.tallies.keys()].sort((a, b) => a - b)) {
      const t = this.tallyOf(vm);
      const idle = end - t.lastEnd;
      rows.push(
        `${vmTag(vm)}  ${String(t.tests).padStart(6)}  ${String(t.passed).padStart(6)}  ${(t.failed ? red : (s: string) => s)(String(t.failed).padStart(6))}  ${String(t.skipped).padStart(7)}  ${minutes(t.lastEnd).padStart(10)}  ${idle < 5_000 ? dim("none") : minutes(idle)}`,
      );
    }
    return rows;
  }

  // One row per VM, a mark every few seconds, each file named in its stretch.
  private timeline() {
    const end = Math.max(1, Date.now() - this.begun);
    const width = Math.min(process.stdout.columns || 100, 120) - 6;
    const step = Math.max(5, Math.ceil(end / 1000 / width / 5) * 5) * 1000;
    const rows = [dim(`which file ran where, one mark per ${step / 1000} s`)];
    for (const vm of [...this.tallies.keys()].sort((a, b) => a - b)) {
      // Columns are time: a file starts at its own column, after the gap
      // its VM spent before it, and takes at least one.
      let row = "";
      let column = 0;
      for (const span of this.spans.filter((s) => s.vm === vm)) {
        const from = Math.max(column, Math.round(span.begun / step));
        const to = Math.max(from + 1, Math.round(span.ended / step));
        const name = span.file.replace(/\.spec\.ts$/, "").replace(/^jobs-/, "");
        const room = to - from - 1;
        row +=
          " ".repeat(from - column) +
          inVmColor(vm, "▏") +
          (room > 0 ? clip(name, room).padEnd(room) : "");
        column = to;
      }
      rows.push(`${vmTag(vm)}  ${row}`);
    }
    return rows;
  }

  private print(lines: string[]) {
    this.eraseBoard();
    for (const line of lines) {
      process.stdout.write(`${useColor ? line : plain(line)}\n`);
      this.log?.write(`${plain(line)}\n`);
    }
    this.drawBoard();
  }

  private eraseBoard() {
    if (this.boardRows === 0) return;
    process.stdout.write(`\u001b[${this.boardRows}A\u001b[0J`);
    this.boardRows = 0;
  }

  private drawBoard() {
    if (!tty || !this.log || this.ended) return;
    const columns = Math.min(
      140,
      Math.max(60, (process.stdout.columns || 100) - 1),
    );
    const now = Date.now();
    const head = `── ${this.finished}/${this.total} done · ${minutes(now - this.begun)} `;
    const rows = [dim(head.padEnd(columns, "─"))];
    const titleWidth = Math.max(10, columns - 54);
    for (const vm of this.vms()) {
      const run = this.onVm.get(vm);
      const done = this.tallies.get(vm)?.tests ?? 0;
      const count = dim(`${String(done).padStart(3)} done`);
      if (run) {
        rows.push(
          `${vmTag(vm)}  ${inVmColor(vm, "▶")} ${dim(whereOf(run.test).padEnd(26))} ${clip(titleOf(run.test), titleWidth).padEnd(titleWidth)} ${minutes(now - run.begun).padStart(7)} ${count}`,
        );
      } else {
        rows.push(
          `${vmTag(vm)}  ${dim("· idle".padEnd(28 + titleWidth + 8))} ${count}`,
        );
      }
    }
    this.eraseBoard();
    process.stdout.write(`${rows.join("\n")}\n`);
    this.boardRows = rows.length;
  }
}
