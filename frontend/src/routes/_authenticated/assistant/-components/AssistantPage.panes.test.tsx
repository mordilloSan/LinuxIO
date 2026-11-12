import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { PaneView } from "@/api/acp";
import { render } from "@/test/render";

import { assistantStore } from "./assistant-store";
import AssistantPage from "./AssistantPage";

vi.mock("@/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api")>()),
  useStreamMux: () => ({ status: "open", isOpen: true, getStream: () => null }),
}));

beforeEach(() => {
  assistantStore.disconnect();
  vi.spyOn(assistantStore, "connect").mockImplementation(async () => {});
});

const setState = assistantStore._patch;

const pane = (patch: Partial<PaneView> = {}): PaneView => ({
  conn: 1,
  sessionId: "s",
  id: "lot-map",
  title: "Work map",
  plugin: "lot-map",
  tree: {
    type: "Box",
    props: { flexDirection: "column" },
    children: [{ type: "Text", props: {}, children: ["Nothing is running"] }],
  },
  ...patch,
});

const side = {
  sessionId: "side1",
  blocks: [{ kind: "agent" as const, id: "sa", text: "A side answer" }],
  status: "ready" as const,
  pending: null,
  open: true,
  error: null,
};

const panePanel = () =>
  screen.queryByRole("complementary", { name: "Work map" });

async function renderReady() {
  const view = render(<AssistantPage agent="claude" />);
  setState({ status: "ready", sessionId: "s", canFork: true });
  await screen.findByLabelText("Message");
  return view;
}

describe("AssistantPage plugin panes", () => {
  it("lays out exactly as before when there is no pane", async () => {
    const { container } = await renderReady();
    setState({ panes: [pane({ sessionId: "another-chat" })] });
    expect(panePanel()).toBeNull();
    expect(container.querySelector(".assistant__body")).not.toHaveClass(
      "assistant__body--split",
    );
    expect(screen.queryByRole("button", { name: /^Show / })).toBeNull();
  });

  it("floats the pane of the open chat beside it and leaves the chat centred", async () => {
    const { container } = await renderReady();
    setState({
      panes: [
        pane({ conn: 1, sessionId: "older", title: "Other chat" }),
        pane({ conn: 2 }),
      ],
    });
    const panel = await screen.findByRole("complementary", {
      name: "Work map",
    });
    expect(panel).toHaveClass("assistant-pane-card");
    expect(panel).toHaveTextContent("Nothing is running");
    // The card floats over the chat area; it never takes a column from it.
    expect(panel.parentElement).toHaveClass("assistant__body");
    expect(panel.parentElement).not.toHaveClass("assistant__body--split");
    expect(screen.queryByText("Other chat")).toBeNull();
    expect(container.querySelector(".assistant__main")).toBeInTheDocument();
  });

  it("follows the chat: another session hides it", async () => {
    await renderReady();
    setState({ panes: [pane()] });
    expect(await screen.findByText("Nothing is running")).toBeInTheDocument();
    setState({ sessionId: "s2" });
    await waitFor(() => expect(panePanel()).toBeNull());
  });

  it("makes way for an open side question", async () => {
    await renderReady();
    setState({ panes: [pane()], side });
    expect(await screen.findByText("A side answer")).toBeInTheDocument();
    expect(panePanel()).toBeNull();
    setState({ side: { ...side, open: false } });
    expect(
      await screen.findByRole("complementary", { name: "Work map" }),
    ).toBeInTheDocument();
    setState({ sideOpening: true, side: null });
    await waitFor(() => expect(panePanel()).toBeNull());
  });

  it("hides the pane and shows it again from the header", async () => {
    const { container } = await renderReady();
    setState({ panes: [pane()] });
    await userEvent.click(
      await screen.findByRole("button", { name: "Hide Work map" }),
    );
    expect(panePanel()).toBeNull();
    expect(container.querySelector(".assistant__body")).not.toHaveClass(
      "assistant__body--split",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Show Work map" }),
    );
    expect(
      await screen.findByRole("complementary", { name: "Work map" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Show Work map" })).toBeNull();
  });

  it("keeps another plugin's pane visible after hiding one", async () => {
    await renderReady();
    setState({ panes: [pane()] });
    await userEvent.click(
      await screen.findByRole("button", { name: "Hide Work map" }),
    );
    setState({
      panes: [pane({ id: "notes", title: "Notes", plugin: "notes" })],
    });
    expect(
      await screen.findByRole("complementary", { name: "Notes" }),
    ).toBeInTheDocument();
  });

  it("says when a pane has controls it cannot use", async () => {
    await renderReady();
    setState({
      panes: [
        pane({
          tree: {
            type: "Box",
            children: [{ type: "Button", props: { key: "go", label: "Go" } }],
          },
        }),
      ],
    });
    expect(
      await screen.findByText("This pane has controls LinuxIO cannot use yet."),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Go" })).toBeNull();
  });
});
