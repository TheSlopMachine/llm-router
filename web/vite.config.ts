import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import webfontDownload from 'vite-plugin-webfont-dl'
import sveltePreprocess from 'svelte-preprocess'

export default defineConfig({
  plugins: [
    svelte({
      preprocess: sveltePreprocess()
    }),
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
      '/dashboard/api': 'http://localhost:8080',
      '/login': 'http://localhost:8080',
      '/logout': 'http://localhost:8080',
      '/bootstrap': 'http://localhost:8080',
    }
  }
})
