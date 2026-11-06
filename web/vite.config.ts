import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'

// API-Pfade, die im Dev-Server an sarah weitergereicht werden
const apiPaths = ['/version', '/list', '/config', '/scan', '/watch', '/preview', '/events', '/i18n']

export default defineConfig(({ mode }) => {
  // Ziel für "pnpm dev", z. B. SARAH_API=http://raspberrypi.local:7575 pnpm dev
  const target = loadEnv(mode, '.', '').SARAH_API || 'http://localhost:7575'

  return {
    base: '/ui/',
    plugins: [vue()],
    build: {
      // wird per go:embed ins Binary eingebettet (siehe src/ui.go)
      outDir: '../src/ui/dist',
      emptyOutDir: true,
    },
    server: {
      // en.json liegt bei den Go-Quellen (src/locales/)
      fs: { allow: ['.', '../src/locales'] },
      proxy: Object.fromEntries(apiPaths.map((p) => [p, { target, changeOrigin: true }])),
    },
  }
})
