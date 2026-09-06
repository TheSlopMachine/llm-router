import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import webfontDownload from 'vite-plugin-webfont-dl'

declare const process: { env: Record<string, string | undefined> }
const backendHost = process.env.VITE_BACKEND_HOST || 'localhost'
const backendPort = process.env.VITE_BACKEND_PORT || '38473'
const backendTarget = `http://${backendHost}:${backendPort}`

export default defineConfig({
  plugins: [
    svelte(),
    webfontDownload([
      'https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600&family=DM+Mono:wght@400&display=swap',
      'https://fonts.googleapis.com/css2?family=Material+Symbols+Outlined:opsz,wght,FILL,GRAD@20,300,0,0'
    ])
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
