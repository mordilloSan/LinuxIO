import { afterEach, describe, expect, it, vi } from "vitest";

import {
  registerToastLinkRouter,
  type ToastMeta,
  toastLinkOptions,
} from "./toast-meta";

const DOCKER = { label: "Open Docker", to: "/docker" } as const;

let releaseLinkRouter = () => {};

/** Puts the user on pathname as far as toast links can tell. */
function onPage(pathname: string) {
  const link = { navigate: vi.fn(), pathname: () => pathname };
  releaseLinkRouter = registerToastLinkRouter(link);
  return link;
}

function clickAction(opts: unknown) {
  const { action } = opts as { action: { onClick: (event: unknown) => void } };
  action.onClick({});
}

function folder(splat: string): ToastMeta {
  return {
    label: "Open files",
    params: { _splat: splat },
    to: "/filebrowser/$",
  };
}

describe("toastLinkOptions", () => {
  afterEach(() => releaseLinkRouter());

  it("offers the page to a user on another page", () => {
    const link = onPage("/");

    const opts = toastLinkOptions(DOCKER);

    expect(opts).toEqual({
      action: { label: "Open Docker", onClick: expect.any(Function) },
      duration: 10_000,
    });
    expect(link.navigate).not.toHaveBeenCalled();
    clickAction(opts);
    expect(link.navigate).toHaveBeenCalledWith(DOCKER);
  });

  it("reads the page when the toast fires, not when the work started", () => {
    let pathname = "/docker";
    releaseLinkRouter = registerToastLinkRouter({
      navigate: vi.fn(),
      pathname: () => pathname,
    });

    expect(toastLinkOptions(DOCKER)).toBeUndefined();
    pathname = "/";
    expect(toastLinkOptions(DOCKER)?.action).toBeDefined();
  });

  it("offers nothing on the page itself or below it", () => {
    onPage("/docker");
    expect(toastLinkOptions(DOCKER)).toBeUndefined();

    onPage("/docker/compose");
    expect(toastLinkOptions(DOCKER)).toBeUndefined();

    const opts = { description: "3 containers" };
    expect(toastLinkOptions(DOCKER, opts)).toBe(opts);
  });

  it("treats only a slash as below, and the Dashboard as its own page", () => {
    const storage = { label: "Open storage", to: "/storage" } as const;
    const dashboard = { label: "Open dashboard", to: "/" } as const;

    onPage("/storage/shares");
    expect(toastLinkOptions(storage)).toBeUndefined();
    expect(toastLinkOptions(dashboard)?.action).toBeDefined();

    onPage("/storagex");
    expect(toastLinkOptions(storage)?.action).toBeDefined();

    onPage("/");
    expect(toastLinkOptions(dashboard)).toBeUndefined();
    expect(toastLinkOptions(storage)?.action).toBeDefined();
  });

  it("offers a folder unless the file browser already shows it", () => {
    const docs = folder("home/ana/docs");

    onPage("/filebrowser/home/ana/docs");
    expect(toastLinkOptions(docs)).toBeUndefined();
    onPage("/filebrowser/home/ana/docs/");
    expect(toastLinkOptions(docs)).toBeUndefined();

    for (const elsewhere of [
      "/filebrowser/home/ana",
      "/filebrowser/home/ana/docs/2026",
      "/filebrowser",
      "/docker",
    ]) {
      const link = onPage(elsewhere);
      const opts = toastLinkOptions(docs);
      expect(opts?.action, elsewhere).toBeDefined();
      clickAction(opts);
      expect(link.navigate).toHaveBeenCalledWith(docs);
    }
  });

  it("knows a folder the router keeps partly encoded", () => {
    onPage("/filebrowser/srv/100%25 done");
    expect(toastLinkOptions(folder("srv/100% done"))).toBeUndefined();
  });

  it("offers nothing on the sign-in page", () => {
    onPage("/sign-in");
    expect(
      toastLinkOptions({ label: "Open storage", to: "/storage" }),
    ).toBeUndefined();
  });

  it("offers the file browser itself anywhere but in the file browser", () => {
    const files = folder("");

    for (const inside of [
      "/filebrowser",
      "/filebrowser/",
      "/filebrowser/srv",
    ]) {
      onPage(inside);
      expect(toastLinkOptions(files), inside).toBeUndefined();
    }

    onPage("/");
    expect(toastLinkOptions(files)).toMatchObject({
      action: { label: "Open files" },
    });
  });

  it("offers nothing for a toast without a page", () => {
    onPage("/");
    expect(toastLinkOptions({ label: "System" })).toBeUndefined();
    expect(toastLinkOptions(undefined)).toBeUndefined();

    const opts = { description: "details" };
    expect(toastLinkOptions({ label: "System" }, opts)).toBe(opts);
  });

  it("never replaces an action the caller passes", () => {
    onPage("/");
    const opts = {
      action: { label: "Open the real folder", onClick: vi.fn() },
      duration: 10_000,
    };

    expect(toastLinkOptions(DOCKER, opts)).toBe(opts);
  });

  it("keeps the caller's options and labels an unlabelled link Open", () => {
    onPage("/");

    const opts = toastLinkOptions(
      { to: "/docker" },
      { description: "x", duration: 3000 },
    );

    expect(opts).toEqual({
      action: { label: "Open", onClick: expect.any(Function) },
      description: "x",
      duration: 3000,
    });
  });

  it("offers nothing while no router is registered", () => {
    expect(toastLinkOptions(DOCKER)).toBeUndefined();

    const release = registerToastLinkRouter({
      navigate: vi.fn(),
      pathname: () => "/",
    });
    release();

    expect(toastLinkOptions(DOCKER)).toBeUndefined();
  });
});
