// Screenshot of the Sync run-log modal (components/dialogs/SyncLogDialog.vue): serves web/dist, answers the panel API
// with fixtures (one finished preview run with a very long dry-run log, no agent is called) and captures the modal
// scrolling inside its box. English UI, company "Example Company Ltd", the domain.local demo, at 2×.
//
//   node tests/visual/sync-log-shot.mjs <output dir> [theme…]   (after npm run build; default: classic-blue)
import { createReadStream, existsSync, mkdirSync, statSync } from 'node:fs'
import { createServer } from 'node:http'
import { extname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { chromium } from '@playwright/test'

const out = process.argv[2] || 'sync-log-shot'
const themes = process.argv.slice(3).length ? process.argv.slice(3) : ['classic-blue']
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
  if (!existsSync(f) || statSync(f).isDirectory()) f = join(DIST, 'index.html')
  res.writeHead(200, { 'Content-Type': TYPES[extname(f)] || 'application/octet-stream' })
  createReadStream(f).pipe(res)
}).listen(0, '127.0.0.1')
await new Promise((r) => server.once('listening', r))
const origin = `http://127.0.0.1:${server.address().port}`

// a long dry-run log: the skip line per object that overflowed the page in production
const LOG = Array.from(
  { length: 2600 },
  (_, i) =>
    `2026/10/20 02:00:${String(i % 60).padStart(2, '0')} NOTICE: Finance/2026/Invoices/INV-${1000 + i}.pdf: Skipped set directory modification time as --dry-run is set`,
).join('\n')
const RUN = {
  id: 'r1',
  job: 'finance',
  status: 'ok',
  preview: true,
  started: '2026-10-20T02:00:00-03:00',
  result: { seconds: 12.4, plan: { New: 1284, NewBytes: 4.7e9, Changed: 37, ChangedBytes: 2.1e8, Delete: 0, DeleteBytes: 0, Same: 9021 } },
}
const JOB = {
  id: 'finance',
  name: 'Finance',
  source: { host: 'fs-old.domain.local', share: 'Finance', path: '', auth: 'kerberos' },
  dest: '/srv/shares/Finance',
  mode: 'copy',
  enabled: true,
  cron: '0 2 * * *',
  rev: 1,
  approved_rev: 1,
}

const browser = await chromium.launch({ channel: 'chrome' })
try {
  for (const theme of themes) {
    const scheme = theme.endsWith('dark') ? 'dark' : 'light'
    const brand = { theme, lang: 'en', company: 'Example Company Ltd', themes: [{ id: theme, label: theme, scheme }] }
    const page = await browser.newPage({ viewport: { width: 1600, height: 900 }, deviceScaleFactor: 2 })
    const json = (body) => (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) })
    await page.route('**/api/**', json({}))
    await page.route('**/api/health', json({ status: 'ok', domain: 'domain.local', netbios: 'EXAMPLE', brand }))
    await page.route(
      '**/api/me',
      json({ user: 'administrator', domain: 'domain.local', netbios: 'EXAMPLE', brand, settings: { lang: 'en', session_minutes: 30 } }),
    )
    await page.route('**/api/agents', json([{ id: 1, name: 'dc01', url: 'https://192.168.10.20:9443' }]))
    await page.route('**/api/agents/1/health', json({ status: 'ok', dc: true, files: true }))
    await page.route('**/api/agents/1/syncs/quarantines', json({ quarantines: [], space: [] }))
    await page.route(
      '**/api/agents/1/syncs',
      json({
        jobs: [JOB],
        runs: [RUN],
        roots: ['/srv/shares'],
        file_server: true,
        kerberos: true,
        machine: 'DC01$',
      }),
    )
    await page.route('**/api/agents/1/sync-runs/r1/log', (route) => route.fulfill({ status: 200, contentType: 'text/plain', body: LOG }))

    await page.goto(`${origin}/sync`)
    await page.getByRole('button', { name: 'Log' }).first().click()
    await page.locator('dialog[open] pre').waitFor()
    await page.waitForTimeout(400)
    await page.screenshot({ path: `${out}/sync-${theme}-log.png` }) // the viewport: the modal contained over the Sync page
    console.log('ok', `sync-${theme}-log`)
    await page.close()
  }
} finally {
  await browser.close()
  server.close()
}
