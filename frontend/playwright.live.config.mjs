// Theme runtime against a real panel (theme-base): first paint already themed and switching without reload.
// Needs the panel built with the current SPA (make build) and the demo login of the test domain:
//   LIVE_URL=http://127.0.0.1:8080 npx playwright test -c playwright.live.config.mjs
import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: 'tests/live',
  outputDir: '../.run/visual/live-results',
  workers: 1,
  reporter: [['list']],
  use: {
    baseURL: process.env.LIVE_URL || 'http://127.0.0.1:8080',
    channel: 'chrome',
    viewport: { width: 1600, height: 1000 },
    locale: 'pt-BR',
  },
})
