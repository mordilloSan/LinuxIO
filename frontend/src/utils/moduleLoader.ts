import React from "react";

// Cache for loaded module components
const moduleCache = new Map<string, Promise<React.ComponentType>>();

/**
 * Check if we're in development mode with Vite dev server
 */
const isDevMode = import.meta.env.DEV;

/**
 * Dynamically load a module component
 * - In dev mode: Uses Vite's dynamic import with HMR support
 * - In prod mode: Loads from bundled script via script tag
 */
export function loadModuleComponent(url: string): Promise<React.ComponentType> {
  // Check cache first
  if (moduleCache.has(url)) {
    return moduleCache.get(url)!;
  }

  // Extract module name from URL like /modules/example-module/ui/component.js
  const moduleName = url.match(/\/modules\/([^/]+)\//)?.[1] || "unknown";

  const loadPromise = isDevMode
    ? loadModuleFromSource(moduleName)
    : loadModuleFromBundle(url, moduleName);

  moduleCache.set(url, loadPromise);
  return loadPromise;
}

/**
 * Glob import all module entry points for HMR
 * Vite will automatically watch these files and enable HMR
 */
const devModules = isDevMode
  ? import.meta.glob<{ default: React.ComponentType }>(
      "../../../modules/*/src/index.tsx",
      { eager: false },
    )
  : {};

// Debug: Log discovered modules in development
if (isDevMode) {
  console.log(
    "🔍 Vite discovered modules:",
    Object.keys(devModules).map((path) => path.replace("../../../modules/", "").replace("/src/index.tsx", "")),
  );
}

/**
 * Load module from source using Vite dynamic import (dev mode with HMR)
 */
async function loadModuleFromSource(
  moduleName: string,
): Promise<React.ComponentType> {
  try {
    // Build the expected path for this module
    const modulePath = `../../../modules/${moduleName}/src/index.tsx`;

    // Look up the module in our glob imports
    const moduleImporter = devModules[modulePath];

    if (!moduleImporter) {
      throw new Error(
        `Module ${moduleName} not found in project modules/. ` +
          `Expected at modules/${moduleName}/src/index.tsx. ` +
          `Make sure to link the module with: make link-module MODULE=${moduleName}`,
      );
    }

    // Import the module (Vite handles HMR)
    const module = await moduleImporter();

    if (!module.default) {
      throw new Error(
        `Module ${moduleName} does not have a default export at ${modulePath}`,
      );
    }

    console.log(`✅ Loaded module ${moduleName} from source (HMR enabled)`);
    return module.default;
  } catch (error) {
    console.error(`Failed to load module ${moduleName} from source:`, error);
    throw new Error(
      `Failed to load module ${moduleName}: ${error instanceof Error ? error.message : String(error)}`,
    );
  }
}

/**
 * Load module from bundled script (production mode)
 * Modules must export to window.LinuxIOModules[moduleName].default
 */
function loadModuleFromBundle(
  url: string,
  moduleName: string,
): Promise<React.ComponentType> {
  return new Promise<React.ComponentType>((resolve, reject) => {
    const script = document.createElement("script");
    script.type = "module";
    script.src = url;

    script.onload = () => {
      const mod = (window as any).LinuxIOModules?.[moduleName];

      if (!mod?.default) {
        reject(
          new Error(
            `Module ${moduleName} did not export default component to window.LinuxIOModules.${moduleName}.default`,
          ),
        );
        return;
      }

      console.log(`✅ Loaded module ${moduleName} from bundle`);
      resolve(mod.default);
    };

    script.onerror = () => {
      reject(new Error(`Failed to load module from ${url}`));
    };

    document.head.appendChild(script);
  });
}

/**
 * Create a lazy-loaded component wrapper for a module
 * This integrates with React.Suspense for loading states
 */
export function createModuleLazyComponent(url: string) {
  return React.lazy(() =>
    loadModuleComponent(url).then((Component) => ({ default: Component })),
  );
}
