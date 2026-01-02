import React from "react";

// Cache for loaded module components
const moduleCache = new Map<string, Promise<React.ComponentType>>();

/**
 * Dynamically load a module component from a remote URL
 * Modules must export their component to window.LinuxIOModules[moduleName].default
 */
export function loadModuleComponent(url: string): Promise<React.ComponentType> {
  // Check cache first
  if (moduleCache.has(url)) {
    return moduleCache.get(url)!;
  }

  const loadPromise = new Promise<React.ComponentType>((resolve, reject) => {
    const script = document.createElement("script");
    script.type = "module";
    script.src = url;

    script.onload = () => {
      // Extract module name from URL like /modules/example-module/component.js
      const moduleName = url.match(/\/modules\/([^/]+)\//)?.[1] || "unknown";
      const mod = (window as any).LinuxIOModules?.[moduleName];

      if (!mod?.default) {
        reject(
          new Error(
            `Module ${moduleName} did not export default component to window.LinuxIOModules.${moduleName}.default`,
          ),
        );
        return;
      }

      resolve(mod.default);
    };

    script.onerror = () => {
      reject(new Error(`Failed to load module from ${url}`));
    };

    document.head.appendChild(script);
  });

  moduleCache.set(url, loadPromise);
  return loadPromise;
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
