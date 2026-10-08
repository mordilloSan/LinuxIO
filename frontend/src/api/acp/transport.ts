import * as acp from "@agentclientprotocol/sdk";

import { bindStreamHandlers } from "../stream-helpers";
import type { Stream } from "../StreamMultiplexer";

export interface AssistantReady {
  cwd: string;
}

export interface AssistantExit {
  code: number;
  stderr: string;
}

export interface AcpTransport {
  stream: acp.Stream;
  ready: Promise<AssistantReady>;
  exit: Promise<AssistantExit | null>;
  close(): void;
}

// Go marshals the bridge's lines with sorted keys, so "linuxio" is not first.
const BRIDGE_LINE_MARKER = '"linuxio":';

/**
 * Turns a LinuxIO duplex stream into the ND-JSON web-stream pair the ACP SDK
 * consumes. The bridge's own `{"linuxio":…}` ready and exit lines are
 * intercepted here; every other line goes to the SDK untouched.
 */
export function createAcpTransport(stream: Stream): AcpTransport {
  const encoder = new TextEncoder();
  const decoder = new TextDecoder();

  let resolveReady!: (value: AssistantReady) => void;
  let rejectReady!: (reason: Error) => void;
  const ready = new Promise<AssistantReady>((resolve, reject) => {
    resolveReady = resolve;
    rejectReady = reject;
  });
  let resolveExit!: (value: AssistantExit | null) => void;
  const exit = new Promise<AssistantExit | null>((resolve) => {
    resolveExit = resolve;
  });
  let exitSeen = false;
  let pending = "";
  let controller: ReadableStreamDefaultController<Uint8Array> | null = null;
  let closed = false;

  const handleBridgeLine = (line: string) => {
    let parsed: {
      linuxio?: string;
      cwd?: string;
      code?: number;
      stderr?: string;
    };
    try {
      parsed = JSON.parse(line);
    } catch {
      return false;
    }
    if (
      typeof parsed !== "object" ||
      parsed === null ||
      typeof parsed.linuxio !== "string" ||
      "jsonrpc" in parsed
    ) {
      return false;
    }
    if (parsed.linuxio === "ready" && typeof parsed.cwd === "string") {
      resolveReady({ cwd: parsed.cwd });
    } else if (parsed.linuxio === "exit") {
      exitSeen = true;
      resolveExit({ code: parsed.code ?? -1, stderr: parsed.stderr ?? "" });
    }
    return true;
  };

  const handleText = (text: string) => {
    if (closed) return;
    pending += text;
    let newline = pending.indexOf("\n");
    while (newline >= 0) {
      const line = pending.slice(0, newline);
      pending = pending.slice(newline + 1);
      if (!(line.includes(BRIDGE_LINE_MARKER) && handleBridgeLine(line))) {
        try {
          controller?.enqueue(encoder.encode(`${line}\n`));
        } catch {
          // The reader already cancelled or closed the readable.
        }
      }
      newline = pending.indexOf("\n");
    }
  };

  const finish = () => {
    if (closed) return;
    closed = true;
    if (!exitSeen) resolveExit(null);
    rejectReady(
      new Error("Assistant stream closed before the agent was ready"),
    );
    try {
      controller?.close();
    } catch {
      // Already closed by the reader.
    }
  };

  const readable = new ReadableStream<Uint8Array>({
    start(c) {
      controller = c;
    },
    cancel() {
      stream.close();
    },
  });

  const writable = new WritableStream<Uint8Array>({
    write(chunk) {
      stream.write(chunk);
    },
    close() {
      stream.close();
    },
  });

  const unbind = bindStreamHandlers(stream, {
    onData: (data) => handleText(decoder.decode(data, { stream: true })),
    onClose: () => {
      finish();
      unbind();
    },
  });

  // Swallow the rejection for callers that only await `exit`.
  ready.catch(() => {});

  return {
    stream: acp.ndJsonStream(writable, readable),
    ready,
    exit,
    close: () => {
      stream.close();
      finish();
    },
  };
}
