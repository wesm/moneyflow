import { svelte, vitePreprocess } from '@sveltejs/vite-plugin-svelte'
import { defineConfig } from 'vite'

export default defineConfig({
  base: './',
  plugins: [svelte({ preprocess: vitePreprocess({ script: true }) })],
  optimizeDeps: {
    exclude: ['@kenn-io/kit-ui', 'layerchart'],
  },
  server: {
    host: '127.0.0.1',
    port: 5173,
    strictPort: true,
  },
})
