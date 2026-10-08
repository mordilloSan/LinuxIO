import { describe, expect, it } from "vitest";

import type { Stream } from "../StreamMultiplexer";
import { createAcpTransport } from "./transport";

const encoder = new TextEncoder();
const decoder = new TextDecoder();

function fakeStream() {
  const written: string[] = [];
  const stream = {
    abort: () => {},
    close: () => {
      stream.onClose?.();
    },
    id: 1,
    onClose: null,
    onData: null,
    onProgress: null,
    onResult: null,
    resize: () => {},
    status: "open",
    type: "assistant.open",
    write: (data: Uint8Array) => {
      written.push(decoder.decode(data));
    },
  } as unknown as Stream & {
    onData: ((data: Uint8Array) => void) | null;
    onClose: (() => void) | null;
  };
  const feed = (text: string) => stream.onData?.(encoder.encode(text));
  return { feed, stream, written };
}

async function readAll(readable: ReadableStream<unknown>, count: number) {
  const reader = readable.getReader();
  const out: unknown[] = [];
  while (out.length < count) {
    const { value, done } = await reader.read();
    if (done) break;
    out.push(value);
  }
  reader.releaseLock();
  return out;
}

describe("createAcpTransport", () => {
  it("resolves ready from the bridge line and hides it from the SDK", async () => {
    const { feed, stream } = fakeStream();
    const transport = createAcpTransport(stream);
    feed(
      '{"linuxio":"ready","cwd":"/home/alice"}\n{"jsonrpc":"2.0","id":1,"result":{}}\n',
    );
    await expect(transport.ready).resolves.toEqual({ cwd: "/home/alice" });
    const [message] = await readAll(transport.stream.readable, 1);
    expect(message).toEqual({ jsonrpc: "2.0", id: 1, result: {} });
  });

  it("reassembles a message split across frames", async () => {
    const { feed, stream } = fakeStream();
    const transport = createAcpTransport(stream);
    feed(
      '{"linuxio":"ready","cwd":"/h"}\n{"jsonrpc":"2.0","method":"session/upd',
    );
    feed('ate","params":{"a":1}}\n');
    const [message] = await readAll(transport.stream.readable, 1);
    expect(message).toEqual({
      jsonrpc: "2.0",
      method: "session/update",
      params: { a: 1 },
    });
  });

  it("writes outgoing messages as one JSON line each", async () => {
    const { stream, written } = fakeStream();
    const transport = createAcpTransport(stream);
    const writer = transport.stream.writable.getWriter();
    await writer.write({
      jsonrpc: "2.0",
      id: 7,
      method: "initialize",
      params: {},
    });
    writer.releaseLock();
    expect(written).toEqual([
      '{"jsonrpc":"2.0","id":7,"method":"initialize","params":{}}\n',
    ]);
  });

  it("resolves exit from the bridge line and closes the readable", async () => {
    const { feed, stream } = fakeStream();
    const transport = createAcpTransport(stream);
    feed(
      '{"linuxio":"ready","cwd":"/h"}\n{"linuxio":"exit","code":3,"stderr":"boom\\n"}\n',
    );
    stream.close();
    await expect(transport.exit).resolves.toEqual({
      code: 3,
      stderr: "boom\n",
    });
    const reader = transport.stream.readable.getReader();
    await expect(reader.read()).resolves.toEqual({
      done: true,
      value: undefined,
    });
  });

  it("resolves exit with null when the stream closes without an exit line", async () => {
    const { stream } = fakeStream();
    const transport = createAcpTransport(stream);
    stream.close();
    await expect(transport.exit).resolves.toBeNull();
  });

  it("passes a non-JSON line through so the SDK can answer with a parse error", async () => {
    const { feed, stream, written } = fakeStream();
    const transport = createAcpTransport(stream);
    feed(
      '{"linuxio":"ready","cwd":"/h"}\nnpm warn deprecated something\n{"jsonrpc":"2.0","id":2,"result":{}}\n',
    );
    const [message] = await readAll(transport.stream.readable, 1);
    expect(message).toEqual({ jsonrpc: "2.0", id: 2, result: {} });
    expect(written.some((line) => line.includes("-32700"))).toBe(true);
  });

  it("recognises bridge lines with the linuxio key last (Go sorts map keys)", async () => {
    const { feed, stream } = fakeStream();
    const transport = createAcpTransport(stream);
    feed(
      '{"cwd":"/home/x","linuxio":"ready"}\n{"code":1,"linuxio":"exit","stderr":"bye"}\n{"jsonrpc":"2.0","id":4,"result":{}}\n',
    );
    await expect(transport.ready).resolves.toEqual({ cwd: "/home/x" });
    const [message] = await readAll(transport.stream.readable, 1);
    expect(message).toEqual({ jsonrpc: "2.0", id: 4, result: {} });
    stream.close();
    await expect(transport.exit).resolves.toEqual({ code: 1, stderr: "bye" });
  });

  it("passes a JSON-RPC line that merely mentions linuxio through", async () => {
    const { feed, stream } = fakeStream();
    const transport = createAcpTransport(stream);
    feed(
      '{"jsonrpc":"2.0","id":5,"result":{"linuxio":"ready","cwd":"/x"}}\n{"jsonrpc":"2.0","linuxio":"x","id":6,"result":{}}\n',
    );
    const out = await readAll(transport.stream.readable, 2);
    expect(out).toHaveLength(2);
  });

  it("ignores data that arrives after close", () => {
    const { feed, stream } = fakeStream();
    const transport = createAcpTransport(stream);
    transport.close();
    expect(() => feed('{"jsonrpc":"2.0","id":1,"result":{}}\n')).not.toThrow();
  });
});
