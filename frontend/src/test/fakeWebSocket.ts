export type FakeCloseEvent = Pick<CloseEvent, "code" | "reason">;
type Bytes = Uint8Array<ArrayBufferLike>;

export class FakeWebSocket {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSING = 2;
  static readonly CLOSED = 3;

  static instances: FakeWebSocket[] = [];

  binaryType: BinaryType = "blob";
  onclose: ((event: CloseEvent) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  onmessage: ((event: MessageEvent<ArrayBuffer>) => void) | null = null;
  onopen: ((event: Event) => void) | null = null;
  readyState = FakeWebSocket.CONNECTING;
  sent: Bytes[] = [];

  constructor(readonly url: string) {
    FakeWebSocket.instances.push(this);
  }

  static install() {
    FakeWebSocket.instances = [];
    Object.defineProperty(globalThis, "WebSocket", {
      configurable: true,
      writable: true,
      value: FakeWebSocket,
    });
    Object.defineProperty(window, "WebSocket", {
      configurable: true,
      writable: true,
      value: FakeWebSocket,
    });
  }

  static latest(): FakeWebSocket {
    const socket = FakeWebSocket.instances.at(-1);
    if (!socket) {
      throw new Error("No FakeWebSocket instances created");
    }
    return socket;
  }

  open() {
    this.readyState = FakeWebSocket.OPEN;
    this.onopen?.(new Event("open"));
  }

  closeWith({ code = 1000, reason = "" }: Partial<FakeCloseEvent> = {}) {
    this.readyState = FakeWebSocket.CLOSED;
    this.onclose?.({ code, reason } as CloseEvent);
  }

  error() {
    this.onerror?.(new Event("error"));
  }

  receive(data: Uint8Array | ArrayBuffer) {
    const buffer = data instanceof Uint8Array ? data.buffer.slice(0) : data;
    this.onmessage?.({ data: buffer } as MessageEvent<ArrayBuffer>);
  }

  send(data: ArrayBuffer | ArrayBufferView | string | Blob) {
    if (typeof data === "string" || data instanceof Blob) {
      throw new Error("FakeWebSocket only supports binary sends");
    }

    if (data instanceof ArrayBuffer) {
      this.sent.push(new Uint8Array(data.slice(0)));
      return;
    }

    this.sent.push(
      new Uint8Array(
        data.buffer.slice(data.byteOffset, data.byteOffset + data.byteLength),
      ),
    );
  }

  close() {
    this.closeWith();
  }
}

export function readMuxFrame(frame: Uint8Array) {
  const view = new DataView(frame.buffer, frame.byteOffset, frame.byteLength);
  return {
    streamID: view.getUint32(0, false),
    flags: view.getUint8(4),
    payload: frame.slice(5),
  };
}

export function readBridgeFrame(frame: Uint8Array) {
  const view = new DataView(frame.buffer, frame.byteOffset, frame.byteLength);
  return {
    opcode: view.getUint8(0),
    streamID: view.getUint32(1, false),
    payloadLength: view.getUint32(5, false),
    payload: frame.slice(9),
  };
}

export function makeInboundMuxFrame(
  streamID: number,
  flags: number,
  payload: Bytes = new Uint8Array(0),
) {
  const frame = new Uint8Array(5 + payload.length);
  const view = new DataView(frame.buffer);
  view.setUint32(0, streamID, false);
  frame[4] = flags;
  frame.set(payload, 5);
  return frame;
}

export function makeBridgeFrame(
  opcode: number,
  streamID: number,
  payload: Bytes = new Uint8Array(0),
) {
  const frame = new Uint8Array(9 + payload.length);
  const view = new DataView(frame.buffer);
  frame[0] = opcode;
  view.setUint32(1, streamID, false);
  view.setUint32(5, payload.length, false);
  frame.set(payload, 9);
  return frame;
}
