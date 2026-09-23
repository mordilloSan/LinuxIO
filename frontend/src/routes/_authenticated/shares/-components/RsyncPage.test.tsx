import { beforeEach, describe, expect, it, vi } from "vitest";

import { linuxio, type RsyncStatus } from "@/api";
import {
  createTestQueryClient,
  fireEvent,
  renderWithTanStackRouter,
  screen,
  waitFor,
} from "@/test/render";

import RsyncPage from "./RsyncPage";

const mocks = vi.hoisted(() => ({ call: vi.fn() }));
vi.mock("@/api/calls", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/calls")>()),
  call: mocks.call,
}));

const stoppedStatus: RsyncStatus = {
  config: undefined,
  active: false,
  enabled: false,
  ssh_ready: false,
};

function renderPage(available = true, status: RsyncStatus = stoppedStatus) {
  const queryClient = createTestQueryClient();
  queryClient.setQueryData(linuxio.shares.get_rsync.queryKey, status);
  queryClient.setQueryData(
    linuxio.shares.get_rsync_ssh({ port: 22 }).queryKey,
    {
      available: true,
      port: 22,
    },
  );
  mocks.call.mockImplementation(async (route: string, request: unknown) => {
    if (route === "shares.get_rsync_ssh")
      return { available: true, ...(request as { port: number }) };
    if (route === "shares.save_rsync") {
      const { password: _password, ...config } = request as Record<
        string,
        unknown
      >;
      return { config, active: true, enabled: true, ssh_ready: true };
    }
    return stoppedStatus;
  });
  return {
    ...renderWithTanStackRouter(<RsyncPage />, {
      queryClient,
      capabilities: { rsyncAvailable: available },
    }),
    queryClient,
  };
}

