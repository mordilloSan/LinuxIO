import { beforeEach, describe, expect, it, vi } from "vitest";

import { render, screen, waitFor } from "@/test/render";

import ComposeStacksPage from "./ComposeStacksPage";

const mocks = vi.hoisted(() => ({
  readText: vi.fn(),
  resolveComposeFilePath: vi.fn(),
  toastError: vi.fn(),
  toastSuccess: vi.fn(),
  uploadContent: vi.fn(),
}));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useSuspenseQuery: () => ({ data: [], refetch: vi.fn() }),
  };
});

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    call: (_route: unknown, args: { path: string }) =>
      mocks.readText(args.path),
    uploadContent: mocks.uploadContent,
    useCallMutation: (route: unknown) => ({
      mutateAsync:
        route === actual.linuxio.docker.get_compose_file_path
          ? mocks.resolveComposeFilePath
          : vi.fn(),
    }),
  };
});

vi.mock("@/hooks/useConfig", () => ({
  useDockerSettings: () => ({ folders: [] }),
}));

vi.mock("@/hooks/useUploadChunkSize", () => ({
  useUploadChunkSize: () => 1024,
}));

vi.mock("@/hooks/useScopedToast", () => ({
  useScopedToast: () => ({
    error: mocks.toastError,
    success: mocks.toastSuccess,
  }),
}));

// The editor is reduced to its save callback: the test drives exactly what
// the real dialog hands the page when the user saves a stack with a .env.
vi.mock("./ComposeEditorDialog", () => ({
  default: ({
    filePath,
    onSave,
    open,
  }: {
    filePath: string;
    onSave: (
      content: string,
      stackName: string,
      filePath: string,
      envContent?: string,
    ) => Promise<boolean>;
    open: boolean;
  }) => (
    <>
      {open ? <span>Editing {filePath}</span> : null}
      <button
        onClick={() =>
          void onSave("services: {}\n", "web", filePath, "KEY=1\n")
        }
        type="button"
      >
        Save stack
      </button>
    </>
  ),
}));

// The list and the setup dialog are reduced to the callbacks that open the
// editor on an existing stack and on a new one in a chosen folder.
vi.mock("./ComposeList", () => ({
  default: ({
    onEdit,
  }: {
    onEdit: (projectName: string, configPath: string) => void;
  }) => (
    <button
      onClick={() => onEdit("web", "/home/alice/docker/web/docker-compose.yml")}
      type="button"
    >
      Edit stack
    </button>
  ),
}));
vi.mock("./StackSetupDialog", () => ({
  default: ({
    onConfirm,
  }: {
    onConfirm: (stackName: string, workingDir: string) => void;
  }) => (
    <button
      onClick={() => onConfirm("web", "/home/alice/docker/web")}
      type="button"
    >
      Set up stack
    </button>
  ),
}));
vi.mock("./ComposeOperationDialog", () => ({ default: () => null }));
vi.mock("./ComposePostSaveDialog", () => ({ default: () => null }));
vi.mock("./DeleteStackDialog", () => ({ default: () => null }));

const existingFiles: Record<string, { content: string; version: string }> = {
  "/home/alice/docker/web/.env": { content: "KEY=0\n", version: "env-v1" },
  "/home/alice/docker/web/docker-compose.yml": {
    content: "services: {}\n",
    version: "compose-v1",
  },
};

describe("ComposeStacksPage save", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.resolveComposeFilePath.mockResolvedValue({
      path: "/home/alice/docker/web/docker-compose.yml",
    });
    mocks.uploadContent.mockResolvedValue(undefined);
    mocks.readText.mockImplementation((path: string) => {
      const file = existingFiles[path];
      return file
        ? Promise.resolve({ ...file, canSave: true })
        : Promise.reject(new Error("not found"));
    });
  });

  it("creates the compose file and a new .env as the session user, the .env private and never overwritten", async () => {
    const { user } = render(<ComposeStacksPage />);

    await user.click(screen.getByRole("button", { name: "Save stack" }));

    await waitFor(() => expect(mocks.uploadContent).toHaveBeenCalledTimes(2));
    const [composeCall, envCall] = mocks.uploadContent.mock.calls;
    expect(composeCall[0]).toBe("/home/alice/docker/web/docker-compose.yml");
    expect(composeCall[2]).toMatchObject({ owner: "session" });
    expect(composeCall[2]).not.toHaveProperty("private");
    expect(envCall[0]).toBe("/home/alice/docker/web/.env");
    expect(envCall[2]).toMatchObject({ owner: "session", private: true });
    expect(envCall[2].overwrite).toBeFalsy();
    expect(envCall[2].expectedVersion).toBeUndefined();
  });

  it("replaces an existing .env only at the version the editor loaded", async () => {
    const { user } = render(<ComposeStacksPage />);

    await user.click(screen.getByRole("button", { name: "Set up stack" }));
    await screen.findByText(
      "Editing /home/alice/docker/web/docker-compose.yml",
    );
    await user.click(screen.getByRole("button", { name: "Save stack" }));

    await waitFor(() => expect(mocks.uploadContent).toHaveBeenCalledTimes(2));
    const [, envCall] = mocks.uploadContent.mock.calls;
    expect(envCall[0]).toBe("/home/alice/docker/web/.env");
    expect(envCall[2]).toMatchObject({
      expectedVersion: "env-v1",
      overwrite: true,
      owner: "session",
    });
  });

  it("saves an edited compose file at the version the editor loaded", async () => {
    const { user } = render(<ComposeStacksPage />);

    await user.click(screen.getByRole("button", { name: "Edit stack" }));
    await screen.findByText(
      "Editing /home/alice/docker/web/docker-compose.yml",
    );
    await user.click(screen.getByRole("button", { name: "Save stack" }));

    await waitFor(() => expect(mocks.uploadContent).toHaveBeenCalledTimes(2));
    const [composeCall] = mocks.uploadContent.mock.calls;
    expect(composeCall[0]).toBe("/home/alice/docker/web/docker-compose.yml");
    expect(composeCall[2]).toMatchObject({
      expectedVersion: "compose-v1",
      owner: "session",
    });
    expect(composeCall[2].overwrite).toBeFalsy();
  });
});
