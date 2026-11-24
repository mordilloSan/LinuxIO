import { describe, expect, it, vi } from "vitest";

import type { WireGuardCheckResult } from "@/api";
import { render, screen, waitFor, within } from "@/test/render";

import CheckConnectionDialog from "./CheckConnectionDialog";

vi.mock("@iconify/react", () => ({
  Icon: () => <span aria-hidden="true" />,
}));

const TITLES = [
  "Connects to the server",
  "Traffic passes through the tunnel",
  "Reaches the network behind the server",
  "Names resolve",
];

const allOk: WireGuardCheckResult = {
  steps: [
    { id: "handshake", status: "ok" },
    { id: "tunnel", status: "ok", target: "10.99.0.1" },
    { id: "beyond", status: "ok", target: "192.168.122.1" },
    { id: "dns", status: "ok", target: "192.168.122.1" },
  ],
};

const renderDialog = (
  props: Partial<Parameters<typeof CheckConnectionDialog>[0]> = {},
) => {
  const onClose = vi.fn();
  const onRunAgain = vi.fn();
  const view = render(
    <CheckConnectionDialog
      error={null}
      interfaceName="wg0"
      onClose={onClose}
      onRunAgain={onRunAgain}
      open
      port={51820}
      result={undefined}
      running={false}
      {...props}
    />,
  );
  return { ...view, onClose, onRunAgain };
};

/** The status of the row with this title, as its icon names it. */
const statusOf = (title: string) => {
  const row = screen.getByText(title).closest("li");
  if (!row) throw new Error(`no row for ${title}`);
  return within(row).getByRole("img").getAttribute("aria-label");
};

describe("CheckConnectionDialog", () => {
  it("shows every step as passed, and what it cannot see", async () => {
    const { user, onRunAgain, onClose } = renderDialog({ result: allOk });

    await waitFor(() =>
      expect(screen.getByText("Connects to the server")).toBeVisible(),
    );
    for (const title of TITLES) expect(statusOf(title)).toBe("Passed");
    expect(
      screen.getByText(
        "Checked from this server. It cannot tell whether your router forwards UDP port 51820 to it.",
      ),
    ).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent(
      "This VPN works: a client connects, reaches your network and resolves names.",
    );

    await user.click(screen.getByRole("button", { name: "Run again" }));
    expect(onRunAgain).toHaveBeenCalledOnce();
    await user.click(screen.getByRole("button", { name: "Close" }));
    expect(onClose).toHaveBeenCalledOnce();
  });

  it("explains a failed step and why the later ones were skipped", async () => {
    renderDialog({
      result: {
        steps: [
          {
            id: "handshake",
            status: "failed",
            detail: "no handshake on UDP port 51820 of 127.0.0.1 within 5s",
          },
          {
            id: "tunnel",
            status: "skipped",
            detail: "no handshake with the server",
          },
          {
            id: "beyond",
            status: "skipped",
            detail: "no handshake with the server",
          },
          {
            id: "dns",
            status: "skipped",
            detail: "no handshake with the server",
          },
        ],
      },
    });

    await waitFor(() =>
      expect(screen.getByText("Connects to the server")).toBeVisible(),
    );
    expect(statusOf("Connects to the server")).toBe("Failed");
    expect(screen.getByRole("alert")).toHaveTextContent("1 step of 4 failed");
    expect(
      screen.getByText(
        "The server did not answer a handshake on port 51820. A local firewall may be blocking it.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByText("no handshake on UDP port 51820 of 127.0.0.1 within 5s"),
    ).toBeInTheDocument();
    for (const title of TITLES.slice(1))
      expect(statusOf(title)).toBe("Skipped");
    expect(
      screen.getAllByText("Skipped: No handshake with the server."),
    ).toHaveLength(3);
  });

  it("names the DNS server that did not answer", async () => {
    renderDialog({
      result: {
        steps: [
          { id: "handshake", status: "ok" },
          { id: "tunnel", status: "ok", target: "10.99.0.1" },
          {
            id: "beyond",
            status: "failed",
            target: "192.168.122.1",
            detail: "no echo reply from 192.168.122.1 within 2s",
          },
          {
            id: "dns",
            status: "failed",
            target: "192.168.122.1",
            detail: "no answer from 192.168.122.1 port 53 within 2s",
          },
        ],
      },
    });

    await waitFor(() =>
      expect(screen.getByText("Names resolve")).toBeVisible(),
    );
    expect(statusOf("Reaches the network behind the server")).toBe("Failed");
    expect(screen.getByRole("alert")).toHaveTextContent("2 steps of 4 failed");
    expect(
      screen.getByText(/IPv4 forwarding, the NAT rule or the firewall/),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        "The DNS server 192.168.122.1 did not answer through the tunnel, so clients would connect but not open websites by name.",
      ),
    ).toBeInTheDocument();
  });

  it("says when nothing failed but some steps could not be checked", async () => {
    renderDialog({
      result: {
        steps: [
          { id: "handshake", status: "ok" },
          { id: "tunnel", status: "ok", target: "10.99.0.1" },
          {
            id: "beyond",
            status: "skipped",
            target: "192.168.122.1",
            detail:
              "the gateway 192.168.122.1 does not answer a ping from this server either, so this step cannot be judged",
          },
          { id: "dns", status: "ok", target: "10.99.0.1" },
        ],
      },
    });

    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent(
        "Nothing failed, but 1 step could not be checked.",
      ),
    );
    expect(statusOf("Reaches the network behind the server")).toBe("Skipped");
  });

  it("shows an error from the route", async () => {
    renderDialog({ error: new Error("interface wg0 is down") });

    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent(
        "interface wg0 is down",
      ),
    );
    for (const title of TITLES) expect(statusOf(title)).toBe("Not run");
  });

  it("holds itself while the check runs", async () => {
    renderDialog({ result: allOk, running: true });

    await waitFor(() =>
      expect(screen.getByText("Connects to the server")).toBeVisible(),
    );
    // A result from before is not shown as this run's.
    for (const title of TITLES) expect(statusOf(title)).toBe("Checking");
    expect(screen.getByRole("button", { name: "Close" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Checking…" })).toBeDisabled();
  });
});
