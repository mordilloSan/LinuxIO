import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { render } from "@/test/render";

import Composer, { type ComposerProps } from "./Composer";

const mocks = vi.hoisted(() => ({ call: vi.fn() }));

vi.mock("@/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api")>()),
  call: mocks.call,
}));

vi.mock("@/components/ui/PathPickerField", () => ({
  default: ({ onChange }: { onChange(path: string): void }) => (
    <button onClick={() => onChange("/srv/notes/todo.txt")} type="button">
      pick-file
    </button>
  ),
}));

const commands = [
  {
    name: "review",
    description: "Review code",
    input: { hint: "what to review" },
  },
  { name: "compact", description: "Compact context" },
];

const modelOption = {
  id: "model",
  name: "Model",
  type: "select" as const,
  category: "model",
  currentValue: "opus",
  options: [
    { value: "opus", name: "Opus" },
    { value: "sonnet", name: "Sonnet" },
  ],
};

const effortOption = {
  id: "effort",
  name: "Effort",
  type: "select" as const,
  category: "thought_level",
  currentValue: "low",
  options: [
    {
      group: "levels",
      name: "Levels",
      options: [
        { value: "low", name: "Low" },
        { value: "high", name: "High" },
      ],
    },
  ],
};

function setup(props: Partial<ComposerProps> = {}) {
  const handlers = {
    onSend: vi.fn(),
    onSteer: vi.fn(),
    onStop: vi.fn(),
    onSetConfigOption: vi.fn(),
    onSetMode: vi.fn(),
    onBtw: vi.fn(),
  };
  const element = (extra: Partial<ComposerProps> = {}) => (
    <Composer
      canEmbed
      canFork
      canImages
      canSteer={false}
      commands={commands}
      configOptions={[]}
      connecting={false}
      disabled={false}
      modes={null}
      running={false}
      turnStartedAt={null}
      usage={null}
      {...handlers}
      {...props}
      {...extra}
    />
  );
  const view = render(element());
  return {
    ...handlers,
    unmount: view.unmount,
    rerender: (extra: Partial<ComposerProps>) => view.rerender(element(extra)),
  };
}

const png = () => new File(["abc"], "shot.png", { type: "image/png" });

beforeEach(() => {
  mocks.call.mockReset();
});

