import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'

// API paths the dev server forwards to sarah
const apiPaths = ['/version', '/list', '/config', '/scan', '/watch', '/preview', '/events', '/i18n']

export default defineConfig(({ mode }) => {
  // Target for "pnpm dev", e.g. SARAH_API=http://raspberrypi.local:7575 pnpm dev
  const target = loadEnv(mode, '.', '').SARAH_API || 'http://localhost:7575'

  return {
    base: '/ui/',
    plugins: [vue()],
    build: {
      // embedded into the binary via go:embed (see src/ui.go)
      outDir: '../src/ui/dist',
      emptyOutDir: true,
    },
    server: {
      // en.json lives with the Go sources (src/locales/)
      fs: { allow: ['.', '../src/locales'] },
      proxy: Object.fromEntries(apiPaths.map((p) => [p, { target, changeOrigin: true }])),
    },
  }
})
