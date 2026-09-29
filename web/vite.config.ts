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

export default defineConfig({
  base: './',
  plugins: [svelte({ preprocess: vitePreprocess({ script: true }) }), productionMarkerPlugin()],
  build: {
    manifest: true,
    sourcemap: false,
    rollupOptions: {
      output: {
        assetFileNames: 'assets/[name]-[hash][extname]',
        chunkFileNames: 'assets/[name]-[hash].js',
        entryFileNames: 'assets/[name]-[hash].js',
      },
    },
  },
  optimizeDeps: {
    exclude: ['@kenn-io/kit-ui', 'layerchart'],
  },
  server: {
    host: '127.0.0.1',
    port: 5173,
    strictPort: true,
  },
})
