import { afterEach, describe, expect, it, vi } from "vitest";

import * as core from "@/api/linuxio-core";
import { testNetworkInterface } from "@/test/networkInterface";
import { render, screen, waitFor } from "@/test/render";

import CreateInterfaceButton from "./CreateInterfaceButton";

vi.mock("@iconify/react", () => ({
  Icon: () => <span aria-hidden="true" />,
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn(), warning: vi.fn() },
}));

describe("CreateInterfaceButton", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("sends the optional host and MTU with the create request", async () => {
    const creates: unknown[] = [];
    vi.spyOn(core, "request").mockImplementation(
      (_handler, command, request) => {
        if (command === "get_network_info") {
          return Promise.resolve([
            testNetworkInterface({ ipv4: ["192.168.1.2/24"], name: "eth0" }),
          ]);
        }
        if (command === "add_interface") creates.push(request);
        return Promise.resolve();
      },
    );
    const view = render(<CreateInterfaceButton interfaces={[]} />);

    await view.user.click(
      screen.getByRole("button", { name: "Create New Interface" }),
    );
    await waitFor(() =>
      expect(screen.getByRole("combobox", { name: "NIC" })).toHaveTextContent(
        "eth0 (192.168.1.2/24)",
      ),
    );
    await view.user.type(
      screen.getByLabelText("Endpoint host (optional)"),
      "vpn.example.org",
    );
    await view.user.type(screen.getByLabelText("MTU (optional)"), "1400");
    await view.user.click(
      screen.getByRole("button", { name: "Create Interface" }),
    );

    await waitFor(() => expect(creates).toHaveLength(1));
    expect(creates[0]).toMatchObject({
      name: "wg0",
      addresses: "10.10.20.0/24",
      listenPort: "51820",
      egressNic: "eth0",
      host: "vpn.example.org",
      mtu: "1400",
    });
  });
});
