import { readFileSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it, vi } from "vitest";

import { linkRefusalDestination, toastFileError } from "./linkRefusal";

// Vitest runs from frontend/.
const HANDOVER_GO = join(
  process.cwd(),
  "../backend/bridge/handlers/filebrowser/services/handover.go",
);

/** The format string of the Go `plantedLinkError`. */
function backendRefusalFormat() {
  const source = readFileSync(HANDOVER_GO, "utf8");
  const format = source.match(
    /^func plantedLinkError\([^)]*\) error \{\n\treturn fmt\.Errorf\("((?:[^"\\]|\\.)*)"/m,
  );
  if (!format) throw new Error(`no plantedLinkError format in ${HANDOVER_GO}`);
  return format[1];
}

describe("linkRefusalDestination", () => {
  it("finds the destination in the text the bridge formats", () => {
    const format = backendRefusalFormat();
    // The sentinel, the link and where it points, in that order.
    expect(format.match(/%[a-z]/g)).toEqual(["%w", "%s", "%s"]);
    const values = ["forbidden", "/home/a/docs", "/mnt/data/docs"];
    let next = 0;
    const text = format.replace(/%[a-z]/g, () => values[next++]);

    expect(linkRefusalDestination(text)).toBe("/mnt/data/docs");
  });

  it("returns the destination whatever precedes the refusal", () => {
    expect(
      linkRefusalDestination(
        "forbidden: /home/a/docs is a link to /mnt/data/docs. Open that folder to make this change",
      ),
    ).toBe("/mnt/data/docs");
    expect(
      linkRefusalDestination(
        "create parent dir: forbidden: /home/a/docs is a link to /mnt/data/docs. Open that folder to make this change",
      ),
    ).toBe("/mnt/data/docs");
  });

  it("keeps spaces and dots that belong to the destination", () => {
    expect(
      linkRefusalDestination(
        "forbidden: /srv/in is a link to /mnt/My Files/v1.2. Open source. Open that folder to make this change",
      ),
    ).toBe("/mnt/My Files/v1.2. Open source");
    expect(
      linkRefusalDestination(
        "forbidden: /srv/root is a link to /. Open that folder to make this change",
      ),
    ).toBe("/");
  });

  it("returns the first of several refusals", () => {
    expect(
      linkRefusalDestination(
        [
          "a.txt: permission denied",
          "b: forbidden: /home/a/b is a link to /mnt/one. Open that folder to make this change",
          "c: forbidden: /home/a/c is a link to /mnt/two. Open that folder to make this change",
        ].join("\n"),
      ),
    ).toBe("/mnt/one");
  });

  it("returns null for any other text", () => {
    expect(linkRefusalDestination("")).toBeNull();
    expect(linkRefusalDestination("forbidden: permission denied")).toBeNull();
    expect(
      linkRefusalDestination("/home/a/docs is a link to /mnt/data/docs"),
    ).toBeNull();
    expect(
      linkRefusalDestination(
        "x is a link to data/docs. Open that folder to make this change",
      ),
    ).toBeNull();
  });
});

describe("toastFileError", () => {
  const REFUSAL =
    "forbidden: /home/a/docs is a link to /mnt/data/docs. Open that folder to make this change";

  function setup() {
    const toast = { error: vi.fn() };
    const navigate = vi.fn(async () => undefined);
    return { navigate, toast };
  }

  function clickAction(toast: { error: ReturnType<typeof vi.fn> }) {
    const opts = toast.error.mock.calls[0][1] as {
      action: { label: string; onClick: (event: unknown) => void };
    };
    opts.action.onClick({});
  }

  it("offers to open the destination of a refusal in the message", () => {
    const { navigate, toast } = setup();

    toastFileError(toast, navigate, REFUSAL);

    expect(toast.error).toHaveBeenCalledWith(
      REFUSAL,
      expect.objectContaining({
        action: expect.objectContaining({ label: "Open the real folder" }),
      }),
    );
    clickAction(toast);
    expect(navigate).toHaveBeenCalledWith({
      to: "/filebrowser/$",
      params: { _splat: "mnt/data/docs" },
      search: {},
    });
  });

  it("offers it for a refusal among the failures of a batch", () => {
    const { navigate, toast } = setup();

    toastFileError(toast, navigate, "Failed to delete 2 items", {
      description: `a.txt: permission denied\ndocs: ${REFUSAL}`,
    });

    expect(toast.error).toHaveBeenCalledWith(
      "Failed to delete 2 items",
      expect.objectContaining({
        action: expect.objectContaining({ label: "Open the real folder" }),
        description: `a.txt: permission denied\ndocs: ${REFUSAL}`,
      }),
    );
    clickAction(toast);
    expect(navigate).toHaveBeenCalledWith(
      expect.objectContaining({ params: { _splat: "mnt/data/docs" } }),
    );
  });

  it("opens the root for a link to it", () => {
    const { navigate, toast } = setup();

    toastFileError(
      toast,
      navigate,
      "forbidden: /srv/root is a link to /. Open that folder to make this change",
    );

    clickAction(toast);
    expect(navigate).toHaveBeenCalledWith(
      expect.objectContaining({ params: { _splat: "" } }),
    );
  });

  it("leaves any other error as it was", () => {
    const { navigate, toast } = setup();

    toastFileError(toast, navigate, "permission denied");
    toastFileError(toast, navigate, "Failed to delete 1 item", {
      description: "a.txt: permission denied",
    });

    expect(toast.error.mock.calls).toEqual([
      ["permission denied"],
      ["Failed to delete 1 item", { description: "a.txt: permission denied" }],
    ]);
    expect(navigate).not.toHaveBeenCalled();
  });
});
