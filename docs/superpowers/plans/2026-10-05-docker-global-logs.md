# Docker Global Logs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Docker "Logs" tab that streams the merged, live logs of every running container with per-container colour badges, like Dozzle's merged view.

**Architecture:** One new bridge duplex Channel (`docker.logs.follow_all`) fans in `ContainerLogs` for every running container plus any container that starts later (Docker events), writing NDJSON records straight to the Channel. The frontend gets a new `/docker/logs` route whose page parses the records, keeps a sorted, capped buffer, and renders it in `AppVirtualTable` with a search field in the tab bar.

**Tech Stack:** Go (moby client v0.6.0, `stdcopy`, `relay` frames), React 19 + TanStack Router/Query, `AppVirtualTable`, vitest, Make targets.

**Spec:** `docs/superpowers/specs/2026-10-05-docker-global-logs-design.md`

## Global Constraints

- Never run `npm`, `go test`, `vitest`, `golangci-lint` etc. directly; only Make targets (focused: `make test-go-quiet GO_TEST_PKGS=./bridge/handlers/docker/...`, `make test-frontend-only VITEST_FILE=<file>`; final: `make generate` then `make test-quiet`).
- Do not hand-edit `frontend/src/api/generated/*` or `frontend/src/routeTree.gen.ts`; `make generate` and the router plugin own them.
- No hex colours, `fontSize:` inline, `useAppTheme()` or `palette.mode` outside `components/ui` and `theme/`; use `--app-*` variables and `components/ui` components.
- No `useCallback`/`useMemo`/`memo` except for props consumed by `AppVirtualTable` (columns).
- Handlers take `ctx` first and propagate it; goroutines have an owner, cancellation path and exit; no `panic` for runtime failures.
- Never create a Git commit; the user commits.

## Review Focus

1. A log line split across two Docker frames must come out as one record, not two (`dockerLogLineWriter` partial carry; tested in Task 1).
2. A TTY container's `\r\n` endings and ANSI escapes must not leak into `line` (Task 1 TTY test).
3. A container that restarts while the page is open must reappear without duplicating its previous run (event follower uses `Since` = event time, Task 2; not unit-testable without Docker, verified by inspection).
4. Backlog from several containers arrives interleaved; the table must show it in timestamp order (`mergeDockerLogEntries` test in Task 4).
5. Turning Live back on must not re-download the tail and duplicate rows (page reopens with tail `0` once data was received; Task 5 test).

---

### Task 1: Backend line writer (NDJSON records from container log bytes)

**Files:**
- Create: `backend/bridge/handlers/docker/logs_follow_all.go`
- Test: `backend/bridge/handlers/docker/logs_follow_all_test.go`

**Interfaces:**
- Produces: `type dockerLogLineWriter struct { stream io.Writer; id, name string; stderr bool; partial []byte }` with `Write([]byte) (int, error)` and `Close() error`; `func copyContainerLogs(reader io.Reader, tty bool, stdout, stderr *dockerLogLineWriter) error`; `const routeDockerLogsFollowAll = "docker.logs.follow_all"`.

- [ ] **Step 1: Write the failing tests**

```go
package docker

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

func dockerMuxFrame(streamType byte, payload string) []byte {
	frame := make([]byte, 8+len(payload))
	frame[0] = streamType
	binary.BigEndian.PutUint32(frame[4:8], uint32(len(payload)))
	copy(frame[8:], payload)
	return frame
}

func readDockerLogRecords(t *testing.T, out *bytes.Buffer) []dockerLogRecord {
	t.Helper()
	var records []dockerLogRecord
	reader := bytes.NewReader(out.Bytes())
	for reader.Len() > 0 {
		frame, err := relay.ReadRelayFrame(reader)
		if err != nil {
			t.Fatalf("read frame: %v", err)
		}
		if frame.Opcode != relay.OpStreamData {
			t.Fatalf("opcode = %d, want data", frame.Opcode)
		}
		for _, line := range bytes.Split(bytes.TrimRight(frame.Payload, "\n"), []byte("\n")) {
			var rec dockerLogRecord
			if err := json.Unmarshal(line, &rec); err != nil {
				t.Fatalf("decode %q: %v", line, err)
			}
			records = append(records, rec)
		}
	}
	return records
}

func TestCopyContainerLogsDemuxesAndJoinsPartialLines(t *testing.T) {
	var out bytes.Buffer
	stdout := &dockerLogLineWriter{stream: &out, id: "abc123def456", name: "web"}
	stderr := &dockerLogLineWriter{stream: &out, id: "abc123def456", name: "web", stderr: true}
	var src bytes.Buffer
	src.Write(dockerMuxFrame(1, "2026-10-05T10:00:00.000000001Z hello \x1b[32mworld\x1b[0m\n2026-10-05T10:00:00.000000002Z par"))
	src.Write(dockerMuxFrame(1, "tial line\n"))
	src.Write(dockerMuxFrame(2, "2026-10-05T10:00:00.000000003Z oops\n"))
	src.Write(dockerMuxFrame(1, "2026-10-05T10:00:00.000000004Z no newline at end"))

	if err := copyContainerLogs(&src, false, stdout, stderr); err != nil {
		t.Fatalf("copy: %v", err)
	}

	got := readDockerLogRecords(t, &out)
	want := []dockerLogRecord{
		{ID: "abc123def456", Name: "web", TS: "2026-10-05T10:00:00.000000001Z", Line: "hello world"},
		{ID: "abc123def456", Name: "web", TS: "2026-10-05T10:00:00.000000002Z", Line: "partial line"},
		{ID: "abc123def456", Name: "web", TS: "2026-10-05T10:00:00.000000003Z", Line: "oops", Stderr: true},
		{ID: "abc123def456", Name: "web", TS: "2026-10-05T10:00:00.000000004Z", Line: "no newline at end"},
	}
	if len(got) != len(want) {
		t.Fatalf("records = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("record %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestCopyContainerLogsRawTTY(t *testing.T) {
	var out bytes.Buffer
	stdout := &dockerLogLineWriter{stream: &out, id: "abc123def456", name: "tty"}
	src := bytes.NewBufferString("2026-10-05T10:00:00Z \x1b[1mstarted\x1b[0m\r\nno timestamp here\r\n")

	if err := copyContainerLogs(src, true, stdout, nil); err != nil {
		t.Fatalf("copy: %v", err)
	}

	got := readDockerLogRecords(t, &out)
	want := []dockerLogRecord{
		{ID: "abc123def456", Name: "tty", TS: "2026-10-05T10:00:00Z", Line: "started"},
		{ID: "abc123def456", Name: "tty", TS: "", Line: "no timestamp here"},
	}
	if len(got) != len(want) {
		t.Fatalf("records = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("record %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `make test-go-quiet GO_TEST_PKGS=./bridge/handlers/docker/...`
Expected: build failure, `undefined: dockerLogLineWriter`.

- [ ] **Step 3: Write the line writer and copy helper**

```go
package docker

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/moby/moby/api/pkg/stdcopy"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

