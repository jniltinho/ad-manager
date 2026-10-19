import { copyFileSync, mkdirSync, writeFileSync } from 'node:fs'
import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite-plus'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

// The ad-manager SPA.
// - vp build → ../web/dist (embedded in the Go binary through go:embed)
// - vp dev   → proxies /api to the Go server (`ad-manager server --insecure-dev`, 127.0.0.1:8080)
export default defineConfig({
  plugins: [
    vue(),
    tailwindcss(),
    // go:embed needs at least one file in web/dist even without a build: recreate the versioned placeholder
    { name: 'gitkeep', closeBundle: () => writeFileSync(fileURLToPath(new URL('../web/dist/.gitkeep', import.meta.url)), '') },
    // Swagger UI (/swagger, off by default): only the two files the page uses, served from web/dist/swagger
    {
      name: 'swagger-ui',
      closeBundle: () => {
        const dir = fileURLToPath(new URL('../web/dist/swagger/', import.meta.url))
        mkdirSync(dir, { recursive: true })
        for (const f of ['swagger-ui-bundle.js', 'swagger-ui.css']) copyFileSync(`node_modules/swagger-ui-dist/${f}`, dir + f)
      },
    },
  ],
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  build: { outDir: '../web/dist', emptyOutDir: true },
  server: { port: 5173, proxy: { '/api': { target: 'http://127.0.0.1:8080', changeOrigin: true } } },
  // unit tests only: tests/visual is the Playwright suite (npm run test:visual)
  test: { environment: 'node', include: ['src/**/*.test.{js,mjs}', 'tests/unit/**/*.test.mjs'] },
  fmt: { singleQuote: true, semi: false, printWidth: 140 },
})
