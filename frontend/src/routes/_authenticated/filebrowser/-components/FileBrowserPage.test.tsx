import { beforeEach, expect, it, vi } from "vitest";

import { openChannel, type Stream } from "@/api";
import { render } from "@/test/render";

import FileBrowserPage from "./FileBrowserPage";

const route = vi.hoisted(() => ({
  resourceType: "directory",
  search: {} as Record<string, unknown>,
}));

vi.mock("@/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api")>()),
  openChannel: vi.fn(),
  useStreamMux: () => ({ isOpen: true }),
}));

vi.mock("@tanstack/react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-router")>()),
  getRouteApi: () => ({
    useNavigate: () => vi.fn(),
    useParams: ({ select }: { select: (params: object) => unknown }) =>
      select({ _splat: "srv" }),
    useRouteContext: ({ select }: { select: (context: object) => unknown }) =>
      select({ fileBrowserListingQueryOptions: {} }),
    useSearch: ({ select }: { select: (search: object) => unknown }) =>
      select(route.search),
  }),
}));

vi.mock("../-hooks/useFileBrowserController", () => ({
  useFileBrowserController: () => ({
    contentProps: {
      chrome: { normalizedPath: "/srv", onDropItems: vi.fn() },
      data: { resource: { type: route.resourceType } },
    },
    dialogsProps: {},
  }),
}));
vi.mock("../-hooks/useTrashJobs", () => ({
  useTrashJobs: () => ({ restoreInto: vi.fn() }),
}));
vi.mock("../-hooks/useFileConflicts", () => ({
  useFileConflictResolution: () => ({}),
}));
vi.mock("../-hooks/useFileMutations", () => ({
  useFileMutations: () => ({ deleteItems: vi.fn(), trashItems: vi.fn() }),
}));
vi.mock("../-hooks/useFileViewState", () => ({
  useFileViewState: () => ({ actions: {}, cardSize: 1, viewMode: "list" }),
}));
vi.mock("./FileBrowserBreadcrumbs", () => ({ default: () => null }));
vi.mock("./FileBrowserContent", () => ({ default: () => null }));
vi.mock("./FileBrowserDialogs", () => ({ default: () => null }));
vi.mock("./FileBrowserPanels", () => ({
  FileBrowserConflictDialog: () => null,
}));
vi.mock("./NoTrashPrompt", () => ({ default: () => null }));
vi.mock("./TrashPlace", () => ({ default: () => null }));

const fakeStream = (): Stream => ({
  abort: vi.fn(),
  close: vi.fn(),
  id: 1,
  onClose: null,
  onData: null,
  onProgress: null,
  onResult: null,
  resize: vi.fn(),
  status: "open",
  type: "request",
  write: vi.fn(),
});

beforeEach(() => {
  vi.mocked(openChannel).mockReset();
  vi.mocked(openChannel).mockImplementation(fakeStream);
});

it("watches the folder it lists", () => {
  route.resourceType = "directory";
  route.search = {};
  render(<FileBrowserPage />);
  expect(openChannel).toHaveBeenCalledExactlyOnceWith("filebrowser.watch", {
    path: "/srv",
  });
});

it("watches nothing while the Trash shows", () => {
  route.search = { trash: true };
  render(<FileBrowserPage />);
  expect(openChannel).not.toHaveBeenCalled();
});

it("watches nothing while a file shows", () => {
  route.resourceType = "file";
  route.search = {};
  render(<FileBrowserPage />);
  expect(openChannel).not.toHaveBeenCalled();
});
