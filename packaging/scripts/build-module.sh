#!/bin/bash
# Build a LinuxIO module
# Usage: ./build-module.sh example-module [--dev]

set -e

if [ -z "$1" ]; then
  echo "Usage: $0 <module-name> [--dev]"
  echo ""
  echo "Options:"
  echo "  --dev     Build in development mode (no minification, includes source maps)"
  exit 1
fi

MODULE_NAME="$1"
BUILD_MODE="${2:-production}"

# Parse flags
if [ "$2" = "--dev" ]; then
  BUILD_MODE="development"
fi

LINUXIO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
MODULE_DIR="$(cd "$LINUXIO_ROOT/modules/$MODULE_NAME" && pwd)"

if [ ! -d "$MODULE_DIR" ]; then
  echo "Error: Module directory not found: $MODULE_DIR"
  exit 1
fi

if [ ! -f "$MODULE_DIR/src/index.tsx" ]; then
  echo "Error: Module entry point not found: $MODULE_DIR/src/index.tsx"
  exit 1
fi

echo " Building module: $MODULE_NAME"
echo "   Mode: $BUILD_MODE"
echo "   Module dir: $MODULE_DIR"
echo "   LinuxIO root: $LINUXIO_ROOT"

# Run vite from frontend directory with module-specific config
cd "$LINUXIO_ROOT/frontend"

# Create temporary vite config in frontend directory (so it can resolve node_modules)
TEMP_CONFIG="vite.config.module-build.tmp.js"
cat > "$TEMP_CONFIG" << 'EOF'
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { resolve } from 'path';

const moduleName = process.env.MODULE_NAME;
const moduleDir = process.env.MODULE_DIR;
const isProduction = process.env.NODE_ENV === 'production';

export default defineConfig({
  mode: isProduction ? 'production' : 'development',
  plugins: [
    react({
      jsxRuntime: 'classic',
    }),
  ],
  resolve: {
    alias: {
      // Match frontend tsconfig.json paths
      // IMPORTANT: Specific aliases must come before wildcards
      '@/constants': resolve(__dirname, 'src/theme/constants.ts'),
      '@/config': resolve(__dirname, 'src/config.ts'),
      '@/routes': resolve(__dirname, 'src/routes.tsx'),
      '@/theme': resolve(__dirname, 'src/theme/index.ts'),
      '@/api': resolve(__dirname, 'src/api'),
      '@/components': resolve(__dirname, 'src/components'),
      '@/contexts': resolve(__dirname, 'src/contexts'),
      '@/hooks': resolve(__dirname, 'src/hooks'),
      '@/layouts': resolve(__dirname, 'src/layouts'),
      '@/pages': resolve(__dirname, 'src/pages'),
      '@/types': resolve(__dirname, 'src/types'),
      '@/utils': resolve(__dirname, 'src/utils'),
      '@/assets': resolve(__dirname, 'src/assets'),
      '@/services': resolve(__dirname, 'src/services'),
      '@': resolve(__dirname, 'src'),
    },
  },
  build: {
    outDir: resolve(moduleDir, 'dist'),
    emptyOutDir: true,
    copyPublicDir: false,  // Don't copy public assets to module dist
    minify: isProduction,
    sourcemap: !isProduction,
    // Target modern browsers for smaller bundle
    target: 'es2020',
    lib: {
      entry: resolve(moduleDir, 'src/index.tsx'),
      formats: ['iife'],
      name: moduleName.replace(/-./g, (x) => x[1].toUpperCase()),
      fileName: () => 'component.js',
    },
    rolldownOptions: {
      external: [
        'react',
        'react-dom',
        'react/jsx-runtime',
        'react/jsx-dev-runtime',
      ],
      output: {
        globals: {
          react: 'window.React',
          'react-dom': 'window.ReactDOM',
          'react/jsx-runtime': 'window.React',
          'react/jsx-dev-runtime': 'window.React',
        },
        compact: isProduction,
        ...(isProduction && {
          banner: '/* LinuxIO Module - Built in production mode */',
        }),
      },
      treeshake: isProduction ? {
        moduleSideEffects: false,
        propertyReadSideEffects: false,
      } : false,
    },
  },
  // Logging
  logLevel: isProduction ? 'info' : 'warn',
});
EOF

# Build with environment variables
if [ "$BUILD_MODE" = "production" ]; then
  echo "   Optimizations: Minification, tree-shaking, compression"
  NODE_ENV=production MODULE_NAME="$MODULE_NAME" MODULE_DIR="$MODULE_DIR" npx vite build --config "$TEMP_CONFIG"
else
  echo "   Optimizations: None (source maps enabled for debugging)"
  NODE_ENV=development MODULE_NAME="$MODULE_NAME" MODULE_DIR="$MODULE_DIR" npx vite build --config "$TEMP_CONFIG"
fi

# Cleanup
rm "$TEMP_CONFIG"
echo ""
echo " Build complete: $MODULE_DIR/dist/component.js"

# Show file size comparison
if [ -f "$MODULE_DIR/dist/component.js" ]; then
  SIZE=$(du -h "$MODULE_DIR/dist/component.js" | cut -f1)
  echo "   Bundle size: $SIZE"
fi
