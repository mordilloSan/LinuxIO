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

export interface StreamMultiplexerConfig {
  scrollbackBytes: number;
  detachedBufferBytes: number;
  uploadChunkSize: number;
  uploadWindowChunks: number;
  defaultCallTimeoutMs: number;
}

function readPositiveInt(
  rawValue: string | undefined,
  fallback: number,
): number {
  if (!rawValue) return fallback;
  const parsed = Number.parseInt(rawValue, 10);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}

function normalizePositiveInt(value: number, fallback: number): number {
  return Number.isFinite(value) && value > 0 ? Math.trunc(value) : fallback;
}

export const STREAM_MULTIPLEXER_CONFIG: StreamMultiplexerConfig = {
  scrollbackBytes: readPositiveInt(
    import.meta.env.VITE_STREAM_SCROLLBACK_BYTES,
    64 * 1024,
  ),
  detachedBufferBytes: readPositiveInt(
    import.meta.env.VITE_STREAM_DETACHED_BUFFER_BYTES,
    4 * 1024 * 1024,
  ),
  uploadChunkSize: readPositiveInt(
    import.meta.env.VITE_STREAM_UPLOAD_CHUNK_SIZE,
    1 * 1024 * 1024,
  ),
  uploadWindowChunks: readPositiveInt(
    import.meta.env.VITE_STREAM_UPLOAD_WINDOW_CHUNKS,
    4,
  ),
  defaultCallTimeoutMs: readPositiveInt(
    import.meta.env.VITE_STREAM_DEFAULT_CALL_TIMEOUT_MS,
    30000,
  ),
};

