// Screens of the sync wizard (components/dialogs/SyncWizardDialog.vue) for review and the docs: serves web/dist (SPA
// fallback), answers the panel API with fixtures (no agent is called) and captures every step, in English, in a light
// and a dark theme.
//
//   node tests/visual/sync-shots.mjs <output dir> [theme…]     (after npm run build; default: default crystal-dark)
import { createReadStream, existsSync, mkdirSync, statSync } from 'node:fs'
import { createServer } from 'node:http'
import { extname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { chromium } from '@playwright/test'

const out = process.argv[2] || 'sync-shots'
const themes = process.argv.slice(3).length ? process.argv.slice(3) : ['default', 'crystal-dark']
const DIST = fileURLToPath(new URL('../../../web/dist/', import.meta.url))
const TYPES = {
  '.html': 'text/html',
  '.js': 'text/javascript',
  '.css': 'text/css',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.woff2': 'font/woff2',
}
mkdirSync(out, { recursive: true })

const server = createServer((req, res) => {
  let f = join(DIST, decodeURIComponent(new URL(req.url, 'http://x').pathname))
  if (!existsSync(f) || statSync(f).isDirectory()) f = join(DIST, 'index.html') // the SPA's routes
  res.writeHead(200, { 'Content-Type': TYPES[extname(f)] || 'application/octet-stream' })
  createReadStream(f).pipe(res)
}).listen(0, '127.0.0.1')
await new Promise((r) => server.once('listening', r))
const origin = `http://127.0.0.1:${server.address().port}`

const SHARES = ['Finance', 'Public', 'IT']
const PLAN = {
  New: 1284,
  NewBytes: 4.7e9,
  Changed: 37,
  ChangedBytes: 2.1e8,
  Delete: 0,
  DeleteBytes: 0,
  Same: 0,
  sample: { new: ['2026/Balance.xlsx', '2026/Invoices/INV-1041.pdf', 'Reports/Closing.docx'], changed: ['Accounts.xlsx'] },
}

const browser = await chromium.launch({ channel: 'chrome' })
try {
  for (const theme of themes) {
    const scheme = theme.endsWith('dark') ? 'dark' : 'light'
    const brand = { theme, lang: 'en', themes: [{ id: theme, label: theme, scheme }] }
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 }, deviceScaleFactor: 2 })
    let run = null
    const json = (body) => (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) })
    await page.route('**/api/**', json({})) // anything else: empty
    await page.route('**/api/i18n', json({}))
    await page.route('**/api/health', json({ status: 'ok', domain: 'example.local', netbios: 'EXAMPLE', brand }))
    await page.route(
      '**/api/me',
      json({ user: 'diego.rocha', domain: 'example.local', netbios: 'EXAMPLE', brand, settings: { lang: 'en', session_minutes: 30 } }),
    )
    await page.route('**/api/agents', json([{ id: 1, name: 'dc01', url: 'https://192.168.10.20:9443' }]))
    await page.route('**/api/agents/1/health', json({ status: 'ok', dc: true, files: true }))
    await page.route('**/api/agents/1/syncs/quarantines', json({ quarantines: [], space: [] }))
    await page.route('**/api/agents/1/syncs', (route) =>
      json({ jobs: [], runs: run ? [run] : [], roots: ['/srv/shares'], file_server: true, kerberos: true, machine: 'DC01$' })(route),
    )
    await page.route('**/api/agents/1/syncs/browse', (route) => {
      const b = route.request().postDataJSON()
      if (b.check)
        return json({
          steps: [
            { name: 'credentials', ok: true, detail: 'machine account DC01$ (Kerberos)' },
            { name: 'resolve', ok: true, detail: '192.168.10.30' },
            { name: 'port', ok: true },
            { name: 'signin', ok: true, items: SHARES },
          ],
        })(route)
      if (b.size) return json({ files: 1321, bytes: 4.9e9 })(route)
      return json({ dirs: b.path ? [] : ['2025', '2026', 'Reports'] })(route)
    })
    await page.route('**/api/agents/1/dirs*', json({ dirs: ['/srv/shares/Finance', '/srv/shares/Public'] }))
    await page.route('**/api/agents/1/syncs/validate', (route) => {
      const b = route.request().postDataJSON()
      if (b.dest) return json({ dest: { ok: true, exists: true, share: 'Finance', free_bytes: 2.1e11 } })(route)
      return json({ cron: { ok: true, next: '2026-10-20T02:00:00-03:00' } })(route)
    })
    await page.route('**/api/agents/1/syncs/*/preview', (route) => {
      run = { id: 'r1', job: 'finance', status: 'running', preview: true }
      return json(run)(route)
    })
    const shot = async (name) => {
      await page.waitForTimeout(400)
      await page.screenshot({ path: `${out}/sync-${theme}-${name}.png` })
    }
    // the tables while the agent has not answered: the loading spinner
    let release
    const held = new Promise((r) => (release = r))
    await page.route('**/api/agents/1/syncs', async (route) => {
      await held
      await route.fallback()
    })
    await page.goto(`${origin}/sync`)
    await page.locator('.grid-t [role="status"]').first().waitFor()
    await shot('0-loading')
    release()
    await page.getByRole('button', { name: /^new$/i }).first().click()
    await page.locator('#w-host').waitFor()
    await shot('1-source')
    await page.locator('#w-host').fill('fs-old.example.local')
    await page.getByRole('button', { name: /Test connection/i }).click()
    await page.getByText(/shares visible/i).waitFor()
    await shot('1-source-tested')
    await page.getByRole('button', { name: /^Next/i }).click()
    await page.getByRole('button', { name: 'Finance' }).click()
    await page.getByText('Reports').waitFor()
    await shot('2-what-to-copy')
    await page.getByRole('button', { name: /^Copy / }).click()
    await page.getByRole('button', { name: '/srv/shares', exact: true }).click()
    await page.getByRole('button', { name: /^use$/i }).first().click()
    await page.getByText(/The folder exists/i).waitFor()
    await shot('3-where-to')
    await page.getByRole('button', { name: /^Next/i }).click()
    await page.waitForTimeout(300)
    await shot('4-how-and-when')
    await page.getByRole('button', { name: /Save and preview/i }).click()
    await page.getByText(/Preview running|listing/i).waitFor()
    await shot('5-preview-running')
    run = { ...run, status: 'ok', result: { plan: PLAN } }
    await page
      .getByRole('button', { name: /Run now/i })
      .and(page.locator(':not([disabled])'))
      .waitFor({ timeout: 10_000 })
    await shot('5-preview-ready')
    await page.close()
  }
} finally {
  await browser.close()
  server.close()
}
