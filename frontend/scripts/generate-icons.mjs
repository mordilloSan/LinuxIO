/**
 * Scans the frontend source tree for Iconify ids and emits only the icon
 * collections actually referenced by the app.
 *
 * Run manually with: node scripts/generate-icons.mjs
 * Vite also calls this automatically before serving or building.
 */

import { mkdirSync, readdirSync, readFileSync, writeFileSync } from "fs";
import { dirname, extname, join, resolve } from "path";
import { fileURLToPath } from "url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const frontendDir = resolve(__dirname, "..");
const sourceDir = resolve(frontendDir, "src");
const outputPath = resolve(frontendDir, "src/icons/icons.ts");
const shellOutputPath = resolve(frontendDir, "src/icons/shell.ts");

const SOURCE_EXTENSIONS = new Set([".js", ".jsx", ".ts", ".tsx"]);
const ICON_PATTERN = /\b([a-z0-9-]+):([a-z0-9-]+)\b/g;
const INDENT = "  ";
const IDENTIFIER_PATTERN = /^[A-Za-z_$][A-Za-z0-9_$]*$/;
const GENERATED_OUTPUTS = new Set([outputPath, shellOutputPath]);
const SHELL_SOURCE_PREFIXES = [
  "src/components/auth/",
  "src/components/errors/",
  "src/components/navbar/",
  "src/components/sidebar/",
  "src/components/tabbar/",
  "src/components/ui/",
  "src/components/update/",
  "src/layouts/",
  "src/pages/auth/",
];

// Add icon ids here when they are composed dynamically and cannot be discovered
// from string literals in the source tree.
const EXTRA_ICONS = {};

function formatKey(key) {
  return IDENTIFIER_PATTERN.test(key) ? key : JSON.stringify(key);
}

function formatString(value) {
  return `'${value
    .replaceAll("\\", "\\\\")
    .replaceAll("'", "\\'")
    .replaceAll("\n", "\\n")
    .replaceAll("\r", "\\r")}'`;
}

function formatValue(value, indentLevel = 0) {
  if (typeof value === "string") {
    return formatString(value);
  }

  if (
    typeof value === "number" ||
    typeof value === "boolean" ||
    value === null
  ) {
    return JSON.stringify(value);
  }

  if (Array.isArray(value)) {
    if (value.length === 0) {
      return "[]";
    }

    const indent = INDENT.repeat(indentLevel);
    const nextIndent = INDENT.repeat(indentLevel + 1);
    const lines = ["["];

    for (const item of value) {
      lines.push(`${nextIndent}${formatValue(item, indentLevel + 1)},`);
    }

    lines.push(`${indent}]`);
    return lines.join("\n");
  }

  const entries = Object.entries(value);

  if (entries.length === 0) {
    return "{}";
  }

  const indent = INDENT.repeat(indentLevel);
  const nextIndent = INDENT.repeat(indentLevel + 1);
  const lines = ["{"];

  for (const [key, entryValue] of entries) {
    lines.push(
      `${nextIndent}${formatKey(key)}: ${formatValue(entryValue, indentLevel + 1)},`,
    );
  }

  lines.push(`${indent}}`);
  return lines.join("\n");
}

function walkFiles(dir) {
  const entries = readdirSync(dir, { withFileTypes: true }).sort((left, right) =>
    left.name.localeCompare(right.name),
  );

  const files = [];

  for (const entry of entries) {
    const fullPath = join(dir, entry.name);

    if (entry.isDirectory()) {
      files.push(...walkFiles(fullPath));
      continue;
    }

    if (!SOURCE_EXTENSIONS.has(extname(entry.name))) {
      continue;
    }

    files.push(fullPath);
  }

  return files;
}

function sourcePath(filePath) {
  return filePath
    .slice(frontendDir.length + 1)
    .replaceAll("\\", "/");
}

function isShellSource(filePath) {
  const path = sourcePath(filePath);
  return SHELL_SOURCE_PREFIXES.some((prefix) => path.startsWith(prefix));
}

function readIconifyPackageNames() {
  const packageJson = JSON.parse(
    readFileSync(resolve(frontendDir, "package.json"), "utf-8"),
  );

  const dependencyGroups = [
    packageJson.dependencies ?? {},
    packageJson.devDependencies ?? {},
  ];

  const packages = new Set();

  for (const group of dependencyGroups) {
    for (const packageName of Object.keys(group)) {
      if (packageName.startsWith("@iconify-json/")) {
        packages.add(packageName.replace("@iconify-json/", ""));
      }
    }
  }

  return [...packages].sort();
}

function loadCollections() {
  const collections = new Map();

  for (const packageName of readIconifyPackageNames()) {
    const jsonPath = resolve(
      frontendDir,
      `node_modules/@iconify-json/${packageName}/icons.json`,
    );
    const data = JSON.parse(readFileSync(jsonPath, "utf-8"));
    collections.set(data.prefix, data);
  }

  return collections;
}

function createRequestedIconMap(availablePrefixes) {
  return new Map([...availablePrefixes].map((prefix) => [prefix, new Set()]));
}

