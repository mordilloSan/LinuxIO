import { describe, expect, it } from "vitest";

import { linuxio } from "@/api";
import type { VersionResponse } from "@/api/generated/linuxio-types";
import {
  createTestQueryClient,
  renderWithTanStackRouter,
  screen,
} from "@/test/render";

import AppUpdateBanner from "./AppUpdateBanner";

const renderBanner = (version: VersionResponse, privileged = true) => {
  const queryClient = createTestQueryClient();
  queryClient.setQueryData(linuxio.control.version.queryKey, version);
  // The marker shows that the router has rendered the tree.
  return renderWithTanStackRouter(
    <>
      <span>Page</span>
      <AppUpdateBanner />
    </>,
    {
      auth: { isAuthenticated: true, privileged },
      queryClient,
    },
  );
};

describe("AppUpdateBanner", () => {
  it("names the version PackageKit gives and opens the Updates page", async () => {
    renderBanner({ current_version: "v0.55.0", update_version: "0.56.0-1" });

    expect(
      await screen.findByText("LinuxIO 0.56.0-1 is available."),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Open Updates" })).toHaveAttribute(
      "href",
      "/updates",
    );
  });

  it("renders nothing without a pending update", async () => {
    renderBanner({ current_version: "v0.55.0" });

    expect(await screen.findByText("Page")).toBeInTheDocument();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("renders nothing to a session that cannot install the update", async () => {
    renderBanner(
      { current_version: "v0.55.0", update_version: "0.56.0" },
      false,
    );

    expect(await screen.findByText("Page")).toBeInTheDocument();
    expect(screen.queryByText(/is available/)).not.toBeInTheDocument();
  });

  it("stays closed for the version it was closed on", async () => {
    const { user } = renderBanner({
      current_version: "v0.55.0",
      update_version: "0.56.0",
    });

    await user.click(await screen.findByRole("button", { name: "Close" }));

    expect(screen.queryByText(/is available/)).not.toBeInTheDocument();
  });
});
