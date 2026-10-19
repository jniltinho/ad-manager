// Screens of the setup wizard (pages/Setup.vue) for review and the docs: serves web/dist, answers the setup API with
// fixtures (no server is changed) and captures every step, in English, in a light and a dark theme.
//
//   node tests/visual/setup-shots.mjs <output dir> [theme…]     (after npm run build; default: default crystal-dark)
import { spawn } from 'node:child_process'
import { mkdirSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { chromium } from '@playwright/test'

const out = process.argv[2] || 'setup-shots'
const themes = process.argv.slice(3).length ? process.argv.slice(3) : ['default', 'crystal-dark']
const DIST = fileURLToPath(new URL('../../../web/dist/', import.meta.url))
const PORT = 4821
mkdirSync(out, { recursive: true })

const DIAG = {
  hostname: 'dc01',
  fqdn: 'dc01.domain.local',
  os: 'Ubuntu 26.04 LTS',
  packages: {
    samba: false,
    'samba-ad-dc': false,
    winbind: false,
    'krb5-user': false,
    'python3-samba': false,
    'tdb-tools': true,
    'redis-server': true,
    chrony: true,
  },
  samba_role: '',
  realm: '',
  samba_files: false,
  ports_in_use: { 53: true, 88: false, 135: false, 389: false, 445: false, 636: false },
  clock_synced: false,
  disk_free_mib: 6 * 1024,
  services: {},
  ips: ['192.168.10.20', '10.0.2.15'],
}
const STEPS = [
  'distro packages',
  'more than one network card: Samba stays on eth1 (192.168.10.20) only',
  '/etc/hosts: dc01.domain.local and dc01 → 192.168.10.20',
  'provisioning domain.local (DOMAIN)',
  'administrator password',
  'ad-manager and dependencies',
  'panel: https://192.168.10.20:8443  (sign in with a Domain Admins user)',
]
const PLAN = STEPS.flatMap((s, i) => [JSON.stringify({ step: s }), `[dry-run] command ${i + 1} of ${s.split(' ')[0]} …`])

const server = spawn('python3', ['-m', 'http.server', String(PORT), '--bind', '127.0.0.1', '--directory', DIST], { stdio: 'ignore' })
await new Promise((r) => setTimeout(r, 800))
const browser = await chromium.launch({ channel: 'chrome' })
try {
  for (const theme of themes) {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 }, deviceScaleFactor: 2 })
    let status = { state: 'running', finished: false, lines: STEPS.slice(0, 3).map((s) => JSON.stringify({ step: s })) }
    const json =
      (body, code = 200) =>
      (route) =>
        route.fulfill({ status: code, contentType: 'application/json', body: JSON.stringify(body) })
    await page.route('**/api/i18n', json({}))
    await page.route(
      '**/api/health',
      json({ setup: true, brand: { theme, lang: 'en', themes: [{ id: theme, scheme: theme.endsWith('dark') ? 'dark' : 'light' }] } }),
    )
    await page.route('**/api/setup/login', json({}, 204))
    await page.route('**/api/setup/diagnose', json(DIAG))
    await page.route('**/api/setup/plan', json({ lines: PLAN }))
    await page.route('**/api/setup/run', json({}))
    await page.route('**/api/setup/status*', (route) => {
      const s = status
      status = { ...status, lines: [] } // later polls bring nothing new
      return json(s)(route)
    })
    const shot = async (name) => {
      await page.waitForTimeout(350) // transitions
      await page.screenshot({ path: `${out}/setup-${theme}-${name}.png`, fullPage: true })
    }
    await page.goto(`http://127.0.0.1:${PORT}/`)
    await page.locator('#f-token').waitFor()
    await shot('1-access')
    await page.locator('#f-token').fill('a1b2c3d4e5f6')
    await page.keyboard.press('Enter')
    await page.locator('#h-server').waitFor()
    await shot('2-server')
    await page.getByRole('button', { name: /Continue/ }).click()
    await shot('3-role')
    await page.getByRole('button', { name: /Continue/ }).click()
    await page.locator('#f-realm').fill('domain')
    await page.locator('#f-password').fill('weak')
    await page.locator('#f-confirm').fill('weak')
    await page.getByRole('button', { name: 'Review', exact: true }).click()
    await shot('4-details-errors')
    await page.locator('#f-realm').fill('domain.local')
    await page.locator('#f-password').fill('Strong-pass-2026')
    await page.locator('#f-confirm').fill('Strong-pass-2026')
    await shot('5-details')
    await page.getByRole('button', { name: 'Review', exact: true }).click()
    await page.getByRole('button', { name: /Install now/ }).waitFor()
    await shot('6-review')
    await page.getByRole('button', { name: /Install now/ }).click()
    await page.getByRole('progressbar').waitFor()
    await shot('7-installing')
    status = {
      state: 'ok',
      finished: true,
      lines: STEPS.slice(3).map((s) => JSON.stringify({ step: s })),
      log_file: '/var/log/ad-manager/setup.log',
    }
    await page.getByText(/Next steps/).waitFor({ timeout: 10_000 })
    await shot('8-done')
    await page.close()
  }
} finally {
  await browser.close()
  server.kill()
}
