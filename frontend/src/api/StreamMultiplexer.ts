/**
 * StreamMultiplexer - Binary WebSocket stream multiplexer (Singleton)
 *
 * Provides multiplexed bidirectional streams over a single WebSocket connection.
 * The server acts as a pure byte relay - no JSON parsing on the server side.
 * Streams persist across component unmounts for session continuity.
 *
 * Protocol: [streamID:4 bytes][flags:1 byte][payload:N bytes]
 */

// Stream flags
export const Flags = {
  SYN: 0x01, // Open new stream
  DATA: 0x04, // Data frame
  FIN: 0x08, // Close stream
  RST: 0x10, // Abort stream
} as const;

export type StreamStatus = "opening" | "open" | "closing" | "closed";
export type StreamType = "terminal" | "container" | string;

// Forward declare types used in Stream interface (full definitions below)
export interface ProgressFrame {
  bytes: number;
  total: number;
  pct: number;
  phase?: string;
}

export interface ResultFrame {
  status: "ok" | "error";
  error?: string;
  code?: number;
  data?: unknown;
}

export interface Stream {
  readonly id: number;
  readonly type: StreamType;
  readonly status: StreamStatus;
  write(data: Uint8Array): void;
  resize(cols: number, rows: number): void;
  close(): void;
  /** Abort the stream immediately (sends RST flag instead of FIN) */
  abort(): void;
  onData: ((data: Uint8Array) => void) | null;
  onClose: (() => void) | null;
  onProgress: ((progress: ProgressFrame) => void) | null;
  onResult: ((result: ResultFrame) => void) | null;
}

export type MuxStatus = "connecting" | "open" | "closed" | "error";

// Max scrollback to retain (64KB should cover a full screen + some history)
const MAX_SCROLLBACK = 64 * 1024;
// Max buffered bytes while no handler is attached (prevents unbounded memory growth)
const MAX_DETACHED_BUFFER = 4 * 1024 * 1024;

/**
 * Efficient circular buffer - pre-allocated, no reallocations on write.
 * Uses head pointer and length to track valid data.
 */
class CircularBuffer {
  private data: Uint8Array;
  private head = 0; // Start of valid data
  private len = 0; // Length of valid data

  constructor(private capacity: number) {
    this.data = new Uint8Array(capacity);
  }

  get length(): number {
    return this.len;
  }

  /** Append data, overwriting oldest if full */
  append(chunk: Uint8Array): void {
    if (chunk.length >= this.capacity) {
      // Chunk larger than buffer - just keep the end
      this.data.set(chunk.slice(chunk.length - this.capacity));
      this.head = 0;
      this.len = this.capacity;
      return;
    }

    const writePos = (this.head + this.len) % this.capacity;
    const spaceAtEnd = this.capacity - writePos;

    if (chunk.length <= spaceAtEnd) {
      // Fits without wrapping
      this.data.set(chunk, writePos);
    } else {
      // Wrap around
      this.data.set(chunk.slice(0, spaceAtEnd), writePos);
      this.data.set(chunk.slice(spaceAtEnd), 0);
    }

    const newLen = this.len + chunk.length;
    if (newLen > this.capacity) {
      // Overflowed - advance head
      const overflow = newLen - this.capacity;
      this.head = (this.head + overflow) % this.capacity;
      this.len = this.capacity;
    } else {
      this.len = newLen;
    }
  }

  /** Read all data as contiguous array (only allocates on read) */
  read(): Uint8Array {
    if (this.len === 0) return new Uint8Array(0);

    const result = new Uint8Array(this.len);
    const firstPart = Math.min(this.len, this.capacity - this.head);

    result.set(this.data.slice(this.head, this.head + firstPart));
    if (firstPart < this.len) {
      result.set(this.data.slice(0, this.len - firstPart), firstPart);
    }

    return result;
  }
}

class StreamImpl implements Stream {
  private _onData: ((data: Uint8Array) => void) | null = null;
  public onClose: (() => void) | null = null;
  public onProgress: ((progress: ProgressFrame) => void) | null = null;
  public onResult: ((result: ResultFrame) => void) | null = null;
  private _status: StreamStatus = "opening";
  private buffer: Uint8Array[] = []; // Buffer for when handler is detached
  private bufferedBytes = 0;
  private recvBuffer: Uint8Array = new Uint8Array(0); // Buffer for partial StreamFrames
  private scrollback = new CircularBuffer(MAX_SCROLLBACK); // Efficient circular buffer

  constructor(
    public readonly id: number,
    public readonly type: StreamType,
    private mux: StreamMultiplexer,
  ) {}

