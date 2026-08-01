import { readdirSync, readFileSync } from "node:fs";
import path from "node:path";

import { expect, test } from "@playwright/test";

const buildDirectory = path.resolve(
  process.cwd(),
  "../backend/webserver/web/frontend",
);
const assetsDirectory = path.join(buildDirectory, "assets");

function readBuiltFiles(extension: string) {
  return readdirSync(assetsDirectory)
    .filter((file) => file.endsWith(extension))
    .map((file) => readFileSync(path.join(assetsDirectory, file), "utf8"))
    .join("\n");
}

test("keeps development Web Vitals diagnostics out of production", () => {
  const manifest = JSON.parse(
    readFileSync(path.join(buildDirectory, ".vite/manifest.json"), "utf8"),
  ) as Record<string, { file: string }>;
  const productionJavaScript = readBuiltFiles(".js");

  expect(
    Object.keys(manifest).some((key) => key.includes("startWebVitals")),
  ).toBe(false);
  expect(productionJavaScript).not.toContain("linuxio.webVitals");
  expect(productionJavaScript).not.toContain(
    "Unable to start Web Vitals measurement",
  );
});

test("ships Inter locally without external font services", () => {
  const indexHtml = readFileSync(
    path.join(buildDirectory, "index.html"),
    "utf8",
  );
  const productionCss = readBuiltFiles(".css");
  const fontFiles = readdirSync(assetsDirectory).filter((file) =>
    file.endsWith(".woff2"),
  );

  expect(`${indexHtml}\n${productionCss}`).not.toMatch(
    /fonts\.(?:googleapis|gstatic)\.com/,
  );
  expect(productionCss).toContain("Inter Variable");
  expect(
    fontFiles.some((file) => file.startsWith("inter-latin-wght-normal-")),
  ).toBe(true);
});
