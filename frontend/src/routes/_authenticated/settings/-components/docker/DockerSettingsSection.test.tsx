import { act, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { DockerSettings } from "@/api";
import { bridgeConfigQueryKey } from "@/api/config-query";
import { cacheUserId } from "@/api/user-scope";
import { ConfigContext } from "@/contexts/ConfigContext";
import {
  buildTestConfigSnapshots,
  createAuthContextValue,
  createConfigContextValue,
  createTestQueryClient,
  render,
} from "@/test/render";

const mocks = vi.hoisted(() => ({
  toast: {
    error: vi.fn(),
    info: vi.fn(),
    success: vi.fn(),
  },
  updateConfig: vi.fn(),
}));

vi.mock("@iconify/react", () => ({ Icon: () => <span /> }));
vi.mock("sonner", () => ({ toast: mocks.toast }));
vi.mock("@/api/calls", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/calls")>()),
  call: vi.fn(async (route: string) => {
    if (route === "docker.validate_stack_directory") {
      return { exists: true, valid: true };
    }
    throw new Error("the bridge did not answer");
  }),
}));
// The blocks below the folders have tests of their own.
vi.mock("./DockerAutoUpdateSettingsSection", () => ({ default: () => null }));
vi.mock("./DockerDaemonSettingsSection", () => ({ default: () => null }));
vi.mock("./DockerHostIPWaitSetting", () => ({ default: () => null }));
vi.mock("./useDockerAutoUpdateState", () => ({
  useDockerAutoUpdateState: () => ({}),
}));

import DockerSettingsSection from "./DockerSettingsSection";

function renderSection(docker: Partial<DockerSettings> = {}) {
  const queryClient = createTestQueryClient();
  const { bridge } = buildTestConfigSnapshots();
  queryClient.setQueryData(
    bridgeConfigQueryKey(cacheUserId(createAuthContextValue().user)),
    { ...bridge, docker: { ...bridge.docker, ...docker } },
  );
  return render(
    <ConfigContext.Provider
      value={createConfigContextValue({ updateConfig: mocks.updateConfig })}
    >
      <DockerSettingsSection />
    </ConfigContext.Provider>,
    { queryClient, seedConfig: false },
  );
}

const pathValues = () =>
  screen
    .getAllByRole("textbox")
    .map((field) => (field as HTMLInputElement).value);

describe("DockerSettingsSection folders", () => {
  beforeEach(() => {
    mocks.toast.error.mockClear();
    mocks.toast.info.mockClear();
    mocks.toast.success.mockClear();
    mocks.updateConfig.mockClear();
  });

  // The mocked updateConfig stores nothing, which is what the page shows once
  // a refused save has been read back.
  it("keeps what was typed and says nothing until the bridge confirms the save", async () => {
    const { user } = renderSection({ folders: ["/srv/stacks"] });

    await user.clear(screen.getByLabelText("Path"));
    await user.type(screen.getByLabelText("Path"), "/srv/other/");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(mocks.updateConfig).toHaveBeenCalledTimes(1));
    expect(mocks.updateConfig).toHaveBeenCalledWith(
      { docker: { folders: ["/srv/other"] } },
      expect.any(Function),
    );
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Save" })).toBeEnabled(),
    );
    expect(pathValues()).toEqual(["/srv/other/"]);
    expect(mocks.toast.success).not.toHaveBeenCalled();

    const onSaved = mocks.updateConfig.mock.calls[0][1] as () => void;
    act(() => onSaved());

    expect(pathValues()).toEqual(["/srv/stacks"]);
    expect(mocks.toast.success).toHaveBeenCalledWith("Docker folders saved.");
  });

  it("keeps an edit made while the save was on its way", async () => {
    const { user } = renderSection({ folders: ["/srv/stacks"] });

    await user.clear(screen.getByLabelText("Path"));
    await user.type(screen.getByLabelText("Path"), "/srv/other");
    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(mocks.updateConfig).toHaveBeenCalledTimes(1));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Save" })).toBeEnabled(),
    );
    await user.type(screen.getByLabelText("Path"), "2");

    const onSaved = mocks.updateConfig.mock.calls[0][1] as () => void;
    act(() => onSaved());

    expect(pathValues()).toEqual(["/srv/other2"]);
  });
});
