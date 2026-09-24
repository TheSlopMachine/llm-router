import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

declare const process: { env: Record<string, string | undefined> }
const backendHost = process.env.VITE_BACKEND_HOST || 'localhost'
const backendPort = process.env.VITE_BACKEND_PORT || '38473'
const backendTarget = `http://${backendHost}:${backendPort}`

export default defineConfig({
  plugins: [
    svelte(),
  ],
  resolve: {
    alias: {
      $ui: '/src/components/ui/index.ts',
      $lib: '/src/lib',
    },
  },
  build: {
    outDir: '../internal/dashboard/build/web',
    emptyOutDir: true,
    // Vendor code changes on dependency bumps, app code changes daily.
    // Splitting them keeps the vendor chunk cached across deploys, and
    // pulls the markdown/highlight stack out of the critical path -- it is
    // only reachable from the lazily loaded Chat panel.
    rollupOptions: {
      output: {
        manualChunks(id: string) {
          if (!id.includes('node_modules')) return
          if (id.includes('highlight.js') || id.includes('marked') || id.includes('dompurify')) {
            return 'markdown'
          }
          return 'vendor'
        },
      },
    },
  },
  server: {
    proxy: {
      '/api/llm-router': backendTarget,
    }
  }
})
