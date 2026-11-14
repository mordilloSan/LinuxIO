import { Profiler, useState, type ComponentProps } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { createTestQueryClient, render, screen, waitFor } from "@/test/render";

import DirectoryTree from "./DirectoryTree";
import PathPickerField from "./PathPickerField";

const mocks = vi.hoisted(() => ({
  directoryChildren: vi.fn(),
  listDirectory: vi.fn(),
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();

  return {
    ...actual,
    linuxio: {
      ...actual.linuxio,
      filebrowser: {
        ...actual.linuxio.filebrowser,
        directory_children: Object.assign(
          (request: { path: string; includeFiles: boolean }) => ({
            queryKey: ["test", "directory-children", request],
            queryFn: () => mocks.directoryChildren(request),
          }),
          { route: actual.linuxio.filebrowser.directory_children.route },
        ),
        list_directory: Object.assign(
          (request: { path: string }) => {
            mocks.listDirectory(request);
            throw new Error("PathPickerField must not fetch a full listing");
          },
          { route: actual.linuxio.filebrowser.list_directory.route },
        ),
      },
    },
  };
});

type Listing = { files?: string[]; folders?: string[] };

/**
 * Answer each path from the map; a path it does not name is an empty folder,
 * and a path in `failures` is refused with that message.
 */
function serve(
  listings: Record<string, Listing>,
  failures: Record<string, string> = {},
) {
  mocks.directoryChildren.mockImplementation(
    ({ path, includeFiles }: { path: string; includeFiles: boolean }) =>
      path in failures
        ? Promise.reject(new Error(failures[path]))
        : Promise.resolve({
            folders: listings[path]?.folders ?? [],
            files: includeFiles ? (listings[path]?.files ?? []) : [],
          }),
  );
}

function PickerHarness({
  initialValue = "",
  ...props
}: Omit<ComponentProps<typeof PathPickerField>, "onChange" | "value"> & {
  initialValue?: string;
}) {
  const [value, setValue] = useState(initialValue);

  return <PathPickerField {...props} onChange={setValue} value={value} />;
}

const field = () => screen.getByLabelText("Directory Path");
const row = (name: string) => screen.getByRole("treeitem", { name });
const findRow = (name: string) => screen.findByRole("treeitem", { name });

describe("PathPickerField", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    serve({
      "/": { folders: ["media"] },
      "/media": { folders: ["nested"], files: ["debian.iso", "notes.txt"] },
    });
  });

  it("opens with the root expanded, selects a folder and closes", async () => {
    const { user } = render(<PickerHarness />);

    await user.click(field());
    await user.click(await screen.findByText("media"));

    expect(mocks.directoryChildren).toHaveBeenCalledWith({
      path: "/",
      includeFiles: false,
    });
    expect(mocks.listDirectory).not.toHaveBeenCalled();
    expect(field()).toHaveValue("/media/");
    expect(screen.queryByRole("tree")).not.toBeInTheDocument();
  });

  it("selects a filtered file and closes while folders stay navigable", async () => {
    const { user } = render(
      <PickerHarness
        browseLabel="Browse ISO files"
        editable
        fileFilter={(path) => path.toLowerCase().endsWith(".iso")}
        includeFiles
        label="ISO path"
        selectableTypes={["file"]}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Browse ISO files" }));
    await user.click(await screen.findByText("media"));
    await user.click(await screen.findByText("debian.iso"));

    expect(mocks.directoryChildren).toHaveBeenCalledWith({
      path: "/",
      includeFiles: true,
    });
    expect(mocks.directoryChildren).toHaveBeenCalledWith({
      path: "/media",
      includeFiles: true,
    });
    expect(mocks.listDirectory).not.toHaveBeenCalled();
    expect(screen.queryByText("notes.txt")).not.toBeInTheDocument();
    expect(screen.getByLabelText("ISO path")).toHaveValue("/media/debian.iso");
    expect(screen.queryByRole("tree")).not.toBeInTheDocument();
  });

  it("stays collapsed with the reason when loading fails and retries on the next activation", async () => {
    mocks.directoryChildren.mockRejectedValueOnce(
      new Error("resource unavailable"),
    );
    const { user } = render(<PickerHarness />);

    await user.click(field());
    const expandButton = screen.getByRole("button", { name: "Expand /" });

    await waitFor(() => expect(expandButton).not.toBeDisabled());
    expect(row("/")).toHaveAttribute("aria-expanded", "false");
    // The reason stays on the row: its description, while its name stays the
    // folder's, which type-ahead and the other rows' tests rely on.
    expect(screen.getByText("resource unavailable")).toBeVisible();
    expect(row("/")).toHaveAccessibleName("/");
    expect(row("/")).toHaveAccessibleDescription("resource unavailable");

    await user.click(expandButton);

    expect(await findRow("media")).toBeInTheDocument();
    expect(row("/")).toHaveAttribute("aria-expanded", "true");
    expect(screen.queryByText("resource unavailable")).not.toBeInTheDocument();
    expect(row("/")).not.toHaveAttribute("aria-describedby");
  });

  it("shows why a folder leading to the value cannot be listed, without a toast", async () => {
    serve(
      { "/": { folders: ["mnt"] }, "/mnt": { folders: ["backup"] } },
      { "/mnt/backup": "permission denied" },
    );
    const queryClient = createTestQueryClient();
    const { user } = render(
      <PickerHarness initialValue="/mnt/backup/daily/" />,
      {
        queryClient,
      },
    );

    await user.click(field());

    await waitFor(() =>
      expect(row("backup")).toHaveAccessibleDescription("permission denied"),
    );
    expect(screen.getByText("permission denied")).toBeVisible();
    expect(row("backup")).toHaveAttribute("aria-expanded", "false");
    expect(row("mnt")).not.toHaveAttribute("aria-describedby");
    // The row shows the reason, so the app's query cache must not toast it as
    // well; the test client has no toasting `onError`, so check what it reads.
    const listings = queryClient
      .getQueryCache()
      .findAll({ queryKey: ["test", "directory-children"] });
    expect(listings.map((query) => query.queryKey[2])).toEqual(
      expect.arrayContaining([
        { path: "/", includeFiles: false },
        { path: "/mnt", includeFiles: false },
        { path: "/mnt/backup", includeFiles: false },
      ]),
    );
    for (const query of listings) {
      expect(query.meta).toEqual({ silent: true });
    }
  });

  it("ignores repeated row activation while loading", async () => {
    let resolveChildren: (resource: unknown) => void = () => undefined;
    const pendingChildren = new Promise((resolve) => {
      resolveChildren = resolve;
    });
    mocks.directoryChildren.mockReturnValueOnce(pendingChildren);
    const { user } = render(<PickerHarness selectableTypes={[]} />);

    await user.click(field());
    await user.click(row("/"));
    await user.click(row("/"));

    expect(mocks.directoryChildren).toHaveBeenCalledTimes(1);

    resolveChildren({ folders: ["media"], files: [] });
    await waitFor(() =>
      expect(row("/")).toHaveAttribute("aria-expanded", "true"),
    );
  });

  it("closes with Escape and returns focus to the field", async () => {
    const { user } = render(<PickerHarness />);

    await user.click(field());
    expect(screen.getByRole("tree")).toBeInTheDocument();

    await user.keyboard("{Escape}");

    expect(screen.queryByRole("tree")).not.toBeInTheDocument();
    expect(field()).toHaveFocus();
  });

  it("closes when clicking outside", async () => {
    const { user } = render(<PickerHarness />);

    await user.click(field());
    expect(screen.getByRole("tree")).toBeInTheDocument();

    await user.click(document.body);

    expect(screen.queryByRole("tree")).not.toBeInTheDocument();
  });

  it("closes when the read-only field is clicked again", async () => {
    const { user } = render(<PickerHarness />);

    await user.click(field());
    expect(screen.getByRole("tree")).toBeInTheDocument();

    await user.click(field());

    expect(screen.queryByRole("tree")).not.toBeInTheDocument();
  });

  it.each(["{Enter}", " ", "{ArrowDown}"])(
    "opens the read-only field from the keyboard with %j",
    async (key) => {
      const { user } = render(<PickerHarness />);

      await user.tab();
      expect(field()).toHaveFocus();
      expect(field()).toHaveAttribute("aria-expanded", "false");

      await user.keyboard(key);

      expect(field()).toHaveAttribute("aria-expanded", "true");
      expect(field()).toHaveAttribute("aria-haspopup", "tree");
      expect(row("/")).toHaveFocus();
    },
  );

  it("chooses a folder with the keyboard and returns focus to the field", async () => {
    const { user } = render(<PickerHarness />);

    await user.tab();
    await user.keyboard("{Enter}");
    await findRow("media");
    await user.keyboard("{ArrowDown}");

    expect(row("media")).toHaveFocus();

    await user.keyboard("{Enter}");

    expect(field()).toHaveValue("/media/");
    expect(screen.queryByRole("tree")).not.toBeInTheDocument();
    expect(field()).toHaveFocus();
  });

  it("closes on Tab and returns focus to the field", async () => {
    const { user } = render(<PickerHarness />);

    await user.click(field());
    expect(row("/")).toHaveFocus();

    await user.tab();

    expect(screen.queryByRole("tree")).not.toBeInTheDocument();
    expect(field()).toHaveFocus();
  });

  it("walks the tree with the arrow keys without selecting", async () => {
    serve({
      "/": { folders: ["media", "mnt"] },
      "/media": { folders: ["nested"] },
    });
    const { user } = render(<PickerHarness />);

    await user.click(field());
    await findRow("media");
    await user.keyboard("{ArrowDown}");
    expect(row("media")).toHaveFocus();

    await user.keyboard("{ArrowRight}");
    await findRow("nested");
    expect(row("media")).toHaveAttribute("aria-expanded", "true");

    await user.keyboard("{ArrowRight}");
    expect(row("nested")).toHaveFocus();
    expect(row("nested")).toHaveAttribute("aria-level", "3");

    await user.keyboard("{ArrowLeft}");
    expect(row("media")).toHaveFocus();

    await user.keyboard("{ArrowLeft}");
    expect(row("media")).toHaveAttribute("aria-expanded", "false");

    // The rows of the folder that is closing are skipped.
    await user.keyboard("{End}");
    expect(row("mnt")).toHaveFocus();

    await user.keyboard("{Home}");
    expect(row("/")).toHaveFocus();

    expect(field()).toHaveValue("");
    expect(screen.getByRole("tree")).toBeInTheDocument();
  });

  it("moves focus with type-ahead instead of selecting", async () => {
    serve({ "/": { folders: ["bin", "media", "mnt"] } });
    const { user } = render(<PickerHarness />);

    await user.click(field());
    await findRow("media");

    await user.keyboard("m");
    expect(row("media")).toHaveFocus();

    // The same letter again steps to the next match and wraps around.
    await user.keyboard("m");
    expect(row("mnt")).toHaveFocus();
    await user.keyboard("m");
    expect(row("media")).toHaveFocus();

    expect(field()).toHaveValue("");
    expect(screen.getByRole("tree")).toBeInTheDocument();
  });

  it("narrows type-ahead with the letters typed in a row", async () => {
    serve({ "/": { folders: ["bin", "media", "mnt"] } });
    const { user } = render(<PickerHarness />);

    await user.click(field());
    await findRow("media");
    await user.keyboard("mn");

    expect(row("mnt")).toHaveFocus();
  });

  it("never selects a folder from the keyboard in a file-only picker", async () => {
    const { user } = render(
      <PickerHarness includeFiles selectableTypes={["file"]} />,
    );

    await user.click(field());
    await findRow("media");
    await user.keyboard("m{Enter}");

    // Enter on a folder that cannot be chosen opens it.
    expect(await findRow("debian.iso")).not.toHaveAttribute("aria-expanded");
    expect(field()).toHaveValue("");
    expect(screen.getByRole("tree")).toBeInTheDocument();

    await user.keyboard("d{Enter}");

    expect(field()).toHaveValue("/media/debian.iso");
    expect(screen.queryByRole("tree")).not.toBeInTheDocument();
  });

  it("opens at the current value and highlights it without a trailing slash", async () => {
    const { user } = render(
      <PickerHarness editable initialValue="/media/nested" />,
    );

    await user.click(screen.getByRole("button", { name: "Browse folders" }));
    const nested = await findRow("nested");

    expect(nested).toHaveAttribute("aria-selected", "true");
    expect(nested).toHaveFocus();
    expect(mocks.directoryChildren).toHaveBeenCalledWith({
      path: "/media",
      includeFiles: false,
    });
    expect(mocks.directoryChildren).not.toHaveBeenCalledWith({
      path: "/media/nested",
      includeFiles: false,
    });
  });

  it("does not ask for folders the current value names but the host lacks", async () => {
    const { user } = render(
      <PickerHarness editable initialValue="/gone/deeper/still" />,
    );

    await user.click(screen.getByRole("button", { name: "Browse folders" }));
    await findRow("media");

    expect(mocks.directoryChildren).toHaveBeenCalledTimes(1);
    expect(mocks.directoryChildren).toHaveBeenCalledWith({
      path: "/",
      includeFiles: false,
    });
  });

  it("shows a new folder when the listing is invalidated while open", async () => {
    const queryClient = createTestQueryClient();
    const { user } = render(<PickerHarness />, { queryClient });

    await user.click(field());
    await findRow("media");

    serve({ "/": { folders: ["media", "new"] } });
    await queryClient.invalidateQueries({
      queryKey: ["test", "directory-children"],
    });

    expect(await findRow("new")).toBeInTheDocument();
  });
});

