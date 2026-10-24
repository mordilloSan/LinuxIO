import type * as acp from "@agentclientprotocol/sdk";
import { fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { render } from "@/test/render";

import PermissionCard from "./PermissionCard";

const options: acp.PermissionOption[] = [
  { optionId: "yes", name: "Yes", kind: "allow_once" },
  {
    optionId: "always",
    name: "Yes, and don't ask again for gh auth * commands",
    kind: "allow_always",
  },
  { optionId: "no", name: "No", kind: "reject_once" },
];

function setup(toolCall: Partial<acp.ToolCallUpdate> = {}) {
  const onAnswer = vi.fn();
  render(
    <PermissionCard
      onAnswer={onAnswer}
      request={{
        sessionId: "s",
        options,
        toolCall: {
          toolCallId: "t1",
          kind: "execute",
          title: "gh auth status 2>&1",
          rawInput: {
            command: "gh auth status 2>&1",
            description: "Check GitHub CLI authentication",
          },
          ...toolCall,
        },
      }}
    />,
  );
  return { onAnswer };
}

describe("PermissionCard", () => {
  it("asks a question, shows the command and lists numbered choices", () => {
    setup();
    const card = screen.getByRole("alertdialog", { name: "Run this command?" });
    expect(card).toHaveTextContent("Check GitHub CLI authentication");
    expect(
      card.querySelector(".assistant__permission-command"),
    ).toHaveTextContent("gh auth status 2>&1");
    const buttons = within(card).getAllByRole("button");
    expect(buttons.map((b) => b.getAttribute("aria-keyshortcuts"))).toEqual([
      "1",
      "2",
      "3",
    ]);
    // The number badge is decoration; the name is the agent's wording.
    expect(
      within(card).getByRole("button", { name: "Yes" }),
    ).toBeInTheDocument();
  });

  it("puts focus on the first choice so the keyboard answers at once", () => {
    setup();
    expect(screen.getByRole("button", { name: "Yes" })).toHaveFocus();
  });

  it("answers with the clicked choice", async () => {
    const { onAnswer } = setup();
    await userEvent.click(
      screen.getByRole("button", { name: /don't ask again/ }),
    );
    expect(onAnswer).toHaveBeenCalledWith("always");
  });

  it("picks a choice by its number", () => {
    const { onAnswer } = setup();
    fireEvent.keyDown(screen.getByRole("button", { name: "Yes" }), {
      key: "3",
    });
    expect(onAnswer).toHaveBeenCalledWith("no");
  });

  it("moves between choices with the arrow keys", () => {
    setup();
    const yes = screen.getByRole("button", { name: "Yes" });
    fireEvent.keyDown(yes, { key: "ArrowDown" });
    expect(
      screen.getByRole("button", { name: /don't ask again/ }),
    ).toHaveFocus();
    fireEvent.keyDown(document.activeElement as Element, { key: "ArrowUp" });
    fireEvent.keyDown(document.activeElement as Element, { key: "ArrowUp" });
    expect(screen.getByRole("button", { name: "No" })).toHaveFocus();
  });

  it("takes the agent's no on Escape without letting it stop the turn", () => {
    const { onAnswer } = setup();
    const outside = vi.fn();
    document.addEventListener("keydown", outside);
    fireEvent.keyDown(screen.getByRole("button", { name: "Yes" }), {
      key: "Escape",
    });
    document.removeEventListener("keydown", outside);
    expect(onAnswer).toHaveBeenCalledWith("no");
    expect(outside).not.toHaveBeenCalled();
  });

  it("shows a diff instead of a command for an edit", () => {
    setup({
      kind: "edit",
      rawInput: undefined,
      content: [
        { type: "diff", path: "/etc/hosts", oldText: "a\n", newText: "b\n" },
      ],
    });
    const card = screen.getByRole("alertdialog", { name: "Make this edit?" });
    expect(card.querySelector(".assistant-diff__path")).toHaveTextContent(
      "/etc/hosts",
    );
    expect(card.querySelector(".assistant__permission-command")).toBeNull();
  });

  it("falls back to the tool title and a generic question", () => {
    setup({ kind: undefined, rawInput: undefined, title: "Do a thing" });
    const card = screen.getByRole("alertdialog", { name: "Allow this?" });
    expect(
      card.querySelector(".assistant__permission-command"),
    ).toHaveTextContent("Do a thing");
  });
});