describe("rsync backup setup", () => {
  beforeEach(() => mocks.call.mockReset());

  it("offers installation while rsync is absent", async () => {
    renderPage(false);
    expect(
      await screen.findByRole("link", {
        name: "Open Capabilities to install rsync",
      }),
    ).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "Save and start" }),
    ).not.toBeInTheDocument();
  });

  it("saves the module, chosen port and credentials, then displays TOS details", async () => {
    renderPage();
    await screen.findByRole("button", { name: "Save and start" });
    fireEvent.change(screen.getByLabelText(/Backup folder/), {
      target: { value: "/srv/backups" },
    });
    fireEvent.change(screen.getByLabelText(/TNAS IP address/), {
      target: { value: "192.168.1.249" },
    });
    fireEvent.change(screen.getByLabelText(/Backup password/), {
      target: { value: "strong-backup-password" },
    });
    fireEvent.change(screen.getByLabelText(/Module port/), {
      target: { value: "8873" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save and start" }));
    await waitFor(() =>
      expect(mocks.call).toHaveBeenCalledWith("shares.save_rsync", {
        module: "backup",
        path: "/srv/backups",
        nas_address: "192.168.1.249",
        port: 8873,
        username: "tnas",
        password: "strong-backup-password",
      }),
    );
    await waitFor(() => expect(screen.getByText("Running")).toBeVisible());
    expect(
      screen.getByRole("region", { name: "TOS connection details" }),
    ).toHaveTextContent("8873");
    expect(screen.getByLabelText(/Backup password/)).toHaveValue("");
    fireEvent.click(screen.getByRole("button", { name: "Stop and disable" }));
    await waitFor(() =>
      expect(mocks.call).toHaveBeenCalledWith("shares.stop_rsync", undefined),
    );
  });
  it("defaults to SSH port 22 and checks a custom port while the module is stopped", async () => {
    renderPage();
    const check = await screen.findByRole("button", {
      name: "Check SSH port 22",
    });
    fireEvent.click(check);
    await waitFor(() =>
      expect(mocks.call).toHaveBeenCalledWith(
        "shares.get_rsync_ssh",
        { port: 22 },
        { signal: expect.any(AbortSignal) },
      ),
    );
    expect(screen.getByLabelText(/SSH port/)).toHaveValue(22);
    fireEvent.change(screen.getByLabelText(/SSH port/), {
      target: { value: "9222" },
    });
    expect(
      screen.queryByText(/The local SSH listener responds/),
    ).not.toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", { name: "Check SSH port 9222" }),
    );
    await waitFor(() =>
      expect(mocks.call).toHaveBeenCalledWith(
        "shares.get_rsync_ssh",
        { port: 9222 },
        { signal: expect.any(AbortSignal) },
      ),
    );
    expect(
      screen.getByRole("region", { name: "SSH backup connection" }),
    ).toHaveTextContent("9222");
    await screen.findByText(/The local SSH listener responds/);
    fireEvent.change(screen.getByLabelText(/SSH port/), {
      target: { value: "65536" },
    });
    expect(
      screen.getByRole("button", { name: "Check SSH port 65536" }),
    ).toBeDisabled();
    expect(screen.getByText("Enter a port between 1 and 65535.")).toBeVisible();
    expect(
      mocks.call.mock.calls.some(
        ([route, request]) =>
          route === "shares.get_rsync_ssh" && request.port === 65536,
      ),
    ).toBe(false);
    expect(screen.getByText("Stopped")).toBeVisible();
    expect(
      mocks.call.mock.calls.some(([route]) => route === "shares.save_rsync"),
    ).toBe(false);
  });
  it("describes TOS's encryption mode from the running module", async () => {
    renderPage(true, {
      config: {
        module: "backup",
        path: "/srv/data",
        username: "tnas",
        nas_address: "192.168.1.249",
        port: 873,
      },
      active: true,
      enabled: true,
      ssh_ready: true,
    });
    const ssh = await screen.findByRole("region", {
      name: "SSH backup connection",
    });
    await waitFor(() => expect(ssh).toHaveTextContent("Connect from TOS"));
    expect(ssh).toHaveTextContent("A Linux account that can read /srv/data");
    expect(ssh).toHaveTextContent("rsync module encryption mode");
    expect(
      screen.queryByText(/is not managed by LinuxIO/),
    ).not.toBeInTheDocument();
  });
  it("warns when /etc/rsyncd.conf belongs to something else", async () => {
    renderPage(true, {
      config: {
        module: "backup",
        path: "/srv/data",
        username: "tnas",
        nas_address: "192.168.1.249",
        port: 873,
      },
      active: true,
      enabled: true,
      ssh_ready: false,
      ssh_error:
        "/etc/rsyncd.conf is not managed by LinuxIO, so TOS's encryption mode reads that file instead of this module",
    });
    expect(await screen.findByText(/is not managed by LinuxIO/)).toBeVisible();
  });
  it("refreshes daemon status after a partial failure and retains the error", async () => {
    renderPage();
    await screen.findByRole("button", { name: "Save and start" });
    mocks.call.mockImplementation(async (route: string) => {
      if (route === "shares.save_rsync")
        throw new Error("Configuration saved, but service failed");
      if (route === "shares.get_rsync_ssh")
        return { available: true, port: 22 };
      return stoppedStatus;
    });
    fireEvent.change(screen.getByLabelText(/Backup folder/), {
      target: { value: "/srv/backups" },
    });
    fireEvent.change(screen.getByLabelText(/TNAS IP address/), {
      target: { value: "192.168.1.249" },
    });
    fireEvent.change(screen.getByLabelText(/Backup password/), {
      target: { value: "test-backup-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save and start" }));
    await screen.findByText("Configuration saved, but service failed");
    await waitFor(() =>
      expect(mocks.call).toHaveBeenCalledWith("shares.get_rsync", undefined, {
        signal: expect.any(AbortSignal),
      }),
    );
    expect(screen.getByText("Stopped")).toBeVisible();
    expect(
      screen.getByText("Configuration saved, but service failed"),
    ).toBeVisible();
  });
});
