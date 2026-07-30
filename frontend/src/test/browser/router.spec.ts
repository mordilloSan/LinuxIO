import { readFileSync } from "node:fs";
import path from "node:path";

import { expect, test } from "@playwright/test";

test.describe("TanStack child-route browser lifecycle", () => {
  test("supports direct links and refresh", async ({ page }) => {
    await page.goto("/accounts/groups");

    await expect(
      page.getByRole("heading", { name: "Groups route content" }),
    ).toBeVisible();
    await expect(page.getByRole("tab", { name: "Groups" })).toHaveAttribute(
      "aria-selected",
      "true",
    );

    await page.reload();

    await expect(
      page.getByRole("heading", { name: "Groups route content" }),
    ).toBeVisible();
    await expect(page).toHaveURL(/\/accounts\/groups$/);
  });

  test("preserves URL, active tab, and content through browser history", async ({
    page,
  }) => {
    await page.goto("/accounts");
    await expect(
      page.getByRole("heading", { name: "Users route content" }),
    ).toBeVisible();

    await page.getByRole("tab", { name: "Groups" }).click();
    await expect(page.getByRole("status")).toContainText("Loading child route");
    await expect(
      page.getByRole("heading", { name: "Groups route content" }),
    ).toBeVisible();
    await expect(page).toHaveURL(/\/accounts\/groups$/);

    await page.goBack();
    await expect(
      page.getByRole("heading", { name: "Users route content" }),
    ).toBeVisible();
    await expect(page.getByRole("tab", { name: "Users" })).toHaveAttribute(
      "aria-selected",
      "true",
    );

    await page.goForward();
    await expect(
      page.getByRole("heading", { name: "Groups route content" }),
    ).toBeVisible();
    await expect(page.getByRole("tab", { name: "Groups" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
  });

  test("renders route errors inside the parent layout", async ({ page }) => {
    await page.goto("/accounts/error");

    await expect(page.getByRole("alert")).toHaveText(
      "Route failed: fixture loader rejected",
    );
    await expect(page.getByRole("tablist", { name: "Tabs" })).toBeVisible();
  });

  test("renders the default not-found UI for an unknown nested child", async ({
    page,
  }) => {
    await page.goto("/accounts/does-not-exist");

    await expect(page.getByRole("alert")).toHaveText("Fixture page not found");
  });

  test("loads a child component chunk on first navigation", async ({
    page,
  }) => {
    const scripts: string[] = [];
    page.on("response", (response) => {
      const url = response.url();
      if (/\.js(?:$|\?)/.test(url)) scripts.push(url);
    });

    await page.goto("/accounts");
    await expect(
      page.getByRole("heading", { name: "Users route content" }),
    ).toBeVisible();
    expect(scripts.some((url) => /GroupsPage-[^/]+\.js$/.test(url))).toBe(
      false,
    );

    await page.getByRole("tab", { name: "Groups" }).click();
    await expect(
      page.getByRole("heading", { name: "Groups route content" }),
    ).toBeVisible();
    expect(scripts.some((url) => /GroupsPage-[^/]+\.js$/.test(url))).toBe(true);
  });
});

test("keeps every page-level child route in its own production chunk", () => {
  const manifestPath = path.resolve(
    process.cwd(),
    "../backend/webserver/web/frontend/.vite/manifest.json",
  );
  const manifest = JSON.parse(readFileSync(manifestPath, "utf8")) as Record<
    string,
    { file: string; isDynamicEntry?: boolean }
  >;
  const childRouteFiles = [
    "accounts/index.tsx",
    "accounts/groups.tsx",
    "services/index.tsx",
    "services/timers.tsx",
    "services/sockets.tsx",
    "storage/index.tsx",
    "storage/lvm.tsx",
    "shares/index.tsx",
    "shares/mounts.tsx",
    "updates/index.tsx",
    "updates/history.tsx",
    "docker/index.tsx",
    "docker/containers.tsx",
    "docker/compose.tsx",
    "docker/networks.tsx",
    "docker/volumes.tsx",
    "docker/images.tsx",
    "vm/index.tsx",
    "vm/networks.tsx",
    "vm/images.tsx",
    "vm/machines.tsx",
  ];

  const chunks = childRouteFiles.map((routeFile) => {
    const key =
      `src/routes/_authenticated/${routeFile}?tsr-split=component` as const;
    const entry = manifest[key];
    expect(entry, `Missing component chunk for ${routeFile}`).toBeDefined();
    expect(entry.isDynamicEntry, `${routeFile} is not a dynamic entry`).toBe(
      true,
    );
    return entry.file;
  });

  expect(new Set(chunks).size).toBe(childRouteFiles.length);
});
