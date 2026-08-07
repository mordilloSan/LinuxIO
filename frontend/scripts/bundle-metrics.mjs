#!/usr/bin/env node
import { existsSync, readdirSync, statSync, writeFileSync } from "node:fs";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const frontendRoot = path.resolve(__dirname, "..");
const repoRoot = path.resolve(frontendRoot, "..");

const defaultDistDir = path.resolve(
  repoRoot,
  "backend/webserver/web/frontend",
);

const metricConfigs = [
  {
    name: "initial_shell",
    label: "Initial shell",
    entries: ["index.html"],
    extraFiles: ["index.html"],
  },
  {
    name: "dashboard_cold",
    label: "Dashboard cold load",
    entries: [
      "index.html",
      "src/routes/_authenticated.tsx?tsr-split=component",
      "src/routes/_authenticated/index.tsx?tsr-split=component",
    ],
    extraFiles: ["index.html"],
  },
];

const args = parseArgs(process.argv.slice(2));
const distDir = path.resolve(args.dist ?? defaultDistDir);
const manifestPath = path.join(distDir, ".vite/manifest.json");

const manifest = JSON.parse(await readFile(manifestPath, "utf8"));
const metrics = metricConfigs.map((config) => buildGraphMetric(config));
metrics.push(buildAllAssetsMetric());

const result = {
  distDir: path.relative(repoRoot, distDir),
  generatedAt: new Date().toISOString(),
  metrics,
  largestAssets: listLargestAssets(10),
};

if (args.json) {
  console.log(JSON.stringify(result, null, 2));
} else {
  console.log(renderText(result));
}

if (args.markdown) {
  writeFileSync(path.resolve(args.markdown), renderMarkdown(result));
}

function parseArgs(rawArgs) {
  const parsed = {
    dist: undefined,
    json: false,
    markdown: undefined,
  };

  for (let i = 0; i < rawArgs.length; i += 1) {
    const arg = rawArgs[i];
    if (arg === "--json") {
      parsed.json = true;
    } else if (arg === "--markdown") {
      parsed.markdown = rawArgs[++i];
    } else if (arg.startsWith("--markdown=")) {
      parsed.markdown = arg.slice("--markdown=".length);
    } else if (arg === "--dist") {
      parsed.dist = rawArgs[++i];
    } else if (arg.startsWith("--dist=")) {
      parsed.dist = arg.slice("--dist=".length);
    } else {
      throw new Error(`Unknown argument: ${arg}`);
    }
  }

  return parsed;
}

function buildGraphMetric(config) {
  const files = collectGraphFiles(config.entries);
  for (const file of config.extraFiles ?? []) files.add(file);

  return buildMetric({
    name: config.name,
    label: config.label,
    files: [...files].sort(),
  });
}

function buildAllAssetsMetric() {
  const assetsDir = path.join(distDir, "assets");
  const files = readdirSync(assetsDir)
    .filter((file) => /\.(css|js)$/.test(file))
    .map((file) => `assets/${file}`)
    .sort();

  return buildMetric({
    name: "all_js_css_assets",
    label: "All JS/CSS assets",
    files,
  });
}

function buildMetric({ name, label, files }) {
  return {
    name,
    label,
    files: files.length,
    raw: sum(files, (file) => fileSize(file)),
    gzip: sum(files, (file) => encodedTransferSize(file, "gzip")),
    br: sum(files, (file) => encodedTransferSize(file, "br")),
  };
}

function collectGraphFiles(entryKeys) {
  const files = new Set();
  const seenKeys = new Set();

  for (const key of entryKeys) visitManifestEntry(key);

  return files;

  function visitManifestEntry(key) {
    if (seenKeys.has(key)) return;
    seenKeys.add(key);

    const entry = manifest[key];
    if (!entry) {
      throw new Error(`Manifest entry not found: ${key}`);
    }

    addFile(entry.file);
    for (const cssFile of entry.css ?? []) addFile(cssFile);
    for (const assetFile of entry.assets ?? []) addFile(assetFile);
    for (const importKey of entry.imports ?? []) visitManifestEntry(importKey);
  }

  function addFile(file) {
    if (file) files.add(file);
  }
}

