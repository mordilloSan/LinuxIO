# Stream Event API Plan

## Problem

Frontend stream consumers currently attach handlers by assigning callback
properties on `Stream`:

```ts
stream.onData = handleData;
stream.onResult = handleResult;
stream.onClose = handleClose;
```

This works, but it makes ownership unclear. A later assignment replaces the
previous handler, cleanup requires resetting the same properties to `null`, and
adapter code such as the noVNC channel has to take over stream callbacks rather
than subscribe to them.

The stream implementation already owns the important behavior: detached data is
buffered and replayed when a data handler attaches. The missing piece is a
cleaner subscription API around that behavior.

## Goals

- Add event-style subscriptions to LinuxIO streams.
- Keep existing `onData`, `onProgress`, `onResult`, and `onClose` properties
  during migration.
- Preserve detached-data buffering and replay semantics.
- Allow multiple observers without overwriting each other.
- Make stream adapters use subscriptions instead of owning callback slots.
- Avoid changing the bridge protocol or backend. OR BE MOVE AGRESSIVE!!!!!
CVNV CODE FEELS MORE STRUCTURED


## Non-Goals

- Do not make `Stream` pretend to be a browser `WebSocket`.
- Do not remove the existing callback properties in the first pass.
- Do not rewrite every stream consumer at once.
- Do not change stream frame formats or mux behavior.

## Proposed API

Add an `on` method to `Stream`:

```ts
type StreamEventMap = {
  close: void;
  data: Uint8Array;
  progress: ProgressFrame;
  result: ResultFrame;
};

type StreamUnsubscribe = () => void;

interface Stream {
  on<K extends keyof StreamEventMap>(
    event: K,
    handler: (value: StreamEventMap[K]) => void,
  ): StreamUnsubscribe;
}
```

Usage:

```ts
const offData = stream.on("data", handleData);
const offResult = stream.on("result", handleResult);
const offClose = stream.on("close", handleClose);

offData();
offResult();
offClose();
```

The existing callback properties remain valid:

```ts
stream.onData = handleData;
stream.onResult = handleResult;
stream.onClose = handleClose;
```

## Implementation Plan

### 1. Add Event Types

In `frontend/src/api/StreamMultiplexer.ts`, add:

- `StreamEventMap`
- `StreamEventName`
- `StreamUnsubscribe`

Export these through `frontend/src/api/index.ts` via the existing
`export type * from "./StreamMultiplexer"` path.

### 2. Extend the Stream Interface

Add the typed `on(...)` method to `Stream`.

The callback property fields stay in place:

- `onData`
- `onProgress`
- `onResult`
- `onClose`

This keeps the change additive and avoids a big-bang migration.

### 3. Store Listener Sets in StreamImpl

Add listener sets inside `StreamImpl`:

```ts
private readonly dataListeners = new Set<(data: Uint8Array) => void>();
private readonly progressListeners = new Set<(progress: ProgressFrame) => void>();
private readonly resultListeners = new Set<(result: ResultFrame) => void>();
private readonly closeListeners = new Set<() => void>();
```

Implement `on(...)` by adding to the relevant set and returning an unsubscribe
function.

When adding a `"data"` listener, replay scrollback and detached buffered data in
the same way the current `onData` setter does.

### 4. Preserve Callback Property Behavior

Keep the existing property setters and getters. Internally, callback properties
can remain separate from listener sets:

- `_onData`
- `onProgress`
- `onResult`
- `onClose`

Dispatch should call both:

```ts
this._onData?.(data);
for (const listener of this.dataListeners) {
  listener(data);
}
```

This avoids changing existing behavior while allowing new consumers to use
subscriptions.

### 5. Centralize Data Replay

Extract the existing attach replay logic into a helper:

```ts
private replayBufferedData(handler: (data: Uint8Array) => void): void
```

Use it from:

- `set onData(...)`
- `on("data", ...)`

This keeps buffering behavior consistent.

### 6. Update Existing Helpers

Update `bindStreamHandlers` to use subscriptions internally:

```ts
const off = [
  handlers.onData ? stream.on("data", handlers.onData) : null,
  handlers.onProgress ? stream.on("progress", handlers.onProgress) : null,
  handlers.onResult ? stream.on("result", handlers.onResult) : null,
  handlers.onClose ? stream.on("close", handlers.onClose) : null,
].filter(Boolean);

return () => off.forEach((unsubscribe) => unsubscribe());
```

This lets older call sites keep using `bindStreamHandlers`, while the helper
stops mutating global handler properties.

### 7. Update Stream Channel Adapter

Move any WebSocket-like channel adapter to the API layer, for example:

```ts
createStreamMessageChannel(stream, { onResult })
```

Then implement it with subscriptions:

```ts
const offData = stream.on("data", (data) => this.handleData(data));
const offClose = stream.on("close", () => this.markClosed());
const offResult = stream.on("result", (result) => options.onResult?.(result));
```

The adapter should still expose WebSocket-like fields required by noVNC:

- `binaryType`
- `onmessage`
- `onopen`
- `onclose`
- `onerror`
- `protocol`
- `readyState`
- `send(...)`
- `close()`

This keeps browser-channel semantics at the adapter boundary, not in the core
`Stream` type.

### 8. Migrate Call Sites Gradually

High-value first migrations:

- `useLiveStream`
- `Terminal.tsx`
- filebrowser upload/download stream handlers
- VM console stream channel adapter

Lower-risk later migrations can continue to use `bindStreamHandlers` until the
new API is proven stable.

## Testing Plan

Add or update tests for:

- `stream.on("data", handler)` receives live data.
- `stream.on("data", handler)` replays detached buffered data.
- Multiple data listeners can observe the same stream.
- Unsubscribe stops future delivery.
- Callback property handlers and subscription handlers can coexist.
- `bindStreamHandlers` cleans up subscriptions.
- The message-channel adapter does not overwrite existing stream listeners.
- noVNC-style early data buffering still preserves order.

## Rollout

1. Add subscription API and tests.
2. Convert `bindStreamHandlers` to subscriptions.
3. Convert the stream message-channel adapter.
4. Convert the most active stream consumers.
5. Leave callback properties as compatibility surface.
6. After several changesets, decide whether to deprecate direct property
   assignment.

## Risks

- Replaying buffered data for both `onData` and `on("data")` can duplicate data
  if one consumer attaches through each API. This is acceptable during
  migration only if consumers avoid mixing APIs on the same stream.
- Persistent streams such as `terminal.open` need careful testing because
  scrollback replay is user-visible.
- Some stream consumers rely on single-owner semantics today. Multiple
  listeners can reveal assumptions about cleanup order.

## Preferred End State

New code should use:

```ts
const unsubscribe = stream.on("data", handleData);
```

Adapters should subscribe to streams rather than take over callback slots.
Callback properties remain as compatibility until the codebase no longer needs
them.
