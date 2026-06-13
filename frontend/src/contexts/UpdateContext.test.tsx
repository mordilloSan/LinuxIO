import { beforeEach, describe, expect, it, vi } from "vitest";

import type { Stream } from "@/api";

const apiMocks = vi.hoisted(() => ({
  bindStreamHandlers: vi.fn(),
  decodeString: vi.fn((data: Uint8Array) => new TextDecoder().decode(data)),
  getStreamMux: vi.fn(),
  openAppUpdateStream: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    bindStreamHandlers: apiMocks.bindStreamHandlers,
    decodeString: apiMocks.decodeString,
    getStreamMux: apiMocks.getStreamMux,
    openAppUpdateStream: apiMocks.openAppUpdateStream,
  };
});

const { UpdateProvider } = await import("@/contexts/UpdateContext");
const { useLinuxIOUpdater } = await import("@/hooks/useLinuxIOUpdater");
const { act, render, screen, waitFor } = await import("@/test/render");

async function flushPromises() {
  await Promise.resolve();
  await Promise.resolve();
}

function createStream(overrides: Partial<Stream> = {}): Stream {
  return {
    abort: vi.fn(),
    close: vi.fn(),
    id: 1,
    onClose: null,
    onData: null,
    onProgress: null,
    onResult: null,
    resize: vi.fn(),
    status: "open",
    type: "request",
    write: vi.fn(),
    ...overrides,
  };
}

function response(data: unknown, ok = true): Response {
  return {
    json: async () => data,
    ok,
  } as Response;
}

function Probe() {
  const update = useLinuxIOUpdater();
  return (
    <div>
      <div data-testid="phase">{update.phase}</div>
      <div data-testid="status">{update.status}</div>
      <div data-testid="progress">{update.progress}</div>
      <div data-testid="error">{update.error ?? ""}</div>
      <div data-testid="target">{update.targetVersion ?? ""}</div>
      <div data-testid="can-navigate">{String(update.canNavigate)}</div>
      <div data-testid="output">{update.output.join("|")}</div>
      <button onClick={() => update.startUpdate("v2.0.0")}>start</button>
      <button onClick={() => update.resetUpdate()}>reset</button>
    </div>
  );
}

function renderProvider() {
  return render(
    <UpdateProvider>
      <Probe />
    </UpdateProvider>,
  );
}

