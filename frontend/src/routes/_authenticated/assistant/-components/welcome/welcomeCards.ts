import type { Alert } from "@/api";

export interface WelcomeCard {
  key: string;
  icon: string;
  title: string;
  hint: string;
  /** What is sent to the agent when the card is clicked. */
  prompt: string;
}

const MAX_CARDS = 4;
const MAX_TITLE = 200;

// The severities that need someone to look; "info" reports what went well.
const RANK: Record<string, number> = { error: 0, warning: 1 };

const STANDING: WelcomeCard[] = [
  {
    key: "health",
    icon: "mdi:stethoscope",
    title: "Run a full health check",
    hint: "Disks, services, memory and recent errors",
    prompt:
      "Run a health check of this server: disk space and disk health, failed services, memory and load, and errors in the logs since the last boot. List what needs attention first.",
  },
  {
    key: "load",
    icon: "mdi:chip",
    title: "What is using CPU and memory?",
    hint: "The busiest processes right now",
    prompt:
      "Show which processes are using the most CPU and memory right now, and say whether any of it looks wrong.",
  },
  {
    key: "services",
    icon: "mdi:cog-outline",
    title: "Find failed services",
    hint: "systemd units that did not start",
    prompt:
      "List the systemd units that have failed, explain why each one failed and how to fix it.",
  },
  {
    key: "errors",
    icon: "mdi:text-box-search-outline",
    title: "Explain recent errors",
    hint: "The logs since the last boot",
    prompt:
      "Look at the errors in the logs since the last boot and explain the ones that matter.",
  },
];

// An alert's title and message can carry text from outside the host (a login
// name someone tried, a script's output). The message is only shown; the
// prompt quotes the title alone, on one line, and the agent looks the rest up.
const alertCard = (alert: Alert): WelcomeCard => {
  const title = alert.title.replace(/\s+/g, " ").trim().slice(0, MAX_TITLE);
  return {
    key: `alert:${alert.id}`,
    icon:
      alert.severity === "error"
        ? "mdi:alert-circle-outline"
        : "mdi:alert-outline",
    title,
    hint: alert.message ?? alert.source,
    prompt: `This server raised the alert "${title}". Find out what happened, check the current state and tell me what to do about it.`,
  };
};

/**
 * The cards of a new chat's welcome screen: the alerts that need a look
 * first (errors, then warnings, newest first), then the standing prompts,
 * four in all. alertCount is every alert that needs a look, shown or not.
 */
export function welcomeCards(alerts: Alert[]): {
  cards: WelcomeCard[];
  alertCount: number;
} {
  const open = alerts
    .filter((alert) => alert.severity in RANK)
    .toSorted(
      (a, b) =>
        RANK[a.severity] - RANK[b.severity] ||
        b.lastOccurrence.localeCompare(a.lastOccurrence),
    );
  return {
    cards: [...open.map(alertCard), ...STANDING].slice(0, MAX_CARDS),
    alertCount: open.length,
  };
}
