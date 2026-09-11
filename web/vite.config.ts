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
  build: {
    outDir: '../internal/dashboard/build/web',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api/llm-router': backendTarget,
    }
  }
})
