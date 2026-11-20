import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { INTERNAL_DRAG_TYPE, TRASH_DRAG_TYPE } from "./-dragTypes";
import {
  acceptInternalDrop,
  acceptTrashDrop,
  setInternalDrag,
  setTrashDrag,
} from "./-internalDrag";

const mocks = vi.hoisted(() => ({ toastError: vi.fn() }));

vi.mock("sonner", async (importOriginal) => ({
  ...(await importOriginal<typeof import("sonner")>()),
  toast: { error: mocks.toastError },
}));

const HERE = window.location.origin;
const OTHER = "https://backup.example:8090";
const ITEMS = [{ name: "a.txt", path: "/t/files/a.txt" }];

// A DataTransfer that keeps what is set on it, as a drag between tabs does.
const transfer = () => {
  const data = new Map<string, string>();
  return {
    effectAllowed: "",
    getData: (type: string) => data.get(type) ?? "",
    setData: (type: string, value: string) => void data.set(type, value),
    get types() {
      return [...data.keys()];
    },
  } as unknown as DataTransfer;
};

const carrying = (type: string, payload: unknown) =>
  ({
    getData: (requested: string) =>
      requested === type ? JSON.stringify(payload) : "",
    types: [type],
  }) as unknown as DataTransfer;

// What a drag started in this tab put in its payload.
const payloadOf = (dataTransfer: DataTransfer, type: string) =>
  JSON.parse(dataTransfer.getData(type)) as Record<string, unknown>;

const notFromHere = (count: number) => [
  `Cannot drop ${count} item${count === 1 ? "" : "s"} here`,
  {
    description: `${count === 1 ? "It was" : "They were"} not dragged from this LinuxIO, or from a tab of it opened before an update. Reload that tab and drag ${count === 1 ? "it" : "them"} again.`,
  },
];

describe("drags between tabs", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    window.localStorage.clear();
  });
  afterEach(() => vi.restoreAllMocks());

  it("takes a drag started in this tab", () => {
    const moved = transfer();
    setInternalDrag(moved, ["/srv/a.txt"]);
    expect(acceptInternalDrop(moved)).toEqual(["/srv/a.txt"]);

    const trashed = transfer();
    setTrashDrag(trashed, ITEMS);
    expect(acceptTrashDrop(trashed)).toEqual(ITEMS);
    expect(mocks.toastError).not.toHaveBeenCalled();
  });

  it("takes a drag from another tab of this server by the token they share", () => {
    const moved = transfer();
    setInternalDrag(moved, ["/srv/a.txt"]);
    const { token } = payloadOf(moved, INTERNAL_DRAG_TYPE);
    // Another tab of this server knows the token through storage alone.
    const elsewhere = carrying(INTERNAL_DRAG_TYPE, {
      origin: HERE,
      paths: ["/srv/b.txt"],
      token,
    });
    expect(acceptInternalDrop(elsewhere)).toEqual(["/srv/b.txt"]);
    expect(typeof token).toBe("string");
    expect(String(token)).toMatch(/^[0-9a-f]{32}$/);
  });

  it("refuses a forged drag that claims this server without its token, at both readers", () => {
    const moved = transfer();
    setInternalDrag(moved, ["/srv/a.txt"]);

    // Any page can write this server's origin and a path into a drag.
    expect(
      acceptInternalDrop(
        carrying(INTERNAL_DRAG_TYPE, { origin: HERE, paths: ["/etc/pam.d"] }),
      ),
    ).toEqual([]);
    expect(
      acceptInternalDrop(
        carrying(INTERNAL_DRAG_TYPE, {
          origin: HERE,
          paths: ["/etc/pam.d"],
          token: "0".repeat(32),
        }),
      ),
    ).toEqual([]);
    expect(
      acceptTrashDrop(
        carrying(TRASH_DRAG_TYPE, { items: ITEMS, origin: HERE }),
      ),
    ).toEqual([]);
    expect(
      acceptTrashDrop(
        carrying(TRASH_DRAG_TYPE, {
          items: ITEMS,
          origin: HERE,
          token: "f".repeat(32),
        }),
      ),
    ).toEqual([]);

    expect(mocks.toastError).toHaveBeenCalledTimes(4);
    expect(mocks.toastError).toHaveBeenCalledWith(...notFromHere(1));
  });

  it("refuses a drag from another server, naming it, whatever token it has", () => {
    const moved = transfer();
    setInternalDrag(moved, ["/srv/a.txt"]);
    const { token } = payloadOf(moved, INTERNAL_DRAG_TYPE);

    expect(
      acceptInternalDrop(
        carrying(INTERNAL_DRAG_TYPE, {
          origin: OTHER,
          paths: ["/srv/a.txt", "/srv/b.txt"],
          token,
        }),
      ),
    ).toEqual([]);
    expect(
      acceptTrashDrop(
        carrying(TRASH_DRAG_TYPE, { items: ITEMS, origin: OTHER, token: "x" }),
      ),
    ).toEqual([]);

    expect(mocks.toastError).toHaveBeenNthCalledWith(
      1,
      "Cannot drop 2 items here",
      {
        description: `They come from ${OTHER}, another server. Drop them in a tab of that server.`,
      },
    );
    expect(mocks.toastError).toHaveBeenNthCalledWith(
      2,
      "Cannot drop 1 item here",
      {
        description: `It comes from ${OTHER}, another server. Drop it in a tab of that server.`,
      },
    );
  });

  it("refuses a drag of a tab opened before an update, which carries no token", () => {
    expect(
      acceptInternalDrop(
        carrying(INTERNAL_DRAG_TYPE, { paths: ["/srv/a.txt"] }),
      ),
    ).toEqual([]);
    expect(
      acceptTrashDrop(
        carrying(TRASH_DRAG_TYPE, {
          items: [...ITEMS, { name: "b", path: "/t/files/b" }],
        }),
      ),
    ).toEqual([]);
    expect(mocks.toastError).toHaveBeenNthCalledWith(1, ...notFromHere(1));
    expect(mocks.toastError).toHaveBeenNthCalledWith(2, ...notFromHere(2));
  });

  it("ends a drag's token when the drag ends", () => {
    const source = document.createElement("div");
    const moved = transfer();
    setInternalDrag(moved, ["/srv/a.txt"], source);

    source.dispatchEvent(new Event("dragend"));
    expect(acceptInternalDrop(moved)).toEqual([]);
    expect(mocks.toastError).toHaveBeenCalledWith(...notFromHere(1));
  });

  it("keeps drags inside one tab working without storage, and says why another tab's are refused", () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new DOMException("denied", "SecurityError");
    });
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new DOMException("denied", "SecurityError");
    });

    const moved = transfer();
    setInternalDrag(moved, ["/srv/a.txt"]);
    expect(acceptInternalDrop(moved)).toEqual(["/srv/a.txt"]);

    expect(
      acceptInternalDrop(
        carrying(INTERNAL_DRAG_TYPE, {
          origin: HERE,
          paths: ["/srv/b.txt"],
          token: "1".repeat(32),
        }),
      ),
    ).toEqual([]);
    expect(mocks.toastError).toHaveBeenCalledWith("Cannot drop 1 item here", {
      description:
        "This browser keeps LinuxIO from its storage, which a drag between tabs needs. Drop it in the tab it came from.",
    });
  });

  it("says nothing about an empty drag", () => {
    expect(
      acceptInternalDrop(carrying(INTERNAL_DRAG_TYPE, { paths: [] })),
    ).toEqual([]);
    expect(mocks.toastError).not.toHaveBeenCalled();
  });
});
