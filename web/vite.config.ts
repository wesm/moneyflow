import { svelte, vitePreprocess } from '@sveltejs/vite-plugin-svelte'
import { defineConfig, type Plugin } from 'vite'

import { productionMarker } from './scripts/validate-assets'

function productionMarkerPlugin(): Plugin {
  return {
    name: 'moneyflow-production-marker',
    apply: 'build',
    generateBundle() {
      this.emitFile({
        type: 'asset',
        fileName: '.moneyflow-production.json',
        source: `${JSON.stringify(productionMarker)}\n`,
      })
    },
  }
}

function devPort(name: string, fallback: number): number {
  const value = process.env[name]
  if (value === undefined || value === '') return fallback
  const port = Number(value)
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    throw new Error(`${name} must be a TCP port number, got ${JSON.stringify(value)}`)
  }
  return port
}

// `make dev` sets these. Both servers listen only on 127.0.0.1.
const devWebPort = devPort('MONEYFLOW_DEV_WEB_PORT', 5173)
const devBackend = `http://127.0.0.1:${devPort('MONEYFLOW_DEV_BACKEND_PORT', 8081)}`

// The Go server fills these placeholders in production. In development Vite serves index.html,
// so this fills them for the root base path. The empty mutation token makes the client fetch one
// from the proxied bootstrap endpoint before its first mutation. Run before Vite's own transforms,
// which would otherwise rewrite the canonical href.
function devIndexPlugin(): Plugin {
  return {
    name: 'moneyflow-dev-index',
    apply: 'serve',
    transformIndexHtml: {
      order: 'pre',
      handler: (html) =>
        html
          .replaceAll('__MONEYFLOW_BASE_HREF__', '/')
          .replaceAll('__MONEYFLOW_BASE_PATH__', '/')
          .replaceAll('__MONEYFLOW_MUTATION_TOKEN__', '')
          .replaceAll('__MONEYFLOW_CANONICAL_URL__', `http://127.0.0.1:${devWebPort}/`)
          .replaceAll('__MONEYFLOW_ORIGIN_WARNING__', ''),
    },
  }
}

export default defineConfig({
  base: './',
  plugins: [
    svelte({ preprocess: vitePreprocess({ script: true }) }),
    productionMarkerPlugin(),
    devIndexPlugin(),
  ],
  build: {
    manifest: true,
    sourcemap: false,
    // budgets.json is the size gate; this sits above the raw size its compressed limits allow.
    chunkSizeWarningLimit: 600,
    rollupOptions: {
      output: {
        assetFileNames: 'assets/[name]-[hash][extname]',
        chunkFileNames: 'assets/[name]-[hash].js',
        entryFileNames: 'assets/[name]-[hash].js',
      },
    },
  },
  optimizeDeps: {
    exclude: ['@kenn-io/kit-ui'],
  },
  server: {
    host: '127.0.0.1',
    port: devWebPort,
    strictPort: true,
    // No changeOrigin: the backend runs with --external-url set to this server, so the browser's
    // own Origin must reach it unchanged for mutation checks to pass.
    proxy: {
      '/api/': { target: devBackend, timeout: 0, proxyTimeout: 0 },
      '/openapi.json': devBackend,
      '/openapi.yaml': devBackend,
      '/demo/': devBackend,
    },
  },
})