describe("UpdateProvider", () => {
  beforeEach(() => {
    apiMocks.bindStreamHandlers.mockReturnValue(vi.fn());
    vi.stubGlobal("fetch", vi.fn());
  });

  it("starts idle and allows navigation before updates run", () => {
    renderProvider();

    expect(screen.getByTestId("phase")).toHaveTextContent("idle");
    expect(screen.getByTestId("progress")).toHaveTextContent("0");
    expect(screen.getByTestId("can-navigate")).toHaveTextContent("true");
  });

  it("fails fast when the stream mux is not open", async () => {
    apiMocks.getStreamMux.mockReturnValue(null);
    renderProvider();

    await act(async () => {
      screen.getByRole("button", { name: "start" }).click();
    });

    expect(screen.getByTestId("phase")).toHaveTextContent("failed");
    expect(screen.getByTestId("status")).toHaveTextContent("Update failed");
    expect(screen.getByTestId("error")).toHaveTextContent(
      "Stream connection not ready",
    );
    expect(screen.getByTestId("progress")).toHaveTextContent("100");
  });

  it("opens the update stream, records progress output, and resets cleanly", async () => {
    const stream = createStream();
    const unbind = vi.fn();
    const mux = { setUpdating: vi.fn(), status: "open" };
    let handlers!: {
      onClose: () => void;
      onData: (data: Uint8Array) => void;
      onResult: (result: { status: "ok" | "error"; error?: string }) => void;
    };
    apiMocks.getStreamMux.mockReturnValue(mux);
    apiMocks.openAppUpdateStream.mockReturnValue(stream);
    apiMocks.bindStreamHandlers.mockImplementation((_stream, nextHandlers) => {
      handlers = nextHandlers;
      return unbind;
    });
    renderProvider();

    await act(async () => {
      screen.getByRole("button", { name: "start" }).click();
    });

    expect(apiMocks.openAppUpdateStream).toHaveBeenCalledWith(
      expect.any(String),
      "v2.0.0",
    );
    expect(mux.setUpdating).toHaveBeenCalledWith(true);
    expect(screen.getByTestId("phase")).toHaveTextContent("running");
    expect(screen.getByTestId("target")).toHaveTextContent("v2.0.0");
    expect(screen.getByTestId("can-navigate")).toHaveTextContent("false");

    await act(async () => {
      handlers.onData(
        new TextEncoder().encode(
          "Step 3/5: Installing binaries\nInstallation complete\n",
        ),
      );
    });

    expect(screen.getByTestId("output")).toHaveTextContent(
      "Step 3/5: Installing binaries|Installation complete",
    );
    expect(screen.getByTestId("status")).toHaveTextContent(
      "Installation complete",
    );
    expect(screen.getByTestId("progress")).toHaveTextContent("85");

    await act(async () => {
      screen.getByRole("button", { name: "reset" }).click();
    });

    expect(unbind).toHaveBeenCalledTimes(1);
    expect(stream.close).toHaveBeenCalledTimes(1);
    expect(mux.setUpdating).toHaveBeenLastCalledWith(false);
    expect(screen.getByTestId("phase")).toHaveTextContent("idle");
    expect(screen.getByTestId("output")).toHaveTextContent("");
  });

  it("verifies restart completion after the update stream finishes", async () => {
    vi.useFakeTimers();
    const stream = createStream();
    const mux = { setUpdating: vi.fn(), status: "open" };
    let handlers!: {
      onClose: () => void;
      onData: (data: Uint8Array) => void;
      onResult: (result: { status: "ok" | "error"; error?: string }) => void;
    };
    let statusCalls = 0;
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.startsWith("/api/update-status")) {
        statusCalls += 1;
        return response({ status: statusCalls === 1 ? "running" : "ok" });
      }
      if (url === "/api/version") {
        return response({ version: "v2.0.0" });
      }
      return response({}, false);
    });
    apiMocks.getStreamMux.mockReturnValue(mux);
    apiMocks.openAppUpdateStream.mockReturnValue(stream);
    apiMocks.bindStreamHandlers.mockImplementation((_stream, nextHandlers) => {
      handlers = nextHandlers;
      return vi.fn();
    });
    renderProvider();

    await act(async () => {
      screen.getByRole("button", { name: "start" }).click();
      handlers.onData(new TextEncoder().encode("Downloading binaries\n"));
      handlers.onResult({ status: "ok" });
    });

    await act(async () => {
      await flushPromises();
    });

    expect(screen.getByTestId("phase")).toHaveTextContent("verifying");

    await act(async () => {
      await vi.advanceTimersByTimeAsync(2000);
      await flushPromises();
    });

    expect(screen.getByTestId("phase")).toHaveTextContent("done");
    expect(screen.getByTestId("status")).toHaveTextContent("Update complete");
    expect(screen.getByTestId("progress")).toHaveTextContent("100");
    expect(mux.setUpdating).toHaveBeenLastCalledWith(false);
  });

  it("surfaces backend update-status failures after stream errors", async () => {
    const stream = createStream();
    const mux = { setUpdating: vi.fn(), status: "open" };
    let handlers!: {
      onClose: () => void;
      onData: (data: Uint8Array) => void;
      onResult: (result: { status: "ok" | "error"; error?: string }) => void;
    };
    vi.mocked(fetch).mockResolvedValue(
      response({
        exit_code: 7,
        message: "checksum mismatch",
        status: "error",
      }),
    );
    apiMocks.getStreamMux.mockReturnValue(mux);
    apiMocks.openAppUpdateStream.mockReturnValue(stream);
    apiMocks.bindStreamHandlers.mockImplementation((_stream, nextHandlers) => {
      handlers = nextHandlers;
      return vi.fn();
    });
    renderProvider();

    await act(async () => {
      screen.getByRole("button", { name: "start" }).click();
      handlers.onResult({ status: "error", error: "stream failed" });
    });

    await waitFor(() => {
      expect(screen.getByTestId("phase")).toHaveTextContent("failed");
    });
    expect(screen.getByTestId("error")).toHaveTextContent(
      "Update failed (exit code 7): checksum mismatch",
    );
    expect(mux.setUpdating).toHaveBeenLastCalledWith(false);
  });
});