const routeDockerLogsFollowAll = "docker.logs.follow_all"

// dockerLogRecord is one NDJSON line on the merged log Channel.
type dockerLogRecord struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	TS     string `json:"ts"`
	Line   string `json:"line"`
	Stderr bool   `json:"stderr,omitempty"`
}

// dockerLogLineWriter turns one container's log bytes into NDJSON records.
// Every Write emits the whole lines it completes as one relay frame; a
// trailing partial line is carried into the next Write and flushed by Close.
type dockerLogLineWriter struct {
	stream  io.Writer
	id      string
	name    string
	stderr  bool
	partial []byte
}

func (w *dockerLogLineWriter) Write(p []byte) (int, error) {
	data := append(w.partial, p...)
	w.partial = nil
	var frame bytes.Buffer
	for {
		idx := bytes.IndexByte(data, '\n')
		if idx < 0 {
			break
		}
		w.appendRecord(&frame, data[:idx])
		data = data[idx+1:]
	}
	if len(data) > 0 {
		w.partial = append([]byte(nil), data...)
	}
	return len(p), w.writeFrame(&frame)
}

// Close flushes a final line that had no trailing newline.
func (w *dockerLogLineWriter) Close() error {
	if len(w.partial) == 0 {
		return nil
	}
	var frame bytes.Buffer
	w.appendRecord(&frame, w.partial)
	w.partial = nil
	return w.writeFrame(&frame)
}

func (w *dockerLogLineWriter) writeFrame(frame *bytes.Buffer) error {
	if frame.Len() == 0 {
		return nil
	}
	return relay.WriteRelayFrame(w.stream, &relay.StreamFrame{Opcode: relay.OpStreamData, StreamID: 0, Payload: frame.Bytes()})
}

func (w *dockerLogLineWriter) appendRecord(frame *bytes.Buffer, raw []byte) {
	line := strings.TrimRight(string(dockerLogANSIRegex.ReplaceAll(raw, nil)), "\r")
	// Docker prefixes each line with an RFC3339Nano timestamp when Timestamps is set.
	ts, text, _ := strings.Cut(line, " ")
	if _, err := time.Parse(time.RFC3339Nano, ts); err != nil {
		ts, text = "", line
	}
	encoded, err := json.Marshal(dockerLogRecord{ID: w.id, Name: w.name, TS: ts, Line: text, Stderr: w.stderr})
	if err != nil {
		return
	}
	frame.Write(encoded)
	frame.WriteByte('\n')
}

