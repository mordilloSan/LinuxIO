import { onlineManager } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { LinuxIOError } from "@/api/errors";
import { createQueryClient } from "@/api/query-client";
import { createTestQueryClient, render, screen } from "@/test/render";

import SavedOutput from "./SavedOutput";

const mocks = vi.hoisted(() => ({ call: vi.fn(), toastError: vi.fn() }));

vi.mock("sonner", () => ({ toast: { error: mocks.toastError } }));

vi.mock("@/api/calls", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/calls")>()),
  call: mocks.call,
}));

const CHUNK = 262144;

describe("SavedOutput", () => {
  beforeEach(() => {
    // Importing the app's query client ties "online" to the live transport,
    // which a unit test never opens.
    onlineManager.setEventListener(() => () => {});
    onlineManager.setOnline(true);
    mocks.call.mockReset();
    mocks.toastError.mockReset();
  });

  it("asks for the end of the output and shows it", async () => {
    mocks.call.mockResolvedValue({ content: "all of it", offset: 0, size: 9 });

    render(<SavedOutput taskId="task-1" />);

    expect(await screen.findByText("all of it")).toBeInTheDocument();
    expect(mocks.call).toHaveBeenCalledTimes(1);
    expect(mocks.call.mock.calls[0].slice(0, 2)).toEqual([
      "tasks.output",
      { taskId: "task-1" },
    ]);
    expect(
      screen.queryByRole("button", { name: "Show earlier" }),
    ).not.toBeInTheDocument();
  });

  it("loads earlier parts from the reported start and puts them first", async () => {
    const start = 600000;
    mocks.call
      .mockResolvedValueOnce({ content: "tail", offset: start, size: 700000 })
      // The server reports a start a few bytes after the one asked for.
      .mockResolvedValueOnce({
        content: "middle ",
        offset: start - CHUNK + 3,
        size: 700000,
      })
      .mockResolvedValueOnce({ content: "head ", offset: 0, size: 700000 });

    const { user } = render(<SavedOutput taskId="task-1" />);
    await screen.findByText("tail");

    await user.click(screen.getByRole("button", { name: "Show earlier" }));
    await screen.findByText(/middle tail/);
    expect(mocks.call.mock.calls[1].slice(0, 2)).toEqual([
      "tasks.output",
      { taskId: "task-1", offset: start - CHUNK, limit: CHUNK },
    ]);

    const reported = start - CHUNK + 3;
    await user.click(screen.getByRole("button", { name: "Show earlier" }));
    await screen.findByText(/head middle tail/);
    expect(mocks.call.mock.calls[2].slice(0, 2)).toEqual([
      "tasks.output",
      { taskId: "task-1", offset: reported - CHUNK, limit: CHUNK },
    ]);
    expect(
      screen.queryByRole("button", { name: "Show earlier" }),
    ).not.toBeInTheDocument();
  });

  it("keeps every loaded part when a remount refetches after a failed earlier part", async () => {
    const start = 600000;
    const tail = { content: "tail", offset: start, size: 700000 };
    const middle = { content: "middle ", offset: start - CHUNK, size: 700000 };
    mocks.call.mockImplementation(
      async (_route: string, request: { offset?: number }) => {
        if (request.offset === undefined) return tail;
        if (request.offset === middle.offset) return middle;
        throw new LinuxIOError("connection lost", "connection_unavailable");
      },
    );
    const queryClient = createTestQueryClient();

    const first = render(<SavedOutput taskId="task-1" />, { queryClient });
    await screen.findByText("tail");
    await first.user.click(
      screen.getByRole("button", { name: "Show earlier" }),
    );
    await screen.findByText(/middle tail/);
    await first.user.click(
      screen.getByRole("button", { name: "Show earlier" }),
    );
    await screen.findByText(/Unable to load earlier output/);
    first.unmount();

    // The failed fetch left the query invalidated, so the remount refetches.
    const calls = mocks.call.mock.calls.length;
    render(<SavedOutput taskId="task-1" />, { queryClient });
    await vi.waitFor(() =>
      expect(mocks.call.mock.calls.length).toBeGreaterThan(calls),
    );
    await vi.waitFor(() => expect(queryClient.isFetching()).toBe(0));
    expect(await screen.findByText("middle tail")).toBeInTheDocument();
  });

  it("stops offering earlier parts when a reply does not move the start", async () => {
    mocks.call
      .mockResolvedValueOnce({ content: "tail", offset: 500, size: 504 })
      .mockResolvedValueOnce({ content: "again", offset: 500, size: 504 });

    const { user } = render(<SavedOutput taskId="task-1" />);
    await screen.findByText("tail");

    await user.click(screen.getByRole("button", { name: "Show earlier" }));

    await vi.waitFor(() =>
      expect(
        screen.queryByRole("button", { name: "Show earlier" }),
      ).not.toBeInTheDocument(),
    );
    expect(screen.queryByText(/again/)).not.toBeInTheDocument();
  });

  it("shows the output as text, never as markup", async () => {
    const content = "<script>alert(1)</script>\x1b[31mred";
    mocks.call.mockResolvedValue({
      content,
      offset: 0,
      size: content.length,
    });

    const { container } = render(<SavedOutput taskId="task-1" />);

    await screen.findByText(content);
    expect(container.querySelector("script")).toBeNull();
    expect(document.querySelector("script")).toBeNull();
  });

  it("says so when no output was saved", async () => {
    mocks.call.mockRejectedValue(new LinuxIOError("not found", 404));

    render(<SavedOutput taskId="task-1" />);

    expect(
      await screen.findByText("No output was saved for this run."),
    ).toBeInTheDocument();
  });

  it("neither retries nor toasts a missing output under the app's query client", async () => {
    mocks.call.mockRejectedValue(new LinuxIOError("not found", 404));

    render(<SavedOutput taskId="task-1" />, {
      queryClient: createQueryClient(),
    });

    expect(
      await screen.findByText("No output was saved for this run."),
    ).toBeInTheDocument();
    expect(mocks.call).toHaveBeenCalledTimes(1);
    expect(mocks.toastError).not.toHaveBeenCalled();
  });

  describe("scroll position", () => {
    // jsdom has no layout: give every element a scrollport that tests size.
    const layout = { clientHeight: 100, scrollHeight: 1000 };
    const scrollTops = new WeakMap<Element, number>();
    const originals = {
      clientHeight: Object.getOwnPropertyDescriptor(
        Element.prototype,
        "clientHeight",
      ),
      scrollHeight: Object.getOwnPropertyDescriptor(
        Element.prototype,
        "scrollHeight",
      ),
      scrollTop: Object.getOwnPropertyDescriptor(
        Element.prototype,
        "scrollTop",
      ),
    };

    beforeEach(() => {
      layout.clientHeight = 100;
      layout.scrollHeight = 1000;
      Object.defineProperties(Element.prototype, {
        clientHeight: { configurable: true, get: () => layout.clientHeight },
        scrollHeight: { configurable: true, get: () => layout.scrollHeight },
        scrollTop: {
          configurable: true,
          get() {
            return scrollTops.get(this) ?? 0;
          },
          set(value: number) {
            scrollTops.set(this, value);
          },
        },
      });
    });

    afterEach(() => {
      for (const [name, descriptor] of Object.entries(originals)) {
        if (descriptor)
          Object.defineProperty(Element.prototype, name, descriptor);
      }
    });

    const box = (container: HTMLElement) =>
      container.querySelector(".log-viewport__box") as HTMLElement;

    it("opens at the end of the first chunk", async () => {
      mocks.call.mockResolvedValue({
        content: "all of it",
        offset: 0,
        size: 9,
      });

      const { container } = render(<SavedOutput taskId="task-1" />);
      await screen.findByText("all of it");

      expect(box(container).scrollTop).toBe(900);
    });

    it("keeps the view in place when earlier text is added above it", async () => {
      mocks.call
        .mockResolvedValueOnce({ content: "tail", offset: 5000, size: 5004 })
        .mockImplementationOnce(async () => {
          // The earlier text makes the box taller before it is shown.
          layout.scrollHeight = 1500;
          return { content: "head ", offset: 0, size: 5004 };
        });

      const { container, user } = render(<SavedOutput taskId="task-1" />);
      await screen.findByText("tail");
      // The reader scrolled up, leaving 700 px of text below the top of the view.
      box(container).scrollTop = 300;

      await user.click(screen.getByRole("button", { name: "Show earlier" }));
      await screen.findByText(/head tail/);

      expect(box(container).scrollTop).toBe(800);
    });

    it("starts at the end again for another run", async () => {
      mocks.call.mockResolvedValue({
        content: "first run",
        offset: 0,
        size: 9,
      });
      const view = render(<SavedOutput key="task-1" taskId="task-1" />);
      await screen.findByText("first run");
      box(view.container).scrollTop = 100;

      layout.scrollHeight = 2000;
      mocks.call.mockResolvedValue({
        content: "second run",
        offset: 0,
        size: 10,
      });
      view.rerender(<SavedOutput key="task-2" taskId="task-2" />);
      await screen.findByText("second run");

      expect(box(view.container).scrollTop).toBe(1900);
    });
  });
});
