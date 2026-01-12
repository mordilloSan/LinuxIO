import React from 'react';
import ExampleModule from './component.jsx';

// Declare global type for production bundle mode
declare global {
  interface Window {
    LinuxIOModules: Record<string, { default: React.ComponentType }>;
  }
}

// REQUIRED: Export default for both HMR (dev) and bundled (prod) loads
export default ExampleModule;

// Export to window.LinuxIOModules for IIFE bundle mode (production)
// This runs only when loaded as a script tag, not when imported as ESM
if (typeof window !== 'undefined') {
  if (!window.LinuxIOModules) {
    window.LinuxIOModules = {};
  }
  if (!window.LinuxIOModules['example-module']) {
    window.LinuxIOModules['example-module'] = { default: ExampleModule };
    console.log('✅ Example Module loaded (bundle mode)');
  }
}