function listLargestAssets(limit) {
  const assetsDir = path.join(distDir, "assets");
  return readdirSync(assetsDir)
    .filter((file) => /\.(css|js)$/.test(file))
    .map((file) => {
      const relPath = `assets/${file}`;
      return {
        file: relPath,
        raw: fileSize(relPath),
        gzip: encodedTransferSize(relPath, "gzip"),
        br: encodedTransferSize(relPath, "br"),
      };
    })
    .sort((a, b) => b.raw - a.raw)
    .slice(0, limit);
}

function encodedTransferSize(file, encoding) {
  if (!file.startsWith("assets/")) return fileSize(file);

  const suffix = encoding === "br" ? ".br" : ".gz";
  const sidecar = `${file}${suffix}`;
  if (existsSync(resolveDistFile(sidecar))) return fileSize(sidecar);

  return fileSize(file);
}

function fileSize(file) {
  const filePath = resolveDistFile(file);
  return statSync(filePath).size;
}

function resolveDistFile(file) {
  return path.join(distDir, file);
}

function renderText(data) {
  const lines = [
    "Frontend bundle metrics",
    `Dist: ${data.distDir}`,
    "",
    table(
      ["Metric", "Files", "Raw", "Gzip", "Brotli"],
      data.metrics.map((metric) => [
        metric.label,
        String(metric.files),
        formatBytes(metric.raw),
        formatBytes(metric.gzip),
        formatBytes(metric.br),
      ]),
    ),
    "",
    "Largest JS/CSS assets",
    table(
      ["Asset", "Raw", "Gzip", "Brotli"],
      data.largestAssets.map((asset) => [
        asset.file,
        formatBytes(asset.raw),
        formatBytes(asset.gzip),
        formatBytes(asset.br),
      ]),
    ),
  ];

  return lines.join("\n");
}

function renderMarkdown(data) {
  return [
    "## Frontend Bundle Metrics",
    "",
    `Generated from \`${data.distDir}/.vite/manifest.json\`.`,
    "",
    markdownTable(
      ["Metric", "Files", "Raw", "Gzip", "Brotli"],
      data.metrics.map((metric) => [
        metric.label,
        String(metric.files),
        formatBytes(metric.raw),
        formatBytes(metric.gzip),
        formatBytes(metric.br),
      ]),
    ),
    "",
    "### Largest JS/CSS Assets",
    "",
    markdownTable(
      ["Asset", "Raw", "Gzip", "Brotli"],
      data.largestAssets.map((asset) => [
        `\`${asset.file}\``,
        formatBytes(asset.raw),
        formatBytes(asset.gzip),
        formatBytes(asset.br),
      ]),
    ),
    "",
  ].join("\n");
}

function table(headers, rows) {
  const allRows = [headers, ...rows];
  const widths = headers.map((_, column) =>
    Math.max(...allRows.map((row) => row[column].length)),
  );

  return allRows
    .map((row, rowIndex) => {
      const line = row
        .map((cell, column) => cell.padEnd(widths[column]))
        .join("  ");
      if (rowIndex === 0) {
        return `${line}\n${widths.map((width) => "-".repeat(width)).join("  ")}`;
      }
      return line;
    })
    .join("\n");
}

function markdownTable(headers, rows) {
  return [
    `| ${headers.join(" | ")} |`,
    `| ${headers.map(() => "---").join(" | ")} |`,
    ...rows.map((row) => `| ${row.join(" | ")} |`),
  ].join("\n");
}

function formatBytes(bytes) {
  if (bytes >= 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(2)} MiB`;
  return `${(bytes / 1024).toFixed(1)} KiB`;
}

function sum(items, mapper) {
  return items.reduce((total, item) => total + mapper(item), 0);
}
