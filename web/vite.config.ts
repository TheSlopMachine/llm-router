import type { Plugin } from 'vite'
import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

declare const process: { env: Record<string, string | undefined> }
const backendHost = process.env.VITE_BACKEND_HOST || 'localhost'
const backendPort = process.env.VITE_BACKEND_PORT || '38473'
const backendTarget = `http://${backendHost}:${backendPort}`


// Preload the (hashed) icon font so icons never flash as raw ligature text.
function preloadIconFont(): Plugin {
  return {
    name: 'fui-preload-icon-font',
    transformIndexHtml: {
      order: 'post',
      handler(html, ctx) {
        const bundle = ctx.bundle
        if (!bundle) return html
        const file = Object.keys(bundle).find((k) => /material-symbols-outlined.*\.woff2$/.test(k))
        if (!file) return html
        return {
          html,
          tags: [{ tag: 'link', attrs: { rel: 'preload', as: 'font', type: 'font/woff2', crossorigin: '', href: '/' + file }, injectTo: 'head-prepend' }],
        }
      },
    },
  }
}

export default defineConfig({
  plugins: [preloadIconFont(), 
    svelte(),
  ],
  resolve: {
    alias: {
      $ui: '/src/FUI/index.ts',
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