  get status(): StreamStatus {
    return this._status;
  }

  setStatus(s: StreamStatus) {
    this._status = s;
  }

  /** Set data handler - replays scrollback and flushes buffer when attaching */
  set onData(handler: ((data: Uint8Array) => void) | null) {
    this._onData = handler;

    if (handler) {
      // Replay scrollback, excluding tail bytes that will be delivered from buffer.
      // This avoids duplicate output after detach/reattach.
      if (this.scrollback.length > 0) {
        const scrollback = this.scrollback.read();
        const overlapBytes = Math.min(this.bufferedBytes, scrollback.length);
        const replayLength = scrollback.length - overlapBytes;
        console.log(
          `[Stream ${this.id}] Replaying ${replayLength} bytes of scrollback`,
        );
        if (replayLength > 0) {
          handler(scrollback.slice(0, replayLength));
        }
      }

      // Then flush any buffered data that arrived while detached
      if (this.buffer.length > 0) {
        console.log(
          `[Stream ${this.id}] Flushing ${this.buffer.length} buffered items`,
        );
        for (const data of this.buffer) {
          handler(data);
        }
        this.buffer = [];
        this.bufferedBytes = 0;
      }
    }
  }

  get onData(): ((data: Uint8Array) => void) | null {
    return this._onData;
  }

  write(data: Uint8Array): void {
    if (this._status !== "open" && this._status !== "opening") {
      console.warn(
        `[Stream ${this.id}] Cannot write - status: ${this._status}`,
      );
      return;
    }
    // Wrap in StreamFrame for bridge: [opcode:1][streamID:4][length:4][payload]
    const bridgeFrame = new Uint8Array(9 + data.length);
    const view = new DataView(bridgeFrame.buffer);
    bridgeFrame[0] = 0x81; // OpStreamData
    view.setUint32(1, this.id, false);
    view.setUint32(5, data.length, false);
    bridgeFrame.set(data, 9);
    this.mux.sendFrame(this.id, Flags.DATA, bridgeFrame);
  }

  resize(cols: number, rows: number): void {
    if (this._status !== "open" && this._status !== "opening") {
      return;
    }
    const safeCols = Math.max(0, Math.min(cols, 0xffff));
    const safeRows = Math.max(0, Math.min(rows, 0xffff));
    const payload = new Uint8Array(4);
    const payloadView = new DataView(payload.buffer);
    payloadView.setUint16(0, safeCols, false);
    payloadView.setUint16(2, safeRows, false);

    const bridgeFrame = new Uint8Array(9 + payload.length);
    const view = new DataView(bridgeFrame.buffer);
    bridgeFrame[0] = BridgeOpcode.StreamResize;
    view.setUint32(1, this.id, false);
    view.setUint32(5, payload.length, false);
    bridgeFrame.set(payload, 9);
    this.mux.sendFrame(this.id, Flags.DATA, bridgeFrame);
  }

  close(): void {
    if (this._status === "closed" || this._status === "closing") {
      return;
    }
    this._status = "closing";
    // Build OpStreamClose frame for bridge: [opcode:1][streamID:4][length:4]
    const closeFrame = new Uint8Array(9);
    const view = new DataView(closeFrame.buffer);
    closeFrame[0] = 0x82; // OpStreamClose
    view.setUint32(1, this.id, false);
    view.setUint32(5, 0, false); // length = 0
    this.mux.sendFrame(this.id, Flags.FIN, closeFrame);
    // Don't remove stream or call onClose yet - wait for server's response.
    // The stream will be cleaned up when we receive the server's FIN (in handleMessage).
  }

  /**
   * Abort/cancel the stream immediately.
   * Sends OpStreamAbort frame to signal cancellation to the backend,
   * then sends RST flag to close the transport.
   * Can override a pending close() to force immediate abort.
   */
  abort(): void {
    if (this._status === "closed") {
      return;
    }
    // Always send abort, even if already closing (overrides pending FIN)
    this._status = "closing";

    // Send OpStreamAbort frame to backend: [opcode:1][streamID:4][length:4]
    // This signals the backend's AbortMonitor to cancel the operation
    const abortFrame = new Uint8Array(9);
    const view = new DataView(abortFrame.buffer);
    abortFrame[0] = 0x86; // OpStreamAbort
    view.setUint32(1, this.id, false);
    view.setUint32(5, 0, false); // length = 0
    this.mux.sendFrame(this.id, Flags.DATA, abortFrame);

    // Then send RST to close the transport layer
    this.mux.sendFrame(this.id, Flags.RST, new Uint8Array(0));
  }

