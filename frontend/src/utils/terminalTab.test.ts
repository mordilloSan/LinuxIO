import { afterEach, describe, expect, it, vi } from "vitest";

import {
  listenForTypedCommands,
  openTerminalTab,
  typeInTerminalTab,
} from "./terminalTab";

const stops: (() => void)[] = [];
const listen = (isUp = () => true) => {
  const take = vi.fn();
  stops.push(listenForTypedCommands(take, isUp));
  return take;
};
// Opens a tab as the chat does; `load` is that tab bringing its terminal up.
const openTab = (command: string) => {
  const tab = { closed: false, focus: vi.fn() };
  const open = vi
    .spyOn(window, "open")
    .mockReturnValue(tab as unknown as Window);
  expect(openTerminalTab(command)).toBe(true);
  const name = open.mock.calls[0][1] as string;
  expect(open).toHaveBeenCalledWith("/terminal", name);
  const load = () => {
    window.name = name;
    const take = listen();
    window.name = "";
    return take;
  };
  return { tab, load };
};

afterEach(() => {
  for (const stop of stops.splice(0)) stop();
  window.name = "";
  vi.restoreAllMocks();
});

describe("terminal tab handoff", () => {
  it("finds no terminal when no tab listens", async () => {
    await expect(typeInTerminalTab("uptime")).resolves.toBeNull();
  });

  it("gives the command to a terminal that is up in another tab", async () => {
    const take = listen();
    await expect(typeInTerminalTab("uptime")).resolves.toBe("typed");
    await vi.waitFor(() => expect(take).toHaveBeenCalledWith("uptime"));
  });

  it("gives the command to one terminal when several are up", async () => {
    const takes = [listen(), listen()];
    await expect(typeInTerminalTab("uptime")).resolves.toBe("typed");
    await vi.waitFor(() =>
      expect(takes[0].mock.calls.length + takes[1].mock.calls.length).toBe(1),
    );
    // Time for a second delivery, were there one.
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(takes[0].mock.calls.length + takes[1].mock.calls.length).toBe(1);
  });

  it("stops answering once the terminal is left", async () => {
    const take = vi.fn();
    listenForTypedCommands(take, () => true)();
    await expect(typeInTerminalTab("uptime")).resolves.toBeNull();
    expect(take).not.toHaveBeenCalled();
  });

  it("is not found while its shell has ended", async () => {
    const take = listen(() => false);
    await expect(typeInTerminalTab("uptime")).resolves.toBeNull();
    expect(take).not.toHaveBeenCalled();
  });

  it("hands the command to the tab it opened once that terminal is up", async () => {
    const { tab, load } = openTab("uptime");
    // Another terminal coming up meanwhile is not the tab it opened.
    const other = listen();
    const take = load();
    await vi.waitFor(() => expect(take).toHaveBeenCalledWith("uptime"));
    expect(other).not.toHaveBeenCalled();
    expect(tab.focus).not.toHaveBeenCalled();
  });

  it("brings the tab it opened forward only when that tab took the command", async () => {
    const { tab, load } = openTab("uptime");
    const take = load();
    await vi.waitFor(() => expect(take).toHaveBeenCalledWith("uptime"));

    await expect(typeInTerminalTab("df -h")).resolves.toBe("typed");
    await vi.waitFor(() => expect(take).toHaveBeenCalledWith("df -h"));
    expect(tab.focus).toHaveBeenCalledTimes(1);

    // Its terminal gone, another tab's takes the command: it stays behind.
    stops.splice(0).forEach((stop) => stop());
    const other = listen();
    await expect(typeInTerminalTab("free -h")).resolves.toBe("typed");
    await vi.waitFor(() => expect(other).toHaveBeenCalledWith("free -h"));
    expect(tab.focus).toHaveBeenCalledTimes(1);
  });

  it("hands a tab that is still loading the newest command only", async () => {
    const { tab, load } = openTab("uptime");
    // No terminal answers yet, and the chat must not give up on the new tab.
    await expect(typeInTerminalTab("df -h")).resolves.toBe("opening");
    expect(tab.focus).toHaveBeenCalled();

    const take = load();
    await vi.waitFor(() => expect(take).toHaveBeenCalledWith("df -h"));
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(take).toHaveBeenCalledTimes(1);
  });

  it("reports a tab the browser refused", () => {
    vi.spyOn(window, "open").mockReturnValue(null);
    expect(openTerminalTab("uptime")).toBe(false);
  });
});