function collectRequestedIcons(availablePrefixes, shouldIncludeFile = () => true) {
  const requested = new Map(
    [...availablePrefixes].map((prefix) => [prefix, new Set()]),
  );

  for (const filePath of walkFiles(sourceDir)) {
    if (GENERATED_OUTPUTS.has(filePath) || !shouldIncludeFile(filePath)) {
      continue;
    }

    const source = readFileSync(filePath, "utf-8");

    for (const match of source.matchAll(ICON_PATTERN)) {
      const [, prefix, name] = match;

      if (!availablePrefixes.has(prefix)) {
        continue;
      }

      requested.get(prefix).add(name);
    }
  }

  return requested;
}

function addExtraIcons(requested) {
  for (const [prefix, names] of Object.entries(EXTRA_ICONS)) {
    if (!requested.has(prefix)) {
      requested.set(prefix, new Set());
    }

    for (const name of names) {
      requested.get(prefix).add(name);
    }
  }
}

function subtractRequestedIcons(requestedIcons, iconsToRemove) {
  const result = createRequestedIconMap(requestedIcons.keys());

  for (const [prefix, names] of requestedIcons) {
    const excludedNames = iconsToRemove.get(prefix) ?? new Set();

    for (const name of names) {
      if (!excludedNames.has(name)) {
        result.get(prefix).add(name);
      }
    }
  }

  return result;
}

function extractIcons(data, names) {
  const icons = {};
  const missing = [];

  for (const name of names) {
    const icon = data.icons[name];

    if (!icon) {
      missing.push(`${data.prefix}:${name}`);
      continue;
    }

    icons[name] = icon;
  }

  return { icons, missing };
}

function buildOutput(collections, requestedIcons, sourceDescription) {
  const sections = [];
  const missingIcons = [];
  let totalIcons = 0;

  for (const prefix of [...requestedIcons.keys()].sort()) {
    const names = [...requestedIcons.get(prefix)].sort();

    if (names.length === 0) {
      continue;
    }

    const collection = collections.get(prefix);
    if (!collection) {
      missingIcons.push(...names.map((name) => `${prefix}:${name}`));
      continue;
    }

    const { icons, missing } = extractIcons(collection, names);
    const count = Object.keys(icons).length;

    if (count === 0) {
      missingIcons.push(...missing);
      continue;
    }

    totalIcons += count;
    missingIcons.push(...missing);

    sections.push(`// ${prefix} - ${count} icon${count !== 1 ? "s" : ""}
addCollection({
  prefix: ${JSON.stringify(prefix)},
  width: ${collection.width ?? 24},
  height: ${collection.height ?? 24},
  icons: ${formatValue(icons, 1)},
});`);
  }

  const header = [
    "// Auto-generated by scripts/generate-icons.mjs - do not edit manually.",
    `// ${sourceDescription}`,
  ];

  const body = [];

  if (sections.length > 0) {
    body.push('import { addCollection } from "@iconify/react";', "");
    body.push(sections.join("\n\n"));
  }

  const output = `${header.join("\n")}\n${body.length > 0 ? `\n${body.join("\n")}\n` : "\n"}`;

  return {
    output,
    totalIcons,
    missingIcons,
    collectionCount: sections.length,
  };
}

export function generateIcons() {
  const collections = loadCollections();
  const availablePrefixes = new Set(collections.keys());
  const requestedIcons = collectRequestedIcons(availablePrefixes);
  addExtraIcons(requestedIcons);

  const shellRequestedIcons = collectRequestedIcons(
    availablePrefixes,
    isShellSource,
  );
  const routeRequestedIcons = subtractRequestedIcons(
    requestedIcons,
    shellRequestedIcons,
  );

  const shellResult = buildOutput(
    collections,
    shellRequestedIcons,
    "Generated from Iconify ids used by auth, layout, navbar, sidebar, and shared shell components.",
  );
  const routeResult = buildOutput(
    collections,
    routeRequestedIcons,
    "Generated from route-specific Iconify ids, excluding icons already in shell.ts.",
  );

  mkdirSync(resolve(frontendDir, "src/icons"), { recursive: true });
  writeFileSync(shellOutputPath, shellResult.output);
  writeFileSync(outputPath, routeResult.output);

  console.log(
    `[icons] Generated ${shellResult.totalIcons} shell icon${shellResult.totalIcons !== 1 ? "s" : ""} from ${shellResult.collectionCount} collection${shellResult.collectionCount !== 1 ? "s" : ""}.`,
  );
  console.log(
    `[icons] Generated ${routeResult.totalIcons} route icon${routeResult.totalIcons !== 1 ? "s" : ""} from ${routeResult.collectionCount} collection${routeResult.collectionCount !== 1 ? "s" : ""}.`,
  );

  const missingIcons = [
    ...shellResult.missingIcons,
    ...routeResult.missingIcons,
  ];

  if (missingIcons.length > 0) {
    console.warn(
      `[icons] Skipped missing icons: ${missingIcons.sort().join(", ")}`,
    );
  }
}

const executedPath = process.argv[1] && resolve(process.argv[1]);

if (executedPath === fileURLToPath(import.meta.url)) {
  generateIcons();
}