export function configureStreamMultiplexer(
  config: Partial<StreamMultiplexerConfig>,
): void {
  if (config.scrollbackBytes !== undefined) {
    STREAM_MULTIPLEXER_CONFIG.scrollbackBytes = normalizePositiveInt(
      config.scrollbackBytes,
      STREAM_MULTIPLEXER_CONFIG.scrollbackBytes,
    );
  }
  if (config.detachedBufferBytes !== undefined) {
    STREAM_MULTIPLEXER_CONFIG.detachedBufferBytes = normalizePositiveInt(
      config.detachedBufferBytes,
      STREAM_MULTIPLEXER_CONFIG.detachedBufferBytes,
    );
  }
  if (config.uploadChunkSize !== undefined) {
    STREAM_MULTIPLEXER_CONFIG.uploadChunkSize = normalizePositiveInt(
      config.uploadChunkSize,
      STREAM_MULTIPLEXER_CONFIG.uploadChunkSize,
    );
  }
  if (config.uploadWindowChunks !== undefined) {
    STREAM_MULTIPLEXER_CONFIG.uploadWindowChunks = normalizePositiveInt(
      config.uploadWindowChunks,
      STREAM_MULTIPLEXER_CONFIG.uploadWindowChunks,
    );
  }
  if (config.defaultCallTimeoutMs !== undefined) {
    STREAM_MULTIPLEXER_CONFIG.defaultCallTimeoutMs = normalizePositiveInt(
      config.defaultCallTimeoutMs,
      STREAM_MULTIPLEXER_CONFIG.defaultCallTimeoutMs,
    );
  }
}

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
  private recvBuf = new Uint8Array(8192); // Pre-allocated receive buffer
  private recvStart = 0; // Read offset into recvBuf
  private recvEnd = 0; // Write offset into recvBuf
  private readonly detachedBufferBytes: number;
  private readonly scrollback: CircularBuffer;

  constructor(
    public readonly id: number,
    public readonly type: StreamType,
    private mux: StreamMultiplexer,
  ) {
    this.detachedBufferBytes = STREAM_MULTIPLEXER_CONFIG.detachedBufferBytes;
    this.scrollback = new CircularBuffer(
      STREAM_MULTIPLEXER_CONFIG.scrollbackBytes,
    );
  }

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
        if (replayLength > 0) {
          handler(scrollback.slice(0, replayLength));
        }
      }

      // Then flush any buffered data that arrived while detached
      if (this.buffer.length > 0) {
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
    bridgeFrame[0] = BridgeOpcode.StreamData;
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
    closeFrame[0] = BridgeOpcode.StreamClose;
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
    abortFrame[0] = BridgeOpcode.StreamAbort;
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
      this.buffer.push(data);
      this.bufferedBytes += data.length;

      while (
        this.bufferedBytes > this.detachedBufferBytes &&
        this.buffer.length > 0
      ) {
        const dropped = this.buffer.shift();
        if (!dropped) break;
        this.bufferedBytes -= dropped.length;
      }
    }
  }

  /**
   * Ensure recvBuf has room for `needed` additional bytes.
   * Compacts first, then grows by doubling if still insufficient.
   */
  private ensureRecvCapacity(needed: number): void {
    const used = this.recvEnd - this.recvStart;
    // Compact: shift valid data to front when >50% wasted
    if (this.recvStart > this.recvBuf.length >>> 1) {
      this.recvBuf.copyWithin(0, this.recvStart, this.recvEnd);
      this.recvStart = 0;
      this.recvEnd = used;
    }
    // Grow if still not enough room
    if (this.recvEnd + needed > this.recvBuf.length) {
      let newCap = this.recvBuf.length;
      while (newCap < used + needed) newCap *= 2;
      const grown = new Uint8Array(newCap);
      grown.set(this.recvBuf.subarray(this.recvStart, this.recvEnd));
      this.recvBuf = grown;
      this.recvStart = 0;
      this.recvEnd = used;
    }
  }

  /**
   * Handle raw bytes from WebSocket, accumulate in recvBuf,
   * and parse complete StreamFrames.
   * StreamFrame format: [opcode:1][streamID:4][length:4][payload:N]
   */
  handleRawData(data: Uint8Array): void {
    this.ensureRecvCapacity(data.length);
    this.recvBuf.set(data, this.recvEnd);
    this.recvEnd += data.length;

    // Parse complete frames from buffer
    while (this.recvEnd - this.recvStart >= 9) {
      const view = new DataView(
        this.recvBuf.buffer,
        this.recvBuf.byteOffset + this.recvStart,
        this.recvEnd - this.recvStart,
      );
      const opcode = this.recvBuf[this.recvStart];
      const payloadLength = view.getUint32(5, false); // Big endian
      const frameLength = 9 + payloadLength;

      if (this.recvEnd - this.recvStart < frameLength) {
        break; // Incomplete frame
      }

      // Extract payload (only allocation per frame — unavoidable for handoff)
      const payloadStart = this.recvStart + 9;
      const payload = this.recvBuf.slice(
        payloadStart,
        payloadStart + payloadLength,
      );

      // Advance read cursor past this frame
      this.recvStart += frameLength;

      // Route based on opcode
      switch (opcode) {
        case BridgeOpcode.StreamData:
          this.handleData(payload);
          break;
        case BridgeOpcode.StreamClose:
          this.handleClose();
          this.mux.removeStream(this.id);
          break;
        case BridgeOpcode.StreamProgress:
          this.handleProgress(payload);
          break;
        case BridgeOpcode.StreamResult:
          this.handleResult(payload);
          break;
      }
    }

    // Reset offsets when buffer is fully consumed
    if (this.recvStart === this.recvEnd) {
      this.recvStart = 0;
      this.recvEnd = 0;
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
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private reconnectAttempts = 0;
  private shouldReconnect = true;

  // Rapid-close detection: when the server upgrades the WebSocket but
  // immediately closes it (e.g. expired session), the 1008 close frame
  // can be lost and the browser reports code 1006 instead.  Track
  // consecutive "opened then closed within a short window" cycles so we
  // can escalate to an auth error instead of looping forever.
  private rapidCloseCount = 0;
  private connectionOpenedAt = 0;
  private stableConnectionTimer: ReturnType<typeof setTimeout> | null = null;
  private static readonly RAPID_CLOSE_THRESHOLD_MS = 5000;
  private static readonly MAX_RAPID_CLOSES = 3;

  private readonly handleVisibilityChange = () => {
    if (document.visibilityState !== "visible") {
      return;
    }
    this.reconnect();
  };

  private readonly handleOnline = () => {
    this.reconnect();
  };

  constructor(url: string) {
    this.url = url;
    if (typeof document !== "undefined") {
      document.addEventListener(
        "visibilitychange",
        this.handleVisibilityChange,
      );
    }
    if (typeof window !== "undefined") {
      window.addEventListener("online", this.handleOnline);
    }
    this.connect();
  }

  private connect(): void {
    if (
      this.ws?.readyState === WebSocket.OPEN ||
      this.ws?.readyState === WebSocket.CONNECTING
    ) {
      return;
    }

    if (this._status !== "connecting") {
      this._status = "connecting";
      this.notifyStatusChange("connecting");
    }

    this.ws = new WebSocket(this.url);
    this.ws.binaryType = "arraybuffer";

    this.ws.onopen = () => {
      this.clearReconnectTimer();
      this.reconnectAttempts = 0;
      this.shouldReconnect = true;
      this.connectionOpenedAt = Date.now();
      this._status = "open";
      this.notifyStatusChange("open");

      // Only reset rapid-close counter once the connection proves stable.
      // If the server is rejecting us (session expired), the connection
      // opens then closes within milliseconds — we must not reset here.
      this.stableConnectionTimer = setTimeout(() => {
        this.rapidCloseCount = 0;
        this.stableConnectionTimer = null;
      }, StreamMultiplexer.RAPID_CLOSE_THRESHOLD_MS);
    };

    this.ws.onclose = (event: CloseEvent) => {
      this.ws = null;

      if (this.stableConnectionTimer) {
        clearTimeout(this.stableConnectionTimer);
        this.stableConnectionTimer = null;
      }

      // Detect rapid open→close: the connection opened but was closed
      // almost immediately.  This happens when the server upgrades the
      // WebSocket then sends a close frame (e.g. 1008 for expired
      // session), but the browser may report code 1006 instead if the
      // TCP connection is torn down before the close frame arrives.
      const wasRapidClose =
        this.connectionOpenedAt > 0 &&
        Date.now() - this.connectionOpenedAt <
          StreamMultiplexer.RAPID_CLOSE_THRESHOLD_MS;
      this.connectionOpenedAt = 0;

      if (wasRapidClose) {
        this.rapidCloseCount++;
      }

      console.log(
        `[StreamMultiplexer] WebSocket closed: code=${event.code}, reason="${event.reason}"` +
          (wasRapidClose
            ? ` (rapid close ${this.rapidCloseCount}/${StreamMultiplexer.MAX_RAPID_CLOSES})`
            : ""),
      );

      // Close code 1008 = Policy Violation (session expired), OR the
      // server keeps rejecting us (rapid close threshold exceeded).
      // In both cases the user must re-authenticate.
      if (
        event.code === 1008 ||
        this.rapidCloseCount >= StreamMultiplexer.MAX_RAPID_CLOSES
      ) {
        this.shouldReconnect = false;
        this.clearReconnectTimer();
        this.rapidCloseCount = 0;
        this._status = "error";
        this.notifyStatusChange("error");
        this.closeAllStreams();
      } else {
        // Network error or normal closure - mark as closed
        // Frontend can decide whether to show reconnect UI
        if (!wasRapidClose) {
          this.rapidCloseCount = 0;
        }
        this._status = "closed";
        this.notifyStatusChange("closed");
        this.closeAllStreams();
        this.scheduleReconnect();
      }
    };

    this.ws.onerror = (event) => {
      console.warn("[StreamMultiplexer] WebSocket error:", event);
    };

    this.ws.onmessage = (event) => {
      if (event.data instanceof ArrayBuffer) {
        this.handleMessage(event.data);
      }
    };
  }

  private clearReconnectTimer(): void {
    if (this.reconnectTimer !== null) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
  }

  private scheduleReconnect(): void {
    if (!this.shouldReconnect) {
      return;
    }
    if (
      this.ws?.readyState === WebSocket.OPEN ||
      this.ws?.readyState === WebSocket.CONNECTING
    ) {
      return;
    }
    if (this.reconnectTimer !== null) {
      return;
    }

    const baseDelay = Math.min(1000 * 2 ** this.reconnectAttempts, 10000);
    const jitter = Math.floor(Math.random() * 250);
    const delay = baseDelay + jitter;
    this.reconnectAttempts += 1;

    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      this.connect();
    }, delay);
  }

  reconnect(): void {
    if (!this.shouldReconnect) {
      return;
    }
    this.clearReconnectTimer();
    this.connect();
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
    bridgeFrame[0] = BridgeOpcode.StreamOpen;
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

    if (flags & (Flags.FIN | Flags.RST)) {
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
    this.shouldReconnect = false;
    this.clearReconnectTimer();
    if (this.stableConnectionTimer) {
      clearTimeout(this.stableConnectionTimer);
      this.stableConnectionTimer = null;
    }
    if (typeof document !== "undefined") {
      document.removeEventListener(
        "visibilitychange",
        this.handleVisibilityChange,
      );
    }
    if (typeof window !== "undefined") {
      window.removeEventListener("online", this.handleOnline);
    }
    this.closeAllStreams();
    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
    this._status = "closed";
  }
}

// Singleton encoder/decoder — avoids allocation on every call
const textEncoder = new TextEncoder();
const textDecoder = new TextDecoder();

export function encodeString(str: string): Uint8Array {
  return textEncoder.encode(str);
}

export function decodeString(data: Uint8Array): string {
  return textDecoder.decode(data);
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
export function initStreamMux(
  config?: Partial<StreamMultiplexerConfig>,
): StreamMultiplexer {
  if (config) {
    configureStreamMultiplexer(config);
  }
  if (instance) {
    if (instance.status === "closed") {
      instance.reconnect();
    }
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