describe("Composer", () => {
  it("sends typed text on Enter", async () => {
    const { onSend } = setup();
    await userEvent.type(screen.getByLabelText("Message"), "hello{Enter}");
    expect(onSend).toHaveBeenCalledWith([{ type: "text", text: "hello" }], {
      text: "hello",
      attachments: [],
    });
  });

  it("attaches a pasted image and sends an image block before the text", async () => {
    const { onSend } = setup();
    const input = screen.getByLabelText("Message");
    fireEvent.paste(input, {
      clipboardData: { files: [png()], types: ["Files"] },
    });
    expect(await screen.findByAltText("shot.png")).toBeInTheDocument();
    await userEvent.type(input, "look{Enter}");
    expect(onSend).toHaveBeenCalledWith(
      [
        { type: "image", data: "YWJj", mimeType: "image/png" },
        { type: "text", text: "look" },
      ],
      { text: "look", attachments: ["shot.png"] },
    );
  });

  it("ignores pasted images when the agent does not take images", async () => {
    setup({ canImages: false });
    fireEvent.paste(screen.getByLabelText("Message"), {
      clipboardData: { files: [png()], types: ["Files"] },
    });
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(screen.queryByAltText("shot.png")).not.toBeInTheDocument();
  });

  it("accepts a dropped image and lets it be removed", async () => {
    setup();
    fireEvent.drop(screen.getByLabelText("Message"), {
      dataTransfer: { files: [png()], types: ["Files"] },
    });
    await screen.findByAltText("shot.png");
    await userEvent.click(
      screen.getByRole("button", { name: "Remove shot.png" }),
    );
    expect(screen.queryByAltText("shot.png")).not.toBeInTheDocument();
  });

  it("filters slash commands by prefix and inserts the chosen one on Enter", async () => {
    const { onSend } = setup();
    const input = screen.getByLabelText("Message");
    await userEvent.type(input, "/");
    expect(
      await screen.findByRole("menuitem", { name: /compact/ }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("menuitem", { name: /review/ }),
    ).toBeInTheDocument();
    const label = screen
      .getByRole("menuitem", { name: /review/ })
      .querySelector(".app-menu__item-label");
    expect(label?.closest(".assistant__slash-menu")).not.toBeNull();
    expect(label?.querySelector(".assistant__slash-name")).toHaveTextContent(
      "/review",
    );
    expect(
      label?.querySelector(".assistant__slash-description"),
    ).toHaveTextContent("Review code");
    await userEvent.type(input, "re");
    expect(
      screen.queryByRole("menuitem", { name: /compact/ }),
    ).not.toBeInTheDocument();
    await userEvent.keyboard("{Enter}");
    expect(input).toHaveValue("/review ");
    expect(screen.getByText("what to review")).toBeInTheDocument();
    expect(onSend).not.toHaveBeenCalled();
  });

  it("closes the slash menu on Escape without stopping the turn", async () => {
    const { onStop } = setup({ running: true, canSteer: true });
    const input = screen.getByLabelText("Message");
    await userEvent.type(input, "/");
    await screen.findByRole("menu", { name: "Slash commands" });
    await userEvent.keyboard("{Escape}");
    await waitFor(() =>
      expect(
        screen.queryByRole("menu", { name: "Slash commands" }),
      ).not.toBeInTheDocument(),
    );
    expect(onStop).not.toHaveBeenCalled();
  });

  it("attaches a file picked through the + menu as an embedded resource", async () => {
    mocks.call.mockResolvedValue({ content: "buy milk" });
    const { onSend } = setup();
    await userEvent.click(screen.getByRole("button", { name: "Attach" }));
    await userEvent.click(
      await screen.findByRole("menuitem", { name: /Attach file/ }),
    );
    await userEvent.click(screen.getByRole("button", { name: "pick-file" }));
    expect(await screen.findByText("todo.txt")).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText("Message"), "go{Enter}");
    expect(onSend).toHaveBeenCalledWith(
      [
        {
          type: "resource",
          resource: { uri: "file:///srv/notes/todo.txt", text: "buy milk" },
        },
        { type: "text", text: "go" },
      ],
      { text: "go", attachments: ["todo.txt"] },
    );
  });

  it("links the file instead of reading it when the agent cannot embed", async () => {
    const { onSend } = setup({ canEmbed: false });
    await userEvent.click(screen.getByRole("button", { name: "Attach" }));
    await userEvent.click(
      await screen.findByRole("menuitem", { name: /Attach file/ }),
    );
    await userEvent.click(screen.getByRole("button", { name: "pick-file" }));
    await screen.findByText("todo.txt");
    await userEvent.type(screen.getByLabelText("Message"), "go{Enter}");
    expect(mocks.call).not.toHaveBeenCalled();
    expect(onSend.mock.calls[0][0][0]).toMatchObject({ type: "resource_link" });
  });

  it("opens the picker when @ is typed at the start of a word", async () => {
    setup();
    const input = screen.getByLabelText("Message");
    await userEvent.type(input, "see @");
    expect(
      await screen.findByRole("button", { name: "pick-file" }),
    ).toBeInTheDocument();
    expect(input).toHaveValue("see ");
  });

  it("keeps the field editable but sends nothing while a non-steerable turn runs", async () => {
    const { onSend, onSteer, onStop } = setup({ running: true });
    const input = screen.getByLabelText("Message");
    expect(input).not.toHaveAttribute("readonly");
    expect(input).toBeEnabled();
    await userEvent.type(input, "hi{Enter}");
    expect(input).toHaveValue("hi");
    expect(onSend).not.toHaveBeenCalled();
    expect(onSteer).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: "Stop" }));
    expect(onStop).toHaveBeenCalledTimes(1);
  });

  it("disables the field while the agent is not ready", () => {
    setup({ disabled: true });
    expect(screen.getByLabelText("Message")).toBeDisabled();
  });

  it("steers instead of sending while a steerable turn runs", async () => {
    const { onSend, onSteer } = setup({
      running: true,
      canSteer: true,
    });
    const input = screen.getByLabelText("Message");
    expect(input).toBeEnabled();
    await userEvent.type(input, "use tabs{Enter}");
    expect(onSteer).toHaveBeenCalledWith([{ type: "text", text: "use tabs" }], {
      text: "use tabs",
      attachments: [],
    });
    expect(onSend).not.toHaveBeenCalled();
  });

  it("shows a size error for an oversized pasted image and clears it on typing", async () => {
    setup();
    const big = new File(["x"], "big.png", { type: "image/png" });
    Object.defineProperty(big, "size", { value: 6 * 1024 * 1024 });
    const input = screen.getByLabelText("Message");
    fireEvent.paste(input, { clipboardData: { files: [big] } });
    expect(await screen.findByText(/5 MB image limit/)).toBeInTheDocument();
    await userEvent.type(input, "a");
    expect(screen.queryByText(/5 MB image limit/)).not.toBeInTheDocument();
  });

  it("adds an image chosen through the + menu via the same path as paste", async () => {
    const click = vi.spyOn(HTMLInputElement.prototype, "click");
    const { onSend } = setup();
    await userEvent.click(screen.getByRole("button", { name: "Attach" }));
    await userEvent.click(
      await screen.findByRole("menuitem", { name: /Add image/ }),
    );
    expect(click).toHaveBeenCalledTimes(1);
    const input = screen.getByLabelText("Add image");
    expect(input).toHaveAttribute("accept", "image/*");
    expect(input).toHaveAttribute("multiple");
    await userEvent.upload(input, png());
    expect(await screen.findByAltText("shot.png")).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText("Message"), "look{Enter}");
    expect(onSend.mock.calls[0][0][0]).toEqual({
      type: "image",
      data: "YWJj",
      mimeType: "image/png",
    });
    click.mockRestore();
  });

  it("applies the 5 MB cap to images chosen through the + menu", async () => {
    setup();
    const big = new File(["x"], "big.png", { type: "image/png" });
    Object.defineProperty(big, "size", { value: 6 * 1024 * 1024 });
    await userEvent.upload(screen.getByLabelText("Add image"), big);
    expect(await screen.findByText(/5 MB image limit/)).toBeInTheDocument();
  });

  it("hides Add image when the agent does not take images", async () => {
    setup({ canImages: false });
    await userEvent.click(screen.getByRole("button", { name: "Attach" }));
    await screen.findByRole("menuitem", { name: /Attach file/ });
    expect(
      screen.queryByRole("menuitem", { name: /Add image/ }),
    ).not.toBeInTheDocument();
  });

  it("lists every command from the / button and inserts the chosen one", async () => {
    setup();
    await userEvent.click(screen.getByRole("button", { name: "Commands" }));
    expect(
      await screen.findByRole("menuitem", { name: /compact/ }),
    ).toBeInTheDocument();
    await userEvent.click(screen.getByRole("menuitem", { name: /review/ }));
    expect(screen.getByLabelText("Message")).toHaveValue("/review ");
    await waitFor(() =>
      expect(
        screen.queryByRole("menu", { name: "Slash commands" }),
      ).not.toBeInTheDocument(),
    );
  });

  it("changes model and effort through one chip", async () => {
    const { onSetConfigOption } = setup({
      configOptions: [effortOption, modelOption],
    });
    const chip = screen.getByRole("button", { name: "Model: Opus Low" });
    await userEvent.click(chip);
    const slider = await screen.findByRole("slider", { name: "Effort" });
    fireEvent.change(slider, { target: { value: "1" } });
    fireEvent.keyUp(slider, { key: "ArrowRight" });
    expect(onSetConfigOption).toHaveBeenCalledWith("effort", "high");
    await userEvent.click(screen.getByRole("menuitem", { name: /Sonnet/ }));
    expect(onSetConfigOption).toHaveBeenCalledWith("model", "sonnet");
  });

  it("keeps other options next to the chip and renders booleans as switches", async () => {
    const { onSetConfigOption } = setup({
      configOptions: [
        { id: "think", name: "Thinking", type: "boolean", currentValue: false },
        effortOption,
        modelOption,
      ],
    });
    expect(
      screen.getByRole("button", { name: "Model: Opus Low" }),
    ).toBeInTheDocument();
    expect(screen.queryAllByRole("combobox")).toHaveLength(0);
    await userEvent.click(screen.getByRole("checkbox", { name: "Thinking" }));
    expect(onSetConfigOption).toHaveBeenCalledWith("think", true);
  });

  it("changes the mode and hides a duplicate mode option", async () => {
    const { onSetMode } = setup({
      configOptions: [{ ...modelOption, id: "mode", category: "mode" }],
      modes: {
        currentModeId: "default",
        availableModes: [
          { id: "default", name: "Default" },
          { id: "plan", name: "Plan" },
        ],
      },
    });
    expect(screen.queryAllByRole("combobox")).toHaveLength(0);
    await userEvent.click(
      screen.getByRole("button", { name: "Mode: Default" }),
    );
    await userEvent.click(
      await screen.findByRole("menuitem", { name: "Plan" }),
    );
    expect(onSetMode).toHaveBeenCalledWith("plan");
  });

  it("cycles to the next mode on Shift+Tab", async () => {
    const { onSetMode } = setup({
      modes: {
        currentModeId: "plan",
        availableModes: [
          { id: "default", name: "Default" },
          { id: "plan", name: "Plan" },
          { id: "edit", name: "Edit" },
        ],
      },
    });
    screen.getByLabelText("Message").focus();
    await userEvent.tab({ shift: true });
    expect(onSetMode).toHaveBeenCalledWith("edit");
    expect(screen.getByLabelText("Message")).toHaveFocus();
  });

  it("skips bypassPermissions when cycling modes", async () => {
    const { onSetMode } = setup({
      modes: {
        currentModeId: "plan",
        availableModes: [
          { id: "default", name: "Default" },
          { id: "plan", name: "Plan" },
          { id: "bypassPermissions", name: "Bypass" },
        ],
      },
    });
    screen.getByLabelText("Message").focus();
    await userEvent.tab({ shift: true });
    expect(onSetMode).toHaveBeenCalledWith("default");
    expect(onSetMode).not.toHaveBeenCalledWith("bypassPermissions");
  });

  it("does not cycle with a single cyclable mode or while connecting", async () => {
    const { onSetMode, rerender } = setup({
      modes: {
        currentModeId: "default",
        availableModes: [
          { id: "default", name: "Default" },
          { id: "bypassPermissions", name: "Bypass" },
        ],
      },
    });
    screen.getByLabelText("Message").focus();
    await userEvent.tab({ shift: true });
    expect(onSetMode).not.toHaveBeenCalled();
    rerender({
      connecting: true,
      modes: {
        currentModeId: "default",
        availableModes: [
          { id: "default", name: "Default" },
          { id: "plan", name: "Plan" },
        ],
      },
    });
    screen.getByLabelText("Message").focus();
    await userEvent.tab({ shift: true });
    expect(onSetMode).not.toHaveBeenCalled();
  });

  it("announces the mode shortcut on the field", () => {
    setup();
    expect(screen.getByLabelText("Message")).toHaveAttribute(
      "aria-keyshortcuts",
      "Shift+Tab",
    );
  });

  it("is a Send button when idle and a Stop button while running", () => {
    const { unmount } = setup();
    expect(screen.getByRole("button", { name: "Send" })).toBeDisabled();
    unmount();
    setup({ running: true });
    expect(screen.getByRole("button", { name: "Stop" })).toBeEnabled();
  });

  it("shows the context gauge once usage exists", () => {
    setup({ usage: { used: 12000, size: 200000, cost: null } });
    expect(
      screen.getByRole("img", { name: "12k / 200k tokens (6%)" }),
    ).toBeInTheDocument();
  });

  describe("/btw", () => {
    it("is listed first in the slash menu", async () => {
      setup();
      await userEvent.type(screen.getByLabelText("Message"), "/");
      const items = await screen.findAllByRole("menuitem");
      expect(items[0]).toHaveTextContent("/btw");
      expect(items[0]).toHaveTextContent(
        "Ask a side question without touching this chat",
      );
    });

    it("sends the question to onBtw instead of onSend", async () => {
      const { onBtw, onSend } = setup();
      await userEvent.type(
        screen.getByLabelText("Message"),
        "/btw hello{Enter}",
      );
      expect(onBtw).toHaveBeenCalledWith("hello");
      expect(onSend).not.toHaveBeenCalled();
      expect(screen.getByLabelText("Message")).toHaveValue("");
    });

    it("opens the panel with no question for a bare /btw", async () => {
      const { onBtw } = setup();
      await userEvent.type(screen.getByLabelText("Message"), "/btw");
      // The first Enter completes the command, the second submits it.
      await userEvent.keyboard("{Enter}{Enter}");
      expect(onBtw).toHaveBeenCalledWith("");
    });

    it("still asks a side question while a non-steerable turn runs", async () => {
      const { onBtw, onSend, onSteer } = setup({
        running: true,
        canSteer: false,
      });
      await userEvent.type(screen.getByLabelText("Message"), "/btw x{Enter}");
      expect(onBtw).toHaveBeenCalledWith("x");
      expect(onSend).not.toHaveBeenCalled();
      expect(onSteer).not.toHaveBeenCalled();
    });

    it("says so when the agent cannot fork", async () => {
      const { onBtw } = setup({ canFork: false });
      await userEvent.type(
        screen.getByLabelText("Message"),
        "/btw hello{Enter}",
      );
      expect(
        screen.getByText("This agent cannot open side questions"),
      ).toBeInTheDocument();
      expect(onBtw).not.toHaveBeenCalled();
    });
  });

  describe("auto-grow", () => {
    const stubHeight = (height: number) =>
      Object.defineProperty(screen.getByLabelText("Message"), "scrollHeight", {
        configurable: true,
        value: height,
      });

    it("grows with the content up to the bound and shrinks back", async () => {
      setup();
      const input = screen.getByLabelText("Message");
      stubHeight(500);
      await userEvent.type(input, "a");
      // 10 lines of the 20px fallback line height; 40% of jsdom's 768px is larger.
      expect(input.style.height).toBe("200px");
      stubHeight(60);
      await userEvent.type(input, "b");
      expect(input.style.height).toBe("60px");
      stubHeight(24);
      await userEvent.clear(input);
      expect(input.style.height).toBe("24px");
    });

    it("pins the box height around the measurement", () => {
      setup();
      const input = screen.getByLabelText("Message");
      const box = input.closest<HTMLElement>(".assistant__composer-box")!;
      Object.defineProperty(box, "offsetHeight", { value: 90 });
      stubHeight(40);
      const log: string[] = [];
      const watch = (
        style: CSSStyleDeclaration,
        prop: "height" | "minHeight",
      ) => {
        let value = style[prop];
        Object.defineProperty(style, prop, {
          configurable: true,
          get: () => value,
          set: (next: string) => {
            value = next;
            log.push(`${prop}=${next}`);
          },
        });
      };
      watch(box.style, "minHeight");
      watch(input.style, "height");
      fireEvent.change(input, { target: { value: "a" } });
      expect(log).toEqual([
        "minHeight=90px",
        "height=auto",
        "height=40px",
        "minHeight=",
      ]);
    });

    it("never exceeds 40% of the viewport", async () => {
      vi.spyOn(window, "innerHeight", "get").mockReturnValue(300);
      setup();
      const input = screen.getByLabelText("Message");
      stubHeight(500);
      await userEvent.type(input, "a");
      expect(input.style.height).toBe("120px");
    });
  });

  it("disables the controls while connecting", () => {
    setup({
      connecting: true,
      configOptions: [modelOption],
      modes: { currentModeId: "d", availableModes: [{ id: "d", name: "D" }] },
    });
    expect(screen.getByRole("button", { name: "Model: Opus" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Mode: D" })).toBeDisabled();
  });

  it("renders no config controls when the agent has none", () => {
    setup();
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /^Model:/ }),
    ).not.toBeInTheDocument();
  });

  describe("while running", () => {
    beforeEach(() => {
      vi.useFakeTimers();
    });
    afterEach(() => {
      vi.useRealTimers();
    });

    it("shows the elapsed time, advances it, and wires Stop", () => {
      const { onStop } = setup({
        running: true,
        turnStartedAt: Date.now(),
      });
      expect(screen.getByText("0:00")).toBeInTheDocument();
      expect(screen.queryByRole("button", { name: "Send" })).toBeNull();
      act(() => {
        vi.advanceTimersByTime(61_000);
      });
      expect(screen.getByText("1:01")).toBeInTheDocument();
      fireEvent.click(screen.getByRole("button", { name: "Stop" }));
      expect(onStop).toHaveBeenCalledTimes(1);
    });

    it("starts no interval while idle", () => {
      const setIntervalSpy = vi.spyOn(globalThis, "setInterval");
      setup({ running: false });
      expect(setIntervalSpy).not.toHaveBeenCalled();
      expect(screen.queryByRole("progressbar")).not.toBeInTheDocument();
    });

    it("clears the elapsed interval when the turn ends while mounted", () => {
      const setIntervalSpy = vi.spyOn(globalThis, "setInterval");
      const clearIntervalSpy = vi.spyOn(globalThis, "clearInterval");
      const { rerender } = setup({ running: true, turnStartedAt: Date.now() });
      const id = setIntervalSpy.mock.results[0].value;
      rerender({ running: false, turnStartedAt: null });
      expect(clearIntervalSpy).toHaveBeenCalledWith(id);
    });

    it("clears the elapsed interval on unmount", () => {
      const setIntervalSpy = vi.spyOn(globalThis, "setInterval");
      const clearIntervalSpy = vi.spyOn(globalThis, "clearInterval");
      const { unmount } = setup({ running: true, turnStartedAt: Date.now() });
      expect(setIntervalSpy).toHaveBeenCalledTimes(1);
      const id = setIntervalSpy.mock.results[0].value;
      unmount();
      expect(clearIntervalSpy).toHaveBeenCalledWith(id);
    });
  });
});