// copyContainerLogs drains one container's log stream into the line writers.
// TTY containers return raw bytes; the others are stdout/stderr multiplexed.
func copyContainerLogs(reader io.Reader, tty bool, stdout, stderr *dockerLogLineWriter) error {
	var err error
	if tty {
		_, err = io.Copy(stdout, reader)
	} else {
		_, err = stdcopy.StdCopy(stdout, stderr, reader)
	}
	if closeErr := stdout.Close(); err == nil {
		err = closeErr
	}
	if stderr != nil {
		if closeErr := stderr.Close(); err == nil {
			err = closeErr
		}
	}
	return err
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `make test-go-quiet GO_TEST_PKGS=./bridge/handlers/docker/...`
Expected: PASS (the existing `logs_test.go` tests keep passing too).

---

### Task 2: Backend Channel handler, contract and registration

**Files:**
- Modify: `backend/bridge/handlers/docker/logs_follow_all.go` (append)
- Modify: `backend/bridge/apischema/contracts.go` (after `DockerLogsFollowRequest`, line ~534)
- Modify: `backend/bridge/handlers/docker/handlers.go:66-70` (add a second `DuplexRoute`)
- Modify: `backend/bridge/apischema/schema_test.go:406` (add the route to the direct-channel list)
- Modify: `docs/api-contract.md:539` (add a table row)

**Interfaces:**
- Consumes: `dockerLogLineWriter`, `copyContainerLogs` from Task 1; `getClient`/`releaseClient`, `writeDockerLogErrorUnlessCanceled` from `logs.go`/`docker.go`.
- Produces: `apischema.DockerLogsFollowAllRequest { Tail *string \`json:"tail,omitempty"\` }`; route `docker.logs.follow_all` (duplex, `NoEndpoint`).

- [ ] **Step 1: Extend the schema test (fails until the route is registered)**

In `schema_test.go` change the loop at line 406 to:

```go
	for _, route := range []string{"docker.logs.follow", "docker.logs.follow_all", "logs.general.follow", "logs.service.follow"} {
```

Run: `make test-go-quiet GO_TEST_PKGS=./bridge/apischema/...`
Expected: FAIL, unknown route `docker.logs.follow_all`.

- [ ] **Step 2: Add the contract type**

In `contracts.go` after `DockerLogsFollowRequest`:

```go
type DockerLogsFollowAllRequest struct {
	Tail *string `json:"tail,omitempty"`
}
```

- [ ] **Step 3: Write the handler**

Append to `logs_follow_all.go` (add imports `context`, `errors`, `fmt`, `log/slog`, `net`, `sync`, `github.com/moby/moby/client`, `apischema`, `runtime`, `bridgeipc`):

```go
// streamDockerLogsFollowAllChannel merges the logs of every running container
// into one Channel and attaches containers that start while it is open.
func streamDockerLogsFollowAllChannel(parent context.Context, stream net.Conn, _ runtime.Runtime, req apischema.DockerLogsFollowAllRequest) error {
	ctx, cleanup := bridgeipc.ReceiveOnlyChannelContext(parent, stream)
	defer cleanup()
	tail := "100"
	if req.Tail != nil && *req.Tail != "" {
		tail = *req.Tail
	}
	slog.Debug("starting merged docker log channel", "component", "docker", "route", routeDockerLogsFollowAll, "mode", tail)

	cli, err := getClient()
	if err != nil {
		slog.Error("failed to get docker client", "component", "docker", "route", routeDockerLogsFollowAll, "error", err)
		return writeDockerLogErrorUnlessCanceled(ctx, stream, err)
	}
	defer releaseClient(cli)

	// Subscribe before listing so a container that starts in between is not missed.
	events := cli.Events(ctx, client.EventsListOptions{
		Filters: make(client.Filters).Add("type", "container").Add("event", "start"),
	})
	containers, err := cli.ContainerList(ctx, client.ContainerListOptions{})
	if err != nil {
		slog.Error("failed to list containers for logs", "component", "docker", "route", routeDockerLogsFollowAll, "error", err)
		return writeDockerLogErrorUnlessCanceled(ctx, stream, err)
	}

	var followers sync.WaitGroup
	follow := func(id, name string, options client.ContainerLogsOptions) {
		followers.Add(1)
		go func() {
			defer followers.Done()
			followContainerLogs(ctx, cli, stream, id, name, options)
		}()
	}
	for _, c := range containers.Items {
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		follow(c.ID, name, client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true, Timestamps: true, Follow: true, Tail: tail})
	}

	var eventsErr error
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case msg, ok := <-events.Messages:
			if !ok {
				break loop
			}
			// Everything since the start event: no gap before the follower
			// attaches, and no lines from the previous run of a restarted container.
			since := fmt.Sprintf("%d.%09d", msg.Time, msg.TimeNano%int64(time.Second))
			follow(msg.Actor.ID, msg.Actor.Attributes["name"], client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true, Timestamps: true, Follow: true, Tail: "all", Since: since})
		case err, ok := <-events.Err:
			if ok && err != nil && !errors.Is(err, io.EOF) {
				eventsErr = err
			}
			break loop
		}
	}
	followers.Wait()
	if eventsErr != nil {
		slog.Error("docker event stream failed", "component", "docker", "route", routeDockerLogsFollowAll, "error", eventsErr)
		return writeDockerLogErrorUnlessCanceled(ctx, stream, eventsErr)
	}
	if ctx.Err() != nil {
		return relay.WriteStreamClose(stream, 0)
	}
	return relay.WriteResultOKAndClose(stream, 0, map[string]any{"status": "stopped"})
}

// followContainerLogs streams one container until it stops or the Channel ends.
// Failures are logged, not returned: one container must not end the merged view.
func followContainerLogs(ctx context.Context, cli *client.Client, stream net.Conn, id, name string, options client.ContainerLogsOptions) {
	inspect, err := cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		slog.Debug("skipping container logs", "component", "docker", "route", routeDockerLogsFollowAll, "container", id, "error", err)
		return
	}
	tty := inspect.Container.Config != nil && inspect.Container.Config.Tty
	reader, err := cli.ContainerLogs(ctx, id, options)
	if err != nil {
		slog.Debug("skipping container logs", "component", "docker", "route", routeDockerLogsFollowAll, "container", id, "error", err)
		return
	}
	defer reader.Close()
	shortID := id
	if len(shortID) > 12 {
		shortID = shortID[:12]
	}
	stdout := &dockerLogLineWriter{stream: stream, id: shortID, name: name}
	stderr := &dockerLogLineWriter{stream: stream, id: shortID, name: name, stderr: true}
	if err := copyContainerLogs(reader, tty, stdout, stderr); err != nil && ctx.Err() == nil {
		slog.Debug("container log follow ended", "component", "docker", "route", routeDockerLogsFollowAll, "container", id, "error", err)
	}
}
```

- [ ] **Step 4: Register the route**

In `handlers.go` after the `docker.logs.follow` `DuplexRoute` entry:

```go
		apischema.DuplexRoute[apischema.DockerLogsFollowAllRequest, apischema.NoResponse](routeDockerLogsFollowAll, apischema.NoEndpoint()).Duplex(
			func(ctx context.Context, stream net.Conn, req apischema.DockerLogsFollowAllRequest) error {
				return streamDockerLogsFollowAllChannel(ctx, stream, rt, req)
			},
		),
```

- [ ] **Step 5: Document the Channel**

In `docs/api-contract.md` after the `docker.logs.follow` row:

```markdown
| `openChannel("docker.logs.follow_all", request)` | `docker.logs.follow_all` | Direct server-producing merged log Channel for every running container; NDJSON records `{id, name, ts, line, stderr?}`, containers that start later are attached. |
```

- [ ] **Step 6: Run backend checks**

Run: `make test-go-quiet GO_TEST_PKGS=./bridge/...` then `make check-backend-quiet`
Expected: PASS; lint clean.

---

### Task 3: Generated contract and tab entry

**Files:**
- Modify: `frontend/src/routes/_authenticated/docker/-components/dockerTabs.ts`
- Generated: `frontend/src/api/generated/linuxio-types.ts`, `frontend/src/api/generated/route-metadata.ts` (via `make generate`)

- [ ] **Step 1: Regenerate the frontend contract**

Run: `make generate`
Expected: `linuxio-types.ts` gains `DockerLogsFollowAllRequest` and the channel map entry `"docker.logs.follow_all": DockerLogsFollowAllRequest`; `route-metadata.ts` gains `"docker.logs.follow_all": "duplex"`. Verify with:

```bash
rg -n "follow_all" frontend/src/api/generated/
```

- [ ] **Step 2: Add the tab**

```ts
export const DOCKER_TABS = [
  { label: "Dashboard", to: "/docker" },
  { label: "Containers", to: "/docker/containers" },
  { label: "Logs", to: "/docker/logs" },
  { label: "Stacks", to: "/docker/compose" },
  { label: "Networks", to: "/docker/networks" },
  { label: "Topology", to: "/docker/topology" },
  { label: "Volumes", to: "/docker/volumes" },
  { label: "Images", to: "/docker/images" },
] as const satisfies readonly RoutedTab[];
```

`RoutedTab.to` is typed against the route tree, so this does not type-check until Task 6 adds the route file. That is expected; do Tasks 4–6 before running frontend checks.

---

### Task 4: Frontend pure helpers (parse, merge, hue, time)

**Files:**
- Create: `frontend/src/routes/_authenticated/docker/-components/dockerLogs.ts`
- Test: `frontend/src/routes/_authenticated/docker/-components/dockerLogs.test.ts`

**Interfaces:**
- Produces:
  - `interface DockerLogEntry { id: string; line: string; name: string; seq: number; stderr: boolean; ts: string }`
  - `DOCKER_LOG_BUFFER_LIMIT = 5000`
  - `parseDockerLogFrame(text: string, firstSeq: number): DockerLogEntry[]`
  - `mergeDockerLogEntries(current: DockerLogEntry[], batch: DockerLogEntry[], limit?: number): DockerLogEntry[]`
  - `dockerLogHue(name: string): number` (0–359)
  - `formatDockerLogTime(ts: string): string`

- [ ] **Step 1: Write the failing tests**

```ts
import { describe, expect, it } from "vitest";

import {
  dockerLogHue,
  formatDockerLogTime,
  mergeDockerLogEntries,
  parseDockerLogFrame,
  type DockerLogEntry,
} from "./dockerLogs";

const entry = (
  seq: number,
  ts: string,
  name = "web",
  line = `line ${seq}`,
): DockerLogEntry => ({ id: "abc123def456", line, name, seq, stderr: false, ts });

describe("parseDockerLogFrame", () => {
  it("parses NDJSON records and skips malformed lines", () => {
    const text =
      '{"id":"abc123def456","name":"web","ts":"2026-10-05T10:00:00.000000001Z","line":"hello"}\n' +
      "not json\n" +
      '{"id":"abc123def456","name":"web","ts":"2026-10-05T10:00:00.000000002Z","line":"oops","stderr":true}\n';
    expect(parseDockerLogFrame(text, 7)).toEqual([
      entry(7, "2026-10-05T10:00:00.000000001Z", "web", "hello"),
      { ...entry(8, "2026-10-05T10:00:00.000000002Z", "web", "oops"), stderr: true },
    ]);
  });
});

describe("mergeDockerLogEntries", () => {
  it("sorts an interleaved backlog by timestamp", () => {
    const current = [entry(0, "2026-10-05T10:00:05Z", "web")];
    const batch = [
      entry(1, "2026-10-05T10:00:03Z", "db"),
      entry(2, "2026-10-05T10:00:01Z", "db"),
      entry(3, "2026-10-05T10:00:06Z", "web"),
    ];
    expect(mergeDockerLogEntries(current, batch).map((e) => e.seq)).toEqual([
      2, 1, 0, 3,
    ]);
  });

  it("appends a live batch without resorting and drops the oldest past the cap", () => {
    const current = [entry(0, "2026-10-05T10:00:01Z"), entry(1, "2026-10-05T10:00:02Z")];
    const batch = [entry(2, "2026-10-05T10:00:03Z")];
    expect(mergeDockerLogEntries(current, batch, 2).map((e) => e.seq)).toEqual([
      1, 2,
    ]);
  });

  it("keeps arrival order for entries without a timestamp", () => {
    const current = [entry(0, "2026-10-05T10:00:05Z")];
    const batch = [entry(1, ""), entry(2, "")];
    expect(mergeDockerLogEntries(current, batch).map((e) => e.seq)).toEqual([
      0, 1, 2,
    ]);
  });
});

describe("dockerLogHue", () => {
  it("is stable per name and within the hue circle", () => {
    expect(dockerLogHue("web")).toBe(dockerLogHue("web"));
    expect(dockerLogHue("web")).not.toBe(dockerLogHue("db"));
    for (const name of ["web", "db", "cache", "a", ""]) {
      const hue = dockerLogHue(name);
      expect(hue).toBeGreaterThanOrEqual(0);
      expect(hue).toBeLessThan(360);
    }
  });
});

describe("formatDockerLogTime", () => {
  it("formats a valid timestamp as a local clock time and passes junk through", () => {
    expect(formatDockerLogTime("2026-10-05T10:00:00.000000001Z")).toMatch(
      /^\d{2}:\d{2}:\d{2}$/,
    );
    expect(formatDockerLogTime("")).toBe("");
    expect(formatDockerLogTime("junk")).toBe("junk");
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `make test-frontend-only VITEST_FILE=src/routes/_authenticated/docker/-components/dockerLogs.test.ts`
Expected: FAIL, cannot resolve `./dockerLogs`.

- [ ] **Step 3: Write the helpers**

```ts
export interface DockerLogEntry {
  /** Short container id. */
  id: string;
  line: string;
  name: string;
  /** Arrival order; the row id and the tie-breaker for equal timestamps. */
  seq: number;
  stderr: boolean;
  /** Docker RFC3339Nano timestamp, or "" when the line had none. */
  ts: string;
}

export const DOCKER_LOG_BUFFER_LIMIT = 5000;

// Hour, minute and second spelled out: { hour12 } alone formats a date.
const TIME_FORMATTER = new Intl.DateTimeFormat(undefined, {
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hour12: false,
});

/** One Channel frame holds one or more NDJSON records; malformed lines are skipped. */
export function parseDockerLogFrame(
  text: string,
  firstSeq: number,
): DockerLogEntry[] {
  const entries: DockerLogEntry[] = [];
  for (const raw of text.split("\n")) {
    if (!raw) continue;
    let record: unknown;
    try {
      record = JSON.parse(raw);
    } catch {
      continue;
    }
    if (typeof record !== "object" || record === null) continue;
    const { id, name, ts, line, stderr } = record as Record<string, unknown>;
    if (
      typeof id !== "string" ||
      typeof name !== "string" ||
      typeof line !== "string"
    ) {
      continue;
    }
    entries.push({
      id,
      line,
      name,
      seq: firstSeq + entries.length,
      stderr: stderr === true,
      ts: typeof ts === "string" ? ts : "",
    });
  }
  return entries;
}

// Docker timestamps are UTC with nine fractional digits, so they order as strings.
function compareDockerLogEntries(a: DockerLogEntry, b: DockerLogEntry): number {
  if (a.ts && b.ts && a.ts !== b.ts) return a.ts < b.ts ? -1 : 1;
  return a.seq - b.seq;
}

/**
 * Appends a flushed batch in timestamp order and drops the oldest entries past
 * `limit`. The backlog of several containers arrives interleaved, so a batch
 * that starts before the newest buffered line re-sorts the whole buffer; live
 * batches just append.
 */
export function mergeDockerLogEntries(
  current: DockerLogEntry[],
  batch: DockerLogEntry[],
  limit = DOCKER_LOG_BUFFER_LIMIT,
): DockerLogEntry[] {
  if (batch.length === 0) return current;
  const sorted = batch.toSorted(compareDockerLogEntries);
  const newest = current.at(-1);
  // ponytail: O(n log n) resort on out-of-order batches; a merge pass if the
  // 5000-entry cap ever grows.
  const next =
    newest && compareDockerLogEntries(sorted[0], newest) < 0
      ? [...current, ...sorted].sort(compareDockerLogEntries)
      : current.concat(sorted);
  return next.length > limit ? next.slice(next.length - limit) : next;
}

/** Deterministic hue for a container name so its badge colour is stable across sessions. */
export function dockerLogHue(name: string): number {
  let hash = 0;
  for (const char of name) {
    hash = (hash * 31 + (char.codePointAt(0) ?? 0)) >>> 0;
  }
  return hash % 360;
}

export function formatDockerLogTime(ts: string): string {
  if (!ts) return "";
  const date = new Date(ts);
  return Number.isNaN(date.getTime()) ? ts : TIME_FORMATTER.format(date);
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `make test-frontend-only VITEST_FILE=src/routes/_authenticated/docker/-components/dockerLogs.test.ts`
Expected: PASS.

---

### Task 5: Page component, styles and test

**Files:**
- Create: `frontend/src/routes/_authenticated/docker/-components/DockerLogsPage.tsx`
- Create: `frontend/src/routes/_authenticated/docker/-components/docker-logs.css`
- Test: `frontend/src/routes/_authenticated/docker/-components/DockerLogsPage.test.tsx`
- Modify: `frontend/src/constants/apiLayering.test.ts:150-158` (register the direct `openChannel` consumer)

**Interfaces:**
- Consumes: Task 4 helpers; `openChannel("docker.logs.follow_all", { tail })` typed by Task 3's generation; `useLiveStream`, `useStreamMux`, `AppVirtualTable`, `RoutedTabSearch`, `AppHeaderSearch`, `AppSelect`, `AppChip`, `AppSwitch`, `AppFormControlLabel`, `AppTooltip`, `AppActionIconButton`, `AppAlert`, `AppTypography`, `PageLoader`, `copyToClipboard`.
- Produces: `default DockerLogsPage({ container?: string; onContainerChange(next: string | undefined): void })`.

- [ ] **Step 1: Write the failing page test**

```tsx
import { beforeEach, describe, expect, it, vi } from "vitest";

import { act, fireEvent, render, screen, waitFor } from "@/test/render";

import DockerLogsPage from "./DockerLogsPage";
import type { DockerLogEntry } from "./dockerLogs";

const mocks = vi.hoisted(() => {
  const streamRef: { current: object | null } = { current: null };
  return {
    closeStream: vi.fn(() => {
      streamRef.current = null;
    }),
    openChannel: vi.fn(() => ({})),
    openStream: vi.fn((options: Record<string, any>) => {
      mocks.streamOptions = options;
      options.open();
      streamRef.current = {};
      return true;
    }),
    streamOptions: null as Record<string, any> | null,
    streamRef,
  };
});

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    linuxio: {
      ...actual.linuxio,
      docker: {
        ...actual.linuxio.docker,
        list_containers: {
          queryKey: ["linuxio", "docker", "list_containers"],
          queryFn: () =>
            Promise.resolve([
              { Id: "abc123def456789", Names: ["/web"], State: "running" },
              { Id: "fed654cba321000", Names: ["/db"], State: "running" },
              { Id: "000000000000000", Names: ["/old"], State: "exited" },
            ]),
        },
      },
    },
    openChannel: mocks.openChannel,
    useStreamMux: () => ({ isOpen: true }),
  };
});

vi.mock("@/hooks/useLiveStream", () => ({
  useLiveStream: () => ({
    closeStream: mocks.closeStream,
    openStream: mocks.openStream,
    streamRef: mocks.streamRef,
  }),
}));

vi.mock("@/components/tabbar", () => ({
  RoutedTabSearch: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
}));

vi.mock("@/components/tables/AppVirtualTable", () => ({
  default: ({
    data,
    scrollElementRef,
  }: {
    data: DockerLogEntry[];
    scrollElementRef: { current: HTMLDivElement | null };
  }) => (
    <div data-testid="docker-logs-table" ref={scrollElementRef}>
      {data.map((entry) => (
        <div
          data-name={entry.name}
          data-stderr={entry.stderr ? "true" : "false"}
          data-testid="docker-log-row"
          key={entry.seq}
        >
          {entry.line}
        </div>
      ))}
    </div>
  ),
}));

const record = (name: string, ts: string, line: string, stderr = false) =>
  JSON.stringify({ id: "abc123def456", line, name, stderr, ts });

const rows = () =>
  screen
    .getAllByTestId("docker-log-row")
    .map((node) => `${node.dataset.name}:${node.textContent}`);

describe("DockerLogsPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.streamRef.current = null;
    mocks.streamOptions = null;
  });

  it("opens the merged stream and renders records in timestamp order", async () => {
    const onContainerChange = vi.fn();
    render(<DockerLogsPage onContainerChange={onContainerChange} />);

    await waitFor(() => expect(mocks.openStream).toHaveBeenCalledTimes(1));
    expect(mocks.openChannel).toHaveBeenCalledWith("docker.logs.follow_all", {
      tail: "100",
    });

    act(() => {
      mocks.streamOptions?.onText(
        [
          record("web", "2026-10-05T10:00:03Z", "web three"),
          record("db", "2026-10-05T10:00:01Z", "db one"),
          record("db", "2026-10-05T10:00:02Z", "db two", true),
        ].join("\n") + "\n",
      );
    });

    expect(rows()).toEqual(["db:db one", "db:db two", "web:web three"]);
    expect(
      screen.getAllByTestId("docker-log-row")[1].dataset.stderr,
    ).toBe("true");
    expect(screen.getByText("3 shown")).toBeInTheDocument();
  });

  it("filters by container and by search text", async () => {
    const { rerender } = render(
      <DockerLogsPage onContainerChange={vi.fn()} />,
    );
    await waitFor(() => expect(mocks.openStream).toHaveBeenCalledTimes(1));
    act(() => {
      mocks.streamOptions?.onText(
        [
          record("web", "2026-10-05T10:00:01Z", "GET /index"),
          record("db", "2026-10-05T10:00:02Z", "ready"),
          record("web", "2026-10-05T10:00:03Z", "GET /about"),
        ].join("\n") + "\n",
      );
    });

    rerender(<DockerLogsPage container="web" onContainerChange={vi.fn()} />);
    expect(rows()).toEqual(["web:GET /index", "web:GET /about"]);

    fireEvent.change(screen.getByPlaceholderText("Search logs…"), {
      target: { value: "about" },
    });
    expect(rows()).toEqual(["web:GET /about"]);
  });

  it("lists running containers in the filter and reports the selection", async () => {
    const onContainerChange = vi.fn();
    render(<DockerLogsPage onContainerChange={onContainerChange} />);
    const select = await screen.findByLabelText("Container");
    await waitFor(() =>
      expect(select.querySelectorAll("option")).toHaveLength(3),
    );
    fireEvent.change(select, { target: { value: "db" } });
    expect(onContainerChange).toHaveBeenCalledWith("db");
  });

  it("reopens with the new tail when the lines select changes and with tail 0 when live resumes", async () => {
    render(<DockerLogsPage onContainerChange={vi.fn()} />);
    await waitFor(() => expect(mocks.openStream).toHaveBeenCalledTimes(1));
    act(() => {
      mocks.streamOptions?.onText(
        record("web", "2026-10-05T10:00:01Z", "first") + "\n",
      );
    });

    fireEvent.change(screen.getByLabelText("Lines"), {
      target: { value: "500" },
    });
    await waitFor(() => expect(mocks.openStream).toHaveBeenCalledTimes(2));
    expect(mocks.closeStream).toHaveBeenCalled();
    expect(mocks.openChannel).toHaveBeenLastCalledWith(
      "docker.logs.follow_all",
      { tail: "500" },
    );
    expect(screen.queryAllByTestId("docker-log-row")).toHaveLength(0);

    act(() => {
      mocks.streamOptions?.onText(
        record("web", "2026-10-05T10:00:02Z", "second") + "\n",
      );
    });
    const live = screen.getByRole("checkbox", { name: "Live" });
    fireEvent.click(live);
    expect(mocks.closeStream).toHaveBeenCalledTimes(2);
    expect(rows()).toEqual(["web:second"]);

    fireEvent.click(live);
    await waitFor(() => expect(mocks.openStream).toHaveBeenCalledTimes(3));
    expect(mocks.openChannel).toHaveBeenLastCalledWith(
      "docker.logs.follow_all",
      { tail: "0" },
    );
  });
});
```

- [ ] **Step 2: Register the direct consumer in the layering test**

In `apiLayering.test.ts` add to `directConsumers`:

```ts
      [
        "routes/_authenticated/docker/-components/DockerLogsPage.tsx",
        "docker.logs.follow_all",
      ],
```

- [ ] **Step 3: Run the page test to verify it fails**

Run: `make test-frontend-only VITEST_FILE=src/routes/_authenticated/docker/-components/DockerLogsPage.test.tsx`
Expected: FAIL, cannot resolve `./DockerLogsPage`.

- [ ] **Step 4: Write the stylesheet**

`docker-logs.css`:

```css
.docker-logs {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  min-height: 0;
  height: 100%;
}

.docker-logs__toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--app-space-8);
  margin-bottom: var(--app-space-8);
}

/* One hue per container, derived from its name. The lightness flips with the
   colour scheme so the soft chip stays readable on both surfaces. */
.app-chip.docker-logs__source {
  --app-chip-color: oklch(48% 0.14 var(--docker-log-hue));
  cursor: pointer;
}

:root[data-app-color-scheme="dark"] .app-chip.docker-logs__source {
  --app-chip-color: oklch(78% 0.14 var(--docker-log-hue));
}

.docker-logs__line {
  font-family: var(--app-font-mono);
}

.docker-logs__line--stderr {
  color: var(--app-palette-error-main);
}

.docker-logs__expanded {
  margin: 0;
  padding: var(--app-space-8) var(--app-space-12);
  font-family: var(--app-font-mono);
  font-size: 0.8125rem; /* body2 */
  white-space: pre-wrap;
  word-break: break-all;
}
```

- [ ] **Step 5: Write the page**

```tsx
import { useQuery } from "@tanstack/react-query";
import {
  type CSSProperties,
  type UIEvent,
  useEffect,
  useEffectEvent,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";

import { linuxio, openChannel, useStreamMux } from "@/api";
import PageLoader from "@/components/loaders/PageLoader";
import { RoutedTabSearch } from "@/components/tabbar";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppAlert from "@/components/ui/AppAlert";
import Chip from "@/components/ui/AppChip";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppHeaderSearch from "@/components/ui/AppHeaderSearch";
import AppSelect from "@/components/ui/AppSelect";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { useLiveStream } from "@/hooks/useLiveStream";
import { copyToClipboard } from "@/utils/clipboard";

import {
  type DockerLogEntry,
  dockerLogHue,
  formatDockerLogTime,
  mergeDockerLogEntries,
  parseDockerLogFrame,
} from "./dockerLogs";

import "./docker-logs.css";

const TAIL_OPTIONS = ["100", "500", "1000", "5000"];
const INITIAL_SILENCE_TIMEOUT_MS = 1500;
const BOTTOM_THRESHOLD_PX = 24;

interface DockerLogsPageProps {
  /** Container name filter, owned by the route search params. */
  container?: string;
  onContainerChange: (container: string | undefined) => void;
}

const getRowId = (entry: DockerLogEntry) => String(entry.seq);

const renderExpanded = (row: { original: DockerLogEntry }) => (
  <pre className="docker-logs__expanded">{row.original.line}</pre>
);

const DockerLogsPage = ({ container, onContainerChange }: DockerLogsPageProps) => {
  const [entries, setEntries] = useState<DockerLogEntry[]>([]);
  const [search, setSearch] = useState("");
  const [tail, setTail] = useState("100");
  const [liveMode, setLiveMode] = useState(true);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [streamEpoch, setStreamEpoch] = useState(0);

  const pendingRef = useRef<DockerLogEntry[]>([]);
  const seqRef = useRef(0);
  const flushFrameRef = useRef<number | null>(null);
  const silenceTimerRef = useRef<number | null>(null);
  const hasReceivedDataRef = useRef(false);
  const pinnedToBottomRef = useRef(true);
  const scrollRef = useRef<HTMLDivElement>(null);

  const { streamRef, openStream, closeStream } = useLiveStream();
  const { isOpen: muxIsOpen } = useStreamMux();
  const { data: containers } = useQuery(linuxio.docker.list_containers);

  const clearSilenceTimer = () => {
    if (silenceTimerRef.current !== null) {
      window.clearTimeout(silenceTimerRef.current);
      silenceTimerRef.current = null;
    }
  };

  const flushPending = () => {
    if (flushFrameRef.current !== null) {
      window.cancelAnimationFrame(flushFrameRef.current);
      flushFrameRef.current = null;
    }
    const batch = pendingRef.current;
    if (batch.length === 0) return;
    pendingRef.current = [];
    setEntries((current) => mergeDockerLogEntries(current, batch));
  };

  const handleStreamText = useEffectEvent((text: string) => {
    const batch = parseDockerLogFrame(text, seqRef.current);
    seqRef.current += batch.length;
    if (batch.length === 0) return;
    pendingRef.current = pendingRef.current.concat(batch);
    if (!hasReceivedDataRef.current) {
      hasReceivedDataRef.current = true;
      clearSilenceTimer();
      setIsLoading(false);
      // First frame flushes now so the loader never yields to an empty table.
      flushPending();
      return;
    }
    if (flushFrameRef.current === null) {
      flushFrameRef.current = window.requestAnimationFrame(flushPending);
    }
  });

  const handleStreamResult = useEffectEvent(
    (result: { status: "ok" | "error"; error?: string }) => {
      clearSilenceTimer();
      flushPending();
      setIsLoading(false);
      if (result.status === "error") {
        setError(result.error || "Failed to load logs");
      }
    },
  );

  const handleStreamClose = useEffectEvent(() => {
    clearSilenceTimer();
    flushPending();
    setIsLoading(false);
  });

  const handleStreamOpenError = useEffectEvent(() => {
    clearSilenceTimer();
    queueMicrotask(() => {
      setError("Failed to connect to log stream");
      setIsLoading(false);
    });
  });

  // Effect event so the opening effect does not depend on `tail`; a resumed
  // stream asks for tail 0 because the buffer already holds the history.
  const startStream = useEffectEvent(() => {
    hasReceivedDataRef.current = false;
    setError(null);
    const opened = openStream({
      open: () =>
        openChannel("docker.logs.follow_all", {
          tail: seqRef.current > 0 ? "0" : tail,
        }),
      onOpenError: handleStreamOpenError,
      onText: handleStreamText,
      onResult: handleStreamResult,
      onClose: handleStreamClose,
    });
    if (opened) {
      clearSilenceTimer();
      silenceTimerRef.current = window.setTimeout(() => {
        silenceTimerRef.current = null;
        if (!hasReceivedDataRef.current) setIsLoading(false);
      }, INITIAL_SILENCE_TIMEOUT_MS);
    }
  });

  useEffect(() => {
    if (!muxIsOpen || !liveMode || streamRef.current) return;
    startStream();
    // `streamEpoch` restarts the stream after the lines select cleared the buffer.
    // oxlint-disable-next-line react/exhaustive-effect-dependencies
  }, [muxIsOpen, liveMode, streamEpoch, streamRef]);

  useEffect(
    () => () => {
      clearSilenceTimer();
      if (flushFrameRef.current !== null) {
        window.cancelAnimationFrame(flushFrameRef.current);
      }
    },
    [],
  );

  // Keep the newest line in view while the user is at the bottom.
  useLayoutEffect(() => {
    const el = scrollRef.current;
    if (el && pinnedToBottomRef.current) {
      el.scrollTop = el.scrollHeight;
    }
    // `entries` retriggers the pin after streamed rows render.
    // oxlint-disable-next-line react/exhaustive-effect-dependencies
  }, [entries]);

  const handleScroll = (event: UIEvent<HTMLDivElement>) => {
    const el = event.currentTarget;
    pinnedToBottomRef.current =
      el.scrollHeight - el.scrollTop - el.clientHeight <= BOTTOM_THRESHOLD_PX;
  };

  const handleLiveModeChange = (checked: boolean) => {
    setLiveMode(checked);
    if (!checked) {
      clearSilenceTimer();
      flushPending();
      closeStream();
      setIsLoading(false);
    }
  };

  const handleTailChange = (value: string) => {
    if (value === tail) return;
    clearSilenceTimer();
    closeStream();
    pendingRef.current = [];
    seqRef.current = 0;
    hasReceivedDataRef.current = false;
    pinnedToBottomRef.current = true;
    setEntries([]);
    setError(null);
    setIsLoading(true);
    setLiveMode(true);
    setTail(value);
    setStreamEpoch((epoch) => epoch + 1);
  };

  const runningNames = (containers ?? [])
    .filter((item) => item.State === "running")
    .map((item) => item.Names[0]?.replace(/^\//, "") ?? item.Id.slice(0, 12));
  const containerNames = Array.from(
    new Set([...runningNames, ...entries.map((entry) => entry.name)]),
  ).sort((a, b) => a.localeCompare(b));

  const needle = search.trim().toLowerCase();
  const visible = entries.filter(
    (entry) =>
      (!container || entry.name === container) &&
      (!needle || entry.line.toLowerCase().includes(needle)),
  );

  const columns = useMemo<AppVirtualTableColumnDef<DockerLogEntry>[]>(
    () => [
      {
        id: "time",
        header: "Time",
        enableSorting: false,
        cell: ({ row }) => (
          <AppTypography noWrap title={row.original.ts} variant="body2">
            {formatDockerLogTime(row.original.ts)}
          </AppTypography>
        ),
        meta: {
          getCellRenderKey: (row) => (row as DockerLogEntry).seq,
          hideBelow: "sm",
          width: "96px",
        },
      },
      {
        id: "container",
        header: "Container",
        enableSorting: false,
        cell: ({ row }) => (
          <Chip
            className="docker-logs__source"
            label={row.original.name}
            onClick={(event) => {
              event.stopPropagation();
              onContainerChange(row.original.name);
            }}
            size="xsmall"
            style={
              {
                "--docker-log-hue": dockerLogHue(row.original.name),
              } as CSSProperties
            }
            title={`Show only ${row.original.name} (${row.original.id})`}
            variant="soft"
          />
        ),
        meta: {
          deferTooltipWhileScrolling: true,
          getCellRenderKey: (row) => (row as DockerLogEntry).seq,
          width: "minmax(110px, 200px)",
        },
      },
      {
        id: "line",
        header: "Message",
        enableSorting: false,
        cell: ({ row }) => (
          <AppTypography
            className={
              row.original.stderr
                ? "docker-logs__line docker-logs__line--stderr"
                : "docker-logs__line"
            }
            noWrap
            variant="body2"
          >
            {row.original.line}
          </AppTypography>
        ),
        meta: {
          align: "left",
          deferTooltipWhileScrolling: true,
          getCellRenderKey: (row) => (row as DockerLogEntry).seq,
        },
      },
    ],
    [onContainerChange],
  );

  const exportText = () =>
    visible
      .map(
        (entry) =>
          `${entry.ts || formatDockerLogTime(entry.ts)} ${entry.name} ${entry.line}`,
      )
      .join("\n");

  const handleDownload = () => {
    const blob = new Blob([exportText()], { type: "text/plain" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `${container ?? "docker"}-logs.txt`;
    a.click();
    URL.revokeObjectURL(url);
  };

  const emptyMessage =
    runningNames.length === 0 && entries.length === 0
      ? "No running containers."
      : entries.length === 0
        ? "No log output yet."
        : "No matching logs.";

  return (
    <>
      <RoutedTabSearch active={search !== ""}>
        <AppHeaderSearch
          clearOnDocumentEscape
          onChange={setSearch}
          placeholder="Search logs…"
          value={search}
        />
      </RoutedTabSearch>
      <div className="docker-logs">
        <div className="docker-logs__toolbar">
          <AppSelect
            label="Lines"
            onChange={(event) => handleTailChange(event.target.value)}
            size="small"
            style={{ width: 112 }}
            value={tail}
          >
            {TAIL_OPTIONS.map((option) => (
              <option key={option} value={option}>
                {option}
              </option>
            ))}
          </AppSelect>
          <AppSelect
            label="Container"
            onChange={(event) =>
              onContainerChange(event.target.value || undefined)
            }
            size="small"
            style={{ minWidth: 180 }}
            value={container ?? ""}
          >
            <option value="">All containers</option>
            {containerNames.map((name) => (
              <option key={name} value={name}>
                {name}
              </option>
            ))}
          </AppSelect>
          <AppActionIconButton
            disabled={visible.length === 0}
            icon="mdi:content-copy"
            iconSize={20}
            label="Copy logs"
            onClick={() => void copyToClipboard(exportText())}
          />
          <AppActionIconButton
            disabled={visible.length === 0}
            icon="mdi:download"
            iconSize={20}
            label="Download logs"
            onClick={handleDownload}
          />
          <AppTooltip
            title={liveMode ? "Live streaming ON" : "Live streaming OFF"}
          >
            <AppFormControlLabel
              control={
                <AppSwitch
                  checked={liveMode}
                  onChange={(_, checked) => handleLiveModeChange(checked)}
                  size="small"
                />
              }
              label="Live"
            />
          </AppTooltip>
          <AppTypography fontWeight={700}>{visible.length} shown</AppTypography>
        </div>

        {isLoading && <PageLoader />}
        {error && <AppAlert severity="error">{error}</AppAlert>}
        {!isLoading && !error && (
          <AppVirtualTable
            ariaLabel="Docker logs"
            columns={columns}
            data={visible}
            emptyMessage={emptyMessage}
            fillAvailable
            getRowId={getRowId}
            onScroll={handleScroll}
            renderExpandedContent={renderExpanded}
            scrollElementRef={scrollRef}
          />
        )}
      </div>
    </>
  );
};

export default DockerLogsPage;
```

Notes for the implementer:
- If `AppSelect` does not wire `label` to the `<select>` for `getByLabelText`, check `AppSelect.tsx` and use the same accessible query other tests use for it; do not change the component.
- If `AppSwitch` is not reachable with `getByRole("checkbox", { name: "Live" })`, look at `AppFormControlLabel` tests for the accessible name they use.
- `exportText` puts the raw Docker timestamp first so downloaded logs stay machine-sortable.

- [ ] **Step 6: Run the page test to verify it passes**

Run: `make test-frontend-only VITEST_FILE=src/routes/_authenticated/docker/-components/DockerLogsPage.test.tsx`
Expected: PASS (4 tests).

---

### Task 6: Route file and frontend verification

**Files:**
- Create: `frontend/src/routes/_authenticated/docker/logs.tsx`
- Generated: `frontend/src/routeTree.gen.ts` (router plugin, on the next vitest/vite run)

**Interfaces:**
- Consumes: `DockerLogsPage` (Task 5), `optionalString`, `loadRouteQueries`.

- [ ] **Step 1: Write the route**

```tsx
import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import DockerLogsPage from "./-components/DockerLogsPage";

export const Route = createFileRoute("/_authenticated/docker/logs")({
  validateSearch: (search) => ({
    ...optionalString(search, "container"),
  }),
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [linuxio.docker.list_containers]),
  component: DockerLogsRoute,
});

function DockerLogsRoute() {
  const { container } = Route.useSearch();
  const navigate = Route.useNavigate();
  return (
    <DockerLogsPage
      container={container}
      onContainerChange={(next) =>
        void navigate({
          replace: true,
          resetScroll: false,
          search: next ? { container: next } : {},
        })
      }
    />
  );
}
```

- [ ] **Step 2: Run the frontend checks**

Run: `make check-frontend-quiet`
Expected: PASS. `routeTree.gen.ts` now contains `/_authenticated/docker/logs`; `dockerTabs.ts` type-checks; the styling-boundary and api-layering tests pass. If `routeTree.gen.ts` was not regenerated, run `make test-frontend-only VITEST_FILE=src/routes/_authenticated/docker/-components/DockerLogsPage.test.tsx` once (the vitest config loads the router plugin) and re-run the check.

---

### Task 7: Whole-change verification

- [ ] **Step 1: Full test run**

Run: `make test-quiet`
Expected: PASS. Known flaky on WSL: the UpdateBanner test and the general-logs-scroll browser spec; a failure there that passes alone is pre-existing. Logs are in `.cache/test-logs/`.

- [ ] **Step 2: Review the diff**

```bash
git status --short
git diff --stat
```

Confirm only these paths changed: the two new Go files, `contracts.go`, `handlers.go`, `schema_test.go`, `docs/api-contract.md`, generated frontend contract files, `routeTree.gen.ts`, `dockerTabs.ts`, the three new page files plus their tests, `logs.tsx`, `apiLayering.test.ts`, and the spec/plan docs.

- [ ] **Step 3: Hand off**

Report the Make targets run and their results, and suggest a commit message; do not commit.
