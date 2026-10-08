import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { render } from "@/test/render";

import { assistantStore, type SideSession } from "./assistant-store";
import SidePanel from "./SidePanel";

const side = (patch: Partial<SideSession> = {}): SideSession => ({
  sessionId: "side1",
  blocks: [{ kind: "agent", id: "a", text: "Side reply" }],
  status: "ready",
  pending: null,
  open: true,
  error: null,
  ...patch,
});

afterEach(() => vi.restoreAllMocks());

describe("SidePanel", () => {
  it("renders the side transcript", () => {
    render(<SidePanel side={side()} />);
    expect(screen.getByText("Side reply")).toBeInTheDocument();
    expect(screen.getByText("Side question")).toBeInTheDocument();
  });

  it("sends on Enter, clears the field and keeps Shift+Enter for newlines", async () => {
    const sendSide = vi.spyOn(assistantStore, "sendSide").mockResolvedValue();
    render(<SidePanel side={side()} />);
    const input = screen.getByLabelText("Side question message");
    await userEvent.type(input, "why?{Shift>}{Enter}{/Shift}");
    expect(sendSide).not.toHaveBeenCalled();
    await userEvent.clear(input);
    await userEvent.type(input, "why?{Enter}");
    expect(sendSide).toHaveBeenCalledWith("why?");
    expect(input).toHaveValue("");
  });

  it("offers Stop instead of Send while running", async () => {
    const stopSide = vi
      .spyOn(assistantStore, "stopSide")
      .mockImplementation(() => {});
    render(<SidePanel side={side({ status: "running" })} />);
    expect(
      screen.queryByRole("button", { name: "Send side question" }),
    ).toBeNull();
    await userEvent.click(
      screen.getByRole("button", { name: "Stop side question" }),
    );
    expect(stopSide).toHaveBeenCalled();
  });

  it("discards and hides through the header icons", async () => {
    const discardSide = vi
      .spyOn(assistantStore, "discardSide")
      .mockResolvedValue();
    const hideSide = vi
      .spyOn(assistantStore, "hideSide")
      .mockImplementation(() => {});
    render(<SidePanel side={side()} />);
    await userEvent.click(
      screen.getByRole("button", { name: "Discard side question" }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Close side question" }),
    );
    expect(discardSide).toHaveBeenCalled();
    expect(hideSide).toHaveBeenCalled();
  });

  it("answers the side permission card and shows errors", async () => {
    const answer = vi
      .spyOn(assistantStore, "answerSidePermission")
      .mockImplementation(() => {});
    render(
      <SidePanel
        side={side({
          error: "fork failed",
          pending: {
            request: {
              sessionId: "side1",
              toolCall: { toolCallId: "t", title: "rm x" },
              options: [
                { optionId: "ok", name: "Allow once", kind: "allow_once" },
              ],
            },
            resolve: vi.fn(),
          },
        })}
      />,
    );
    expect(screen.getByText("fork failed")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Allow once" }));
    expect(answer).toHaveBeenCalledWith("ok");
  });
});
