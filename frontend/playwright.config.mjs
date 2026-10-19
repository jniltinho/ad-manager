// Visual regression of the themes (theme-base): fixed browser, viewport, locale, timezone and fonts; the API is
// replayed from a HAR recorded once against the test Samba (RECORD=1), so the screens are deterministic.
import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: 'tests/visual',
  outputDir: '../.run/visual/results',
  workers: 1,
  // the CI runner (software rendering in a container) is several times slower than a workstation
  timeout: 300_000,
  reporter: [['list']],
  globalTeardown: './tests/visual/scrub.mjs',
  use: {
    baseURL: 'http://127.0.0.1:4810',
    // Google Chrome here; the CI container uses the Chromium bundled with this Playwright version (PW_CHANNEL)
    channel: process.env.PW_CHANNEL || 'chrome',
    viewport: { width: 1600, height: 1000 },
    deviceScaleFactor: 1,
    locale: 'pt-BR',
    timezoneId: 'America/Sao_Paulo',
    colorScheme: 'light',
    // software rendering and a fixed color profile: GPU compositing (e.g. under the modal backdrop) is not
    // pixel-deterministic
    launchOptions: {
      args: ['--disable-gpu', '--num-raster-threads=1', '--force-color-profile=srgb', '--disable-lcd-text', '--font-render-hinting=none'],
    },
  },
  webServer: {
    command: 'node tests/visual/serve.mjs',
    url: 'http://127.0.0.1:4810/',
    reuseExistingServer: false,
    env: {
      ...(process.env.RECORD ? { API_TARGET: 'http://127.0.0.1:8080' } : {}),
      ...(process.env.DIST ? { DIST: process.env.DIST } : {}),
    },
  },
})