describe("DirectoryTree", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    serve({
      "/": { folders: ["media"] },
      "/media": { folders: ["nested"] },
    });
  });

  it("follows a root path that changes while it is mounted", async () => {
    const { rerender } = render(
      <DirectoryTree onSelect={() => undefined} rootPath="/media" />,
    );

    expect(await findRow("nested")).toBeInTheDocument();
    expect(row("/media/")).toHaveAttribute("aria-expanded", "true");

    rerender(<DirectoryTree onSelect={() => undefined} rootPath="/" />);

    expect(await findRow("media")).toBeInTheDocument();
    expect(
      screen.queryByRole("treeitem", { name: "/media/" }),
    ).not.toBeInTheDocument();
  });

  it("moves focus without drawing the tree again", async () => {
    serve({ "/": { folders: ["bin", "media", "mnt"] } });
    const commits = vi.fn();
    const { user } = render(
      <Profiler id="tree" onRender={commits}>
        <DirectoryTree autoFocus onSelect={() => undefined} />
      </Profiler>,
    );

    await findRow("media");
    // The first key turns the focus ring on. That is one small update, and
    // React looks at it once more on the next key.
    await user.keyboard("{ArrowDown}{ArrowUp}");
    expect(row("/")).toHaveFocus();
    commits.mockClear();

    await user.keyboard("{ArrowDown}{ArrowDown}{ArrowUp}{End}{Home}m");

    // A long folder cannot afford a render of every row on each key press.
    expect(row("media")).toHaveFocus();
    expect(row("media")).toHaveAttribute("tabindex", "0");
    expect(row("/")).toHaveAttribute("tabindex", "-1");
    expect(commits).not.toHaveBeenCalled();
  });

  it("does not take focus unless asked to", async () => {
    render(<DirectoryTree onSelect={() => undefined} />);

    await findRow("media");

    expect(document.body).toHaveFocus();
    expect(row("/")).toHaveAttribute("tabindex", "0");
    expect(row("media")).toHaveAttribute("tabindex", "-1");
  });
});
