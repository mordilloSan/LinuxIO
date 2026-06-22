import type { ResultFrame, Stream } from "./StreamMultiplexer";

type ChannelReadyState = "connecting" | "open" | "closing" | "closed";
type ChannelEventName = "open" | "message" | "close" | "error";
type ChannelEventListener = (event: Event) => void;
type ChannelMessageHandler = (event: StreamMessageChannelMessageEvent) => void;

export type StreamMessageChannelOptions = {
  onResult?: (result: ResultFrame) => void;
};

export type StreamMessageChannelMessageEvent = Event & {
  data: ArrayBuffer;
};

/**
 * Presents a LinuxIO Stream as a WebSocket/RTCDataChannel-like message channel.
 *
 * Some consumers, such as noVNC, accept raw channels but require message-event
 * semantics instead of LinuxIO's onData callback shape.
 */
export class StreamMessageChannel {
  binaryType = "arraybuffer" as const;
  onclose: ((event: Event) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  onopen: ((event: Event) => void) | null = null;
  protocol = "";
  readyState: ChannelReadyState;

  private _onmessage: ChannelMessageHandler | null = null;
  private readonly pendingMessages: StreamMessageChannelMessageEvent[] = [];
  private flushScheduled = false;

  private readonly listeners = new Map<
    ChannelEventName,
    Set<ChannelEventListener>
  >();

  constructor(
    private readonly stream: Stream,
    options: StreamMessageChannelOptions = {},
  ) {
    this.readyState = stream.status === "closed" ? "closed" : "open";
    stream.onData = (data) => this.handleData(data);
    stream.onClose = () => this.markClosed();
    stream.onResult = options.onResult ?? null;
  }

  get onmessage(): ChannelMessageHandler | null {
    return this._onmessage;
  }

  set onmessage(handler: ChannelMessageHandler | null) {
    this._onmessage = handler;
    this.scheduleFlush();
  }

  addEventListener(
    type: ChannelEventName,
    listener: ChannelEventListener,
  ): void {
    const listeners =
      this.listeners.get(type) ?? new Set<ChannelEventListener>();
    listeners.add(listener);
    this.listeners.set(type, listeners);
    if (type === "message") {
      this.scheduleFlush();
    }
  }

  removeEventListener(
    type: ChannelEventName,
    listener: ChannelEventListener,
  ): void {
    this.listeners.get(type)?.delete(listener);
  }

  send(data: ArrayBuffer | ArrayBufferView | string): void {
    if (this.readyState !== "open") {
      this.dispatchSimple("error");
      return;
    }
    if (typeof data === "string") {
      this.stream.write(new TextEncoder().encode(data));
      return;
    }
    if (data instanceof ArrayBuffer) {
      this.stream.write(new Uint8Array(data));
      return;
    }
    this.stream.write(
      new Uint8Array(data.buffer, data.byteOffset, data.byteLength),
    );
  }

  close(): void {
    if (this.readyState === "closed" || this.readyState === "closing") {
      return;
    }
    this.readyState = "closing";
    this.stream.onResult = null;
    this.stream.close();
  }

  private handleData(data: Uint8Array): void {
    const payload = new ArrayBuffer(data.byteLength);
    new Uint8Array(payload).set(data);
    const event = new Event("message") as StreamMessageChannelMessageEvent;
    event.data = payload;

    if (this.pendingMessages.length > 0 || !this.hasMessageConsumer()) {
      this.pendingMessages.push(event);
      this.scheduleFlush();
      return;
    }
    this.deliverMessage(event);
  }

  private hasMessageConsumer(): boolean {
    return (
      this._onmessage !== null || (this.listeners.get("message")?.size ?? 0) > 0
    );
  }

  // Replay queued messages on a microtask so consumers finish synchronous
  // setup after assigning onmessage before receiving already-buffered bytes.
  private scheduleFlush(): void {
    if (
      this.flushScheduled ||
      this.pendingMessages.length === 0 ||
      !this.hasMessageConsumer()
    ) {
      return;
    }
    this.flushScheduled = true;
    queueMicrotask(() => {
      this.flushScheduled = false;
      this.flushPending();
    });
  }

  private flushPending(): void {
    while (this.hasMessageConsumer() && this.pendingMessages.length > 0) {
      const event = this.pendingMessages.shift();
      if (!event) break;
      this.deliverMessage(event);
    }
  }

  private deliverMessage(event: StreamMessageChannelMessageEvent): void {
    this._onmessage?.(event);
    this.dispatch("message", event);
  }

  private markClosed(): void {
    if (this.readyState === "closed") {
      return;
    }
    this.flushPending();
    this.readyState = "closed";
    this.dispatchSimple("close");
  }

  private dispatchSimple(type: Exclude<ChannelEventName, "message">): void {
    const event = new Event(type);
    if (type === "open") this.onopen?.(event);
    if (type === "close") this.onclose?.(event);
    if (type === "error") this.onerror?.(event);
    this.dispatch(type, event);
  }

  private dispatch(type: ChannelEventName, event: Event): void {
    for (const listener of this.listeners.get(type) ?? []) {
      listener(event);
    }
  }
}

export const createStreamMessageChannel = (
  stream: Stream,
  options: StreamMessageChannelOptions = {},
): StreamMessageChannel => new StreamMessageChannel(stream, options);