  handleData(data: Uint8Array): void {
    // Always save to scrollback for replay on reconnect
    this.scrollback.append(data);

    if (this._onData) {
      this._onData(data);
    } else {
      // Buffer data when no handler attached
      console.log(
        `[Stream ${this.id}] Buffering ${data.length} bytes (no handler)`,
      );
      this.buffer.push(data);
      this.bufferedBytes += data.length;

      while (
        this.bufferedBytes > MAX_DETACHED_BUFFER &&
        this.buffer.length > 0
      ) {
        const dropped = this.buffer.shift();
        if (!dropped) break;
        this.bufferedBytes -= dropped.length;
      }
    }
  }

  /**
   * Handle raw bytes from WebSocket, accumulate in recvBuffer,
   * and parse complete StreamFrames.
   * StreamFrame format: [opcode:1][streamID:4][length:4][payload:N]
   */
  handleRawData(data: Uint8Array): void {
    // Append to receive buffer
    const newBuffer = new Uint8Array(this.recvBuffer.length + data.length);
    newBuffer.set(this.recvBuffer);
    newBuffer.set(data, this.recvBuffer.length);
    this.recvBuffer = newBuffer;

    // Parse complete frames from buffer
    while (this.recvBuffer.length >= 9) {
      const view = new DataView(
        this.recvBuffer.buffer,
        this.recvBuffer.byteOffset,
        this.recvBuffer.byteLength,
      );
      const opcode = this.recvBuffer[0];
      const payloadLength = view.getUint32(5, false); // Big endian
      const frameLength = 9 + payloadLength;

      if (this.recvBuffer.length < frameLength) {
        // Incomplete frame, wait for more data
        break;
      }

      // Extract payload
      const payload = this.recvBuffer.slice(9, frameLength);

      // Route based on opcode
      switch (opcode) {
        case 0x81: // OpStreamData
          this.handleData(payload);
          break;
        case 0x82: // OpStreamClose
          // Bridge closed the stream - trigger close handler
          this.handleClose();
          this.mux.removeStream(this.id);
          break;
        case 0x84: // OpStreamProgress
          this.handleProgress(payload);
          break;
        case 0x85: // OpStreamResult
          this.handleResult(payload);
          break;
        default:
          console.warn(
            `[Stream ${this.id}] Unknown opcode: 0x${opcode.toString(16)}`,
          );
      }

      // Remove processed frame from buffer
      this.recvBuffer = this.recvBuffer.slice(frameLength);
    }
  }

  /** Handle progress frame from bridge */
  private handleProgress(payload: Uint8Array): void {
    if (!this.onProgress) return;
    try {
      const json = decodeString(payload);
      const progress: ProgressFrame = JSON.parse(json);
      this.onProgress(progress);
    } catch (e) {
      console.error(`[Stream ${this.id}] Failed to parse progress:`, e);
    }
  }

  /** Handle result frame from bridge */
  private handleResult(payload: Uint8Array): void {
    if (!this.onResult) return;
    try {
      const json = decodeString(payload);
      const result: ResultFrame = JSON.parse(json);
      this.onResult(result);
    } catch (e) {
      console.error(`[Stream ${this.id}] Failed to parse result:`, e);
    }
  }

  handleClose(): void {
    this._status = "closed";
    this.onClose?.();
  }
}

export class StreamMultiplexer {
  private ws: WebSocket | null = null;
  private streams = new Map<number, StreamImpl>();
  private streamsByType = new Map<StreamType, StreamImpl>();
  private nextStreamID = 1; // Client uses odd numbers
  private _status: MuxStatus = "connecting";
  private _isUpdating = false; // Flag to pause all API requests during system update
  private url: string;
  private statusListeners = new Set<(status: MuxStatus) => void>();
  private updatingListeners = new Set<(isUpdating: boolean) => void>();

  constructor(url: string) {
    this.url = url;
    this.connect();
  }

  private connect(): void {
    if (this.ws?.readyState === WebSocket.OPEN) return;

    this.ws = new WebSocket(this.url);
    this.ws.binaryType = "arraybuffer";

    this.ws.onopen = () => {
      this._status = "open";
      this.notifyStatusChange("open");
    };

    this.ws.onclose = (event: CloseEvent) => {
      console.log(
        `[StreamMultiplexer] WebSocket closed: code=${event.code}, reason="${event.reason}"`,
      );

      // Close code 1008 = Policy Violation (session expired)
      // This means the backend terminated the session - user must re-authenticate
      if (event.code === 1008) {
        this._status = "error";
        this.notifyStatusChange("error");
        this.closeAllStreams();
      } else {
        // Network error or normal closure - mark as closed
        // Frontend can decide whether to show reconnect UI
        this._status = "closed";
        this.notifyStatusChange("closed");
        this.closeAllStreams();
      }
    };

    this.ws.onerror = () => {
      this._status = "error";
      this.notifyStatusChange("error");
    };

    this.ws.onmessage = (event) => {
      if (event.data instanceof ArrayBuffer) {
        this.handleMessage(event.data);
      }
    };
  }

