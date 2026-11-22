// Hands a command from the Assistant to a terminal open in another browser
// tab of this server, so the chat stays where it is. The command travels on a
// BroadcastChannel, which only pages of this origin can post to: like router
// history state, a crafted link cannot choose it. The terminal still reduces
// what it receives to a typed command (typedCommand.ts) and never runs it.

const CHANNEL = "linuxio-terminal-command";
// A terminal tab answers from a message handler, which a background tab still
// runs at once; no answer in this time means there is none.
const FIND_WAIT_MS = 200;
// How long a tab this page opened has to load and start its shell.
const NEW_TAB_WAIT_MS = 30_000;

type Message =
  // The Assistant asks which tabs have a shell terminal up.
  | { type: "find"; ask: string }
  // A terminal tab answers a find, or (ask null) says it has just come up.
  | { type: "here"; ask: string | null; tab: string }
  // The Assistant gives its command to one of them.
  | { type: "type"; tab: string; command: string };

// The window name of a tab the Assistant opened, which is also the name that
// tab answers by: the Assistant knows its own tab among several terminals,
// through reloads of that tab too.
const OPENED_TAB_NAME = "linuxio-terminal-";

/** Where a command went: a terminal that is up, one still loading, or none. */
export type TerminalTabResult = "typed" | "opening" | null;

const newId = () => `${Date.now()}-${Math.random().toString(36).slice(2)}`;

function openChannel(): BroadcastChannel | null {
  return typeof BroadcastChannel === "undefined"
    ? null
    : new BroadcastChannel(CHANNEL);
}

function messageFrom(data: unknown): Partial<Message> {
  return typeof data === "object" && data !== null ? data : {};
}

// The tab this page opened, the only one a page may bring to the front, and
// the name it answers by.
let openedTab: Window | null = null;
let openedTabName: string | null = null;
// The command a tab that is still loading will be handed, and what ends the
// wait for it.
let loadingTab: { command: string; stop: () => void } | null = null;

/**
 * The terminal page's side: answers the Assistant's search while its shell is
 * up (`isUp`) and passes on a command addressed to this tab. The command is
 * untrusted until the caller has validated it. Returns what stops listening.
 */
export function listenForTypedCommands(
  take: (command: unknown) => void,
  isUp: () => boolean,
): () => void {
  const channel = openChannel();
  if (!channel) return () => {};
  const tab = window.name.startsWith(OPENED_TAB_NAME) ? window.name : newId();
  channel.onmessage = (event) => {
    const message = messageFrom(event.data);
    if (message.type === "find" && typeof message.ask === "string") {
      if (isUp()) channel.postMessage({ type: "here", ask: message.ask, tab });
    } else if (message.type === "type" && message.tab === tab) {
      take(message.command);
    }
  };
  // For the Assistant that opened this tab and waits to hand its command over.
  channel.postMessage({ type: "here", ask: null, tab });
  return () => channel.close();
}

/**
 * Hands the command to a terminal that is already open in another tab, and
 * brings that tab to the front when this page opened it. Resolves null when
 * no tab has a terminal up.
 */
export function typeInTerminalTab(command: string): Promise<TerminalTabResult> {
  // The tab this page opened is not listening yet: it gets this command in
  // place of the one it was opened for, as a prompt holds only one.
  if (loadingTab && openedTab && !openedTab.closed) {
    loadingTab.command = command;
    openedTab.focus();
    return Promise.resolve("opening");
  }
  const channel = openChannel();
  if (!channel) return Promise.resolve(null);
  const ask = newId();
  return new Promise((resolve) => {
    const finish = (found: TerminalTabResult) => {
      window.clearTimeout(timer);
      channel.close();
      resolve(found);
    };
    const timer = window.setTimeout(() => finish(null), FIND_WAIT_MS);
    channel.onmessage = (event) => {
      const message = messageFrom(event.data);
      if (message.type !== "here" || message.ask !== ask) return;
      if (typeof message.tab !== "string") return;
      // The first tab to answer takes it; the others hear nothing more.
      channel.postMessage({ type: "type", tab: message.tab, command });
      // A newer command has gone to a terminal: a tab the user closed while
      // it loaded has nothing left to wait for.
      loadingTab?.stop();
      // Only the tab that took the command is worth bringing forward.
      if (message.tab === openedTabName && openedTab && !openedTab.closed)
        openedTab.focus();
      finish("typed");
    };
    channel.postMessage({ type: "find", ask });
  });
}

/**
 * Opens the terminal in a new tab and hands it the command once its shell is
 * up. Returns false when the browser refused the tab.
 */
export function openTerminalTab(command: string): boolean {
  const channel = openChannel();
  if (!channel) return false;
  // With an opener, so this page can bring the tab to the front later, and
  // under a name of its own, so only that tab is handed the command.
  const name = `${OPENED_TAB_NAME}${newId()}`;
  const tab = window.open("/terminal", name);
  if (!tab) {
    channel.close();
    return false;
  }
  openedTab = tab;
  openedTabName = name;
  loadingTab?.stop();
  const loading = {
    command,
    stop: () => {
      window.clearTimeout(timer);
      channel.close();
      if (loadingTab === loading) loadingTab = null;
    },
  };
  const timer = window.setTimeout(loading.stop, NEW_TAB_WAIT_MS);
  loadingTab = loading;
  channel.onmessage = (event) => {
    const message = messageFrom(event.data);
    if (message.type !== "here" || message.tab !== name) return;
    channel.postMessage({
      type: "type",
      tab: name,
      command: loading.command,
    });
    loading.stop();
  };
  return true;
}
