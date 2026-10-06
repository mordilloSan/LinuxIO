# Docker global logs tab

Date: 2026-10-05. Status: approved in chat, implementing.

## Goal

A "Logs" tab under Docker that shows the merged, live log output of every
running container in one page, in the spirit of Dozzle's merged view. Each
line carries a coloured container badge and a timestamp. New or restarted
containers join the view while it is open. The existing per-container log
dialog on the Containers tab is unchanged.

## Backend

One new duplex Channel route in `backend/bridge/handlers/docker`:

- Route: `docker.logs.follow_all`, request `DockerLogsFollowAllRequest
  { tail?: string }` (per-container tail, default `100`), response
  `NoResponse`, registered next to `docker.logs.follow`.
- The handler lists running containers and starts one follower goroutine per
  container. Each follower calls `ContainerLogs` with `Timestamps: true,
  Follow: true, Tail: req.Tail`, reads the multiplexed frames (or raw bytes
  for TTY containers) with the existing frame readers, splits the output into
  whole lines with a carry-over for partial lines, strips ANSI with the
  existing regex, and writes one NDJSON record per line to the stream with
  `relay.WriteRelayFrame`. Every demuxer write (one Docker log message for
  multiplexed output, one read chunk for TTY output) becomes one frame. All
  followers share one mutex-guarded writer: go-yamux stream writes are not
  atomic under backpressure, so unsynchronised writers could interleave the
  bytes of two frames.
- Record shape: `{"id":"<12-char id>","name":"<name without slash>",
  "ts":"<Docker RFC3339Nano prefix>","line":"<text>","stderr":true}` with
  `stderr` omitted when false.
- The handler subscribes to Docker events filtered to `type=container,
  event=start` before listing, and starts a follower for every container
  that starts afterwards, so restarts and new containers appear. Such a
  follower asks for everything since the container's `State.StartedAt`: the
  daemon emits the start event after the process may already have written
  output, and a restarted container must not replay its previous run. A start
  event for a listed container whose timestamp predates the list result is
  skipped, so a container that started between subscribing and listing is not
  followed twice. A follower whose log stream ends (container died) exits
  quietly. An error in one follower is logged at debug level and does not end
  the Channel.
- Cancelling the Channel context stops the events loop and every follower;
  the handler waits for them before closing the stream. Listing containers or
  obtaining the Docker client failing ends the Channel with an error result.

Contract and docs: add the request type to `apischema/contracts.go`, run
`make generate`, and add a row to `docs/api-contract.md`.

## Frontend

- `dockerTabs.ts`: add `{ label: "Logs", to: "/docker/logs" }` after
  Containers.
- New route `routes/_authenticated/docker/logs.tsx`: validated optional
  `container` search param (container name), loader loads
  `linuxio.docker.list_containers` for names and the filter options.
- New `-components/DockerLogsPage.tsx` with `docker-logs.css`:
  - Toolbar: per-container lines `AppSelect` (100/500/1000/5000), container
    `AppSelect` (All containers or one name, bound to `?container=`), Live
    `AppSwitch`, copy and download `AppActionIconButton`s, shown count. Text
    search renders in the tab bar through `RoutedTabSearch`.
  - Stream: `useLiveStream` opening `openChannel("docker.logs.follow_all",
    { tail })`. Frames are parsed as NDJSON into entries with a monotonic
    `seq` id. Entries are staged and flushed on an animation frame; the stage
    itself is capped at 5000 entries so a background tab (paused animation
    frames) cannot grow it without bound. Each flushed batch is sorted by `ts`
    before appending and the buffer is capped at 5000 entries, dropping the
    oldest. Live off closes the stream and keeps
    the buffer; live on reopens with tail `0`. Changing the lines select
    clears the buffer and reopens.
  - Table: `AppVirtualTable` with columns time (formatted locally, hidden
    below `sm`), container chip, message (monospace, single line truncated,
    expand on click to show the full line). Newest at the bottom; the view
    stays pinned to the bottom while the user is at the bottom.
  - Container colour: a deterministic hue from the container name set as a
    `--docker-log-hue` CSS variable on the chip; the CSS resolves
    `--app-chip-color` with `oklch(...)` for light and dark through
    `light-dark()`. No new palette tokens and no inline colours. Clicking a
    chip sets the container filter.
  - Stderr lines get an error tint from `--app-palette-error-main`.
  - States: loader until the first frame or 1.5 s of silence; "No running
    containers" when the container list has none running; stream error in an
    `AppAlert`.

## Tests

- Go: a test that feeds fake multiplexed frames (with timestamps, a partial
  line across two frames, stderr and ANSI) through the follower reader and
  asserts the NDJSON records written to the stream; a TTY raw variant.
- Frontend: unit tests for the NDJSON parser and merge helper; a page render
  test with a mocked stream covering filter, search and the stderr class.
- Finish with `make generate` and `make test-quiet`.

## Out of scope for this change

Regex search, log level detection beyond stderr, JSON pretty printing,
history for stopped containers, reconnect on dropped stream, multi-select
container filter, deep links from container rows.