  private notifyStatusChange(status: MuxStatus): void {
    for (const listener of this.statusListeners) {
      listener(status);
    }
  }

  private notifyUpdatingChange(isUpdating: boolean): void {
    for (const listener of this.updatingListeners) {
      listener(isUpdating);
    }
  }

  /** Subscribe to status changes */
  addStatusListener(listener: (status: MuxStatus) => void): () => void {
    this.statusListeners.add(listener);
    return () => this.statusListeners.delete(listener);
  }

  /** Subscribe to updating flag changes */
  addUpdatingListener(listener: (isUpdating: boolean) => void): () => void {
    this.updatingListeners.add(listener);
    return () => this.updatingListeners.delete(listener);
  }

  get status(): MuxStatus {
    return this._status;
  }

  get isUpdating(): boolean {
    return this._isUpdating;
  }

  /**
   * Set update-in-progress flag to pause/resume all API requests.
   * When true, all React Query hooks will be disabled.
   */
  setUpdating(value: boolean): void {
    if (this._isUpdating !== value) {
      this._isUpdating = value;
      this.notifyUpdatingChange(value);
    }
  }

  /**
   * Get existing stream by type, or null if none exists
   */
  getStream(type: StreamType): Stream | null {
    const stream = this.streamsByType.get(type);
    if (stream && stream.status === "open") {
      return stream;
    }
    return null;
  }

  // Stream types that should be cached and reused (persistent streams)
  private static readonly PERSISTENT_STREAM_TYPES = new Set(["terminal"]);

  /**
   * Open a new stream and send initial payload.
   * The payload is wrapped in a StreamFrame for the bridge.
   */
  openStream(type: StreamType, initialPayload?: Uint8Array): Stream {
    // Only reuse persistent streams (terminal) - not request/response streams
    const isPersistent = StreamMultiplexer.PERSISTENT_STREAM_TYPES.has(type);
    if (isPersistent) {
      const existing = this.streamsByType.get(type);
      if (existing && existing.status === "open") {
        console.log(`[StreamMux] Reusing persistent stream "${type}"`);
        return existing;
      }
    }

    const id = this.nextStreamID;
    this.nextStreamID += 2; // Keep odd

    const stream = new StreamImpl(id, type, this);
    this.streams.set(id, stream);

    // Only cache persistent streams
    if (isPersistent) {
      this.streamsByType.set(type, stream);
    }

    // Wrap initial payload in StreamFrame for bridge: [opcode:1][streamID:4][length:4][payload]
    const payload = initialPayload || new Uint8Array(0);
    const bridgeFrame = new Uint8Array(9 + payload.length);
    const view = new DataView(bridgeFrame.buffer);
    bridgeFrame[0] = 0x80; // OpStreamOpen
    view.setUint32(1, id, false);
    view.setUint32(5, payload.length, false);
    bridgeFrame.set(payload, 9);

    // Send SYN with the StreamFrame as payload
    const sent = this.sendFrame(id, Flags.SYN, bridgeFrame);
    if (!sent) {
      // Fail fast: keep API behavior deterministic when transport is not available.
      this.streams.delete(id);
      if (isPersistent && this.streamsByType.get(type) === stream) {
        this.streamsByType.delete(type);
      }
      stream.setStatus("closed");
      queueMicrotask(() => {
        stream.handleClose();
      });
      return stream;
    }

    // Mark as open immediately (server-side stream is created on SYN)
    stream.setStatus("open");

    return stream;
  }

  /**
   * Send a frame to the WebSocket
   */
  sendFrame(streamID: number, flags: number, payload: Uint8Array): boolean {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      console.warn("[StreamMux] Cannot send - WebSocket not open");
      return false;
    }

    const frame = new Uint8Array(5 + payload.length);
    const view = new DataView(frame.buffer);
    view.setUint32(0, streamID, false); // Big endian
    frame[4] = flags;
    frame.set(payload, 5);

