import { readdirSync, readFileSync } from "node:fs";
import path from "node:path";

import { expect, test } from "@playwright/test";

const ROUTES_DIR = path.resolve(process.cwd(), "src/routes");

/**
 * Every route file that the TanStack plugin code-splits, discovered from disk
 * rather than hand-listed, so a new route is covered the moment it is added.
 *
 * Mirrors the generator's own filters: `-` prefixed files and directories are
 * ignored, and `__root.tsx` is never split because it is in the initial bundle.
 */
function collectRouteFiles(directory: string, prefix = ""): string[] {
  return readdirSync(directory, { withFileTypes: true })
    .filter((entry) => !entry.name.startsWith("-"))
    .flatMap((entry) => {
      const relative = prefix ? `${prefix}/${entry.name}` : entry.name;
      if (entry.isDirectory()) {
        return collectRouteFiles(path.join(directory, entry.name), relative);
      }
      if (!entry.name.endsWith(".tsx") || entry.name === "__root.tsx") {
        return [];
      }
      return [relative];
    })
    .sort((a, b) => a.localeCompare(b));
}

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

test("keeps every route component in its own production chunk", () => {
  const manifestPath = path.resolve(
    process.cwd(),
    "../backend/webserver/web/frontend/.vite/manifest.json",
  );
  const manifest = JSON.parse(readFileSync(manifestPath, "utf8")) as Record<
    string,
    { file: string; isDynamicEntry?: boolean }
  >;
  const routeFiles = collectRouteFiles(ROUTES_DIR);

  // A route tree small enough to hand-list would mean the discovery above is
  // broken (wrong cwd, wrong directory) rather than that routes were deleted.
  expect(routeFiles.length).toBeGreaterThan(20);

  const chunks = routeFiles.map((routeFile) => {
    const key = `src/routes/${routeFile}?tsr-split=component`;
    const entry = manifest[key];
    expect(
      entry,
      `No split component chunk for ${routeFile}. Every route file must export a component so autoCodeSplitting can lazy-load it.`,
    ).toBeDefined();
    expect(entry.isDynamicEntry, `${routeFile} is not a dynamic entry`).toBe(
      true,
    );
    return entry.file;
  });

  expect(new Set(chunks).size).toBe(routeFiles.length);
});
