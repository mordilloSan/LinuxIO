import { beforeEach, describe, expect, it, vi } from "vitest";

import { render, screen, waitFor } from "@/test/render";

import ComposeStacksPage from "./ComposeStacksPage";

const mocks = vi.hoisted(() => ({
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
// the real dialog hands the page when the user saves a new stack with a .env.
vi.mock("./ComposeEditorDialog", () => ({
  default: ({
    onSave,
  }: {
    onSave: (
      content: string,
      stackName: string,
      filePath: string,
      envContent?: string,
    ) => Promise<boolean>;
  }) => (
    <button
      onClick={() => void onSave("services: {}\n", "web", "", "KEY=1\n")}
      type="button"
    >
      Save stack
    </button>
  ),
}));

vi.mock("./ComposeList", () => ({ default: () => null }));
vi.mock("./ComposeOperationDialog", () => ({ default: () => null }));
vi.mock("./ComposePostSaveDialog", () => ({ default: () => null }));
vi.mock("./DeleteStackDialog", () => ({ default: () => null }));
vi.mock("./StackSetupDialog", () => ({ default: () => null }));

describe("ComposeStacksPage save", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.resolveComposeFilePath.mockResolvedValue({
      path: "/home/alice/docker/web/docker-compose.yml",
    });
    mocks.uploadContent.mockResolvedValue(undefined);
  });

  it("creates the compose file and .env as the session user, the .env private", async () => {
    const { user } = render(<ComposeStacksPage />);

    await user.click(screen.getByRole("button", { name: "Save stack" }));

    await waitFor(() => expect(mocks.uploadContent).toHaveBeenCalledTimes(2));
    const [composeCall, envCall] = mocks.uploadContent.mock.calls;
    expect(composeCall[0]).toBe("/home/alice/docker/web/docker-compose.yml");
    expect(composeCall[2]).toMatchObject({ owner: "session" });
    expect(composeCall[2]).not.toHaveProperty("private");
    expect(envCall[0]).toBe("/home/alice/docker/web/.env");
    expect(envCall[2]).toMatchObject({
      overwrite: true,
      owner: "session",
      private: true,
    });
  });
});