    this.ws.send(frame);
    return true;
  }

  /**
   * Handle incoming WebSocket message
   */
  private handleMessage(data: ArrayBuffer): void {
    if (data.byteLength < 5) {
      console.warn("[StreamMux] Frame too short:", data.byteLength);
      return;
    }

    const view = new DataView(data);
    const streamID = view.getUint32(0, false); // Big endian
    const flags = view.getUint8(4);
    const wsPayload = new Uint8Array(data, 5);

    const stream = this.streams.get(streamID);

    if (flags & Flags.DATA) {
      if (stream) {
        // wsPayload contains raw bytes from yamux stream (may be partial StreamFrames)
        // Use buffered parsing to handle split frames
        stream.handleRawData(wsPayload);
      } else {
        console.warn(`[StreamMux] DATA for unknown stream ${streamID}`);
      }
    }

    if (flags & Flags.FIN) {
      if (stream) {
        stream.handleClose();
        this.streams.delete(streamID);
        if (this.streamsByType.get(stream.type) === stream) {
          this.streamsByType.delete(stream.type);
        }
      }
    }

    if (flags & Flags.RST) {
      if (stream) {
        stream.handleClose();
        this.streams.delete(streamID);
        if (this.streamsByType.get(stream.type) === stream) {
          this.streamsByType.delete(stream.type);
        }
      }
    }
  }

  removeStream(id: number): void {
    const stream = this.streams.get(id);
    if (stream && this.streamsByType.get(stream.type) === stream) {
      this.streamsByType.delete(stream.type);
    }
    this.streams.delete(id);
  }

  private closeAllStreams(): void {
    for (const stream of this.streams.values()) {
      stream.handleClose();
    }
    this.streams.clear();
    this.streamsByType.clear();
  }

  close(): void {
    this.closeAllStreams();
    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
    this._status = "closed";
  }
}

// Utility to encode string to Uint8Array
export function encodeString(str: string): Uint8Array {
  return new TextEncoder().encode(str);
}

// Utility to decode Uint8Array to string
export function decodeString(data: Uint8Array): string {
  return new TextDecoder().decode(data);
}

// Bridge StreamFrame opcodes (must match backend ipc/stream_relay.go)
export const BridgeOpcode = {
  StreamOpen: 0x80,
  StreamData: 0x81,
  StreamClose: 0x82,
  StreamResize: 0x83,
  StreamProgress: 0x84,
  StreamResult: 0x85,
  StreamAbort: 0x86,
} as const;

// File transfer constants (must match backend bridge/handlers/filebrowser/stream.go)
export const STREAM_CHUNK_SIZE = 1 * 1024 * 1024; // 1MB chunks

// Flow control: max bytes in flight before waiting for ACK (progress update)
export const UPLOAD_WINDOW_SIZE = 4 * 1024 * 1024; // 4MB window (4 chunks max in flight)

// ============================================================================
// Singleton Management
// ============================================================================

let instance: StreamMultiplexer | null = null;

/**
 * Get or create the singleton StreamMultiplexer instance.
 * Call this after authentication is confirmed.
 */
export function getStreamMux(): StreamMultiplexer | null {
  return instance;
}

/**
 * Initialize the singleton StreamMultiplexer.
 * Should be called once after successful authentication.
 */
export function initStreamMux(): StreamMultiplexer {
  if (instance && instance.status !== "closed") {
    return instance;
  }

  const proto = window.location.protocol === "https:" ? "wss" : "ws";
  const url = `${proto}://${window.location.host}/ws`;

  instance = new StreamMultiplexer(url);
  return instance;
}

/**
 * Close and destroy the singleton StreamMultiplexer.
 * Call this on logout.
 */
export function closeStreamMux(): void {
  if (instance) {
    instance.close();
    instance = null;
  }
}

/**
 * Wait for the stream multiplexer to be ready (status === "open").
 * Returns immediately if already open, or waits up to timeoutMs.
 * @param timeoutMs Maximum time to wait (default 10 seconds)
 * @returns Promise that resolves to true if ready, false if timeout/error
 */
export function waitForStreamMux(timeoutMs = 10000): Promise<boolean> {
  return new Promise((resolve) => {
    const mux = instance;
    if (!mux) {
      resolve(false);
      return;
    }

    if (mux.status === "open") {
      resolve(true);
      return;
    }

    if (mux.status === "closed" || mux.status === "error") {
      resolve(false);
      return;
    }

    // Wait for status change
    const timeout = setTimeout(() => {
      cleanup();
      resolve(false);
    }, timeoutMs);

    const cleanup = mux.addStatusListener((status) => {
      if (status === "open") {
        clearTimeout(timeout);
        cleanup();
        resolve(true);
      } else if (status === "closed" || status === "error") {
        clearTimeout(timeout);
        cleanup();
        resolve(false);
      }
    });
  });
}
