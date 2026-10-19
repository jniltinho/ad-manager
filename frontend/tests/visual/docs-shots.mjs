// Screenshots of docs/screenshots from a real panel (the all-in-one image with domain.local seeded by
// scripts/demo-seed.sh, see docs/screenshots/README.md), with the panel in English. Each shot is independent: one that fails is reported and the
// others go on. The password comes from the environment only.
//
//   DOCS_URL=https://127.0.0.1:18443 DOCS_PASS=… node tests/visual/docs-shots.mjs ../docs/screenshots [name…]
import { mkdirSync } from 'node:fs'
import { chromium } from '@playwright/test'

const out = process.argv[2] || 'docs-shots'
const only = process.argv.slice(3)
const P = process.env.DOCS_URL || 'https://127.0.0.1:18443'
const USER = process.env.DOCS_USER || 'Administrator'
const PASS = process.env.DOCS_PASS
if (!PASS) throw new Error('DOCS_PASS not set')
{
  const u = new URL(P)
  if (u.protocol !== 'https:' || !['127.0.0.1', 'localhost'].includes(u.hostname) || u.username || u.password)
    throw new Error('only a local panel')
}
mkdirSync(out, { recursive: true })

const browser = await chromium.launch({ channel: process.env.PW_CHANNEL || 'chrome' })
const ctx = await browser.newContext({ viewport: { width: 1600, height: 900 }, ignoreHTTPSErrors: true, locale: 'en-US' })
await ctx.addInitScript(() => localStorage.setItem('ad-manager.lang', localStorage.getItem('ad-manager.lang') || 'en'))
const page = await ctx.newPage()

const settle = async (p = page) => {
  await p.waitForLoadState('networkidle').catch(() => {})
  await p
    .locator('[role="status"]')
    .first()
    .waitFor({ state: 'detached', timeout: 15_000 })
    .catch(() => {})
  await p.waitForTimeout(500)
}
const snap = async (name, p = page) => {
  await settle(p)
  await p.screenshot({ path: `${out}/${name}.png` })
  console.log('ok', name)
}
const go = async (path) => {
  await page.goto(P + path)
  await settle()
}
const row = (text) => page.locator('tr', { hasText: text }).first()
const button = (name, scope = page) => scope.getByRole('button', { name, exact: false }).first()
const close = async () => {
  await page.keyboard.press('Escape')
  await page.waitForTimeout(300)
}
async function signIn(p = page) {
  await p.goto(P + '/')
  await p.locator('input[type=password]').waitFor()
  await p.locator('input').first().fill(USER)
  await p.locator('input[type=password]').fill(PASS)
  await p.keyboard.press('Enter')
  await p.getByRole('button', { name: /sign out/i }).waitFor()
}

let link = ''
const shots = [
  [
    '01-login',
    async () => {
      await page.goto(P + '/')
      await page.locator('input[type=password]').waitFor()
      await snap('01-login')
      await signIn()
    },
  ],
  [
    '02-home',
    async () => {
      await go('/')
      await snap('02-home')
    },
  ],
  [
    '03-users',
    async () => {
      await go('/users')
      await snap('03-users')
    },
  ],
  [
    '04-reset-password',
    async () => {
      await go('/users')
      await button(/^password$/i, row('ana.souza')).click()
      await snap('04-reset-password')
    },
  ],
  [
    '21-password-link-validity',
    async () => {
      await button(/send link to the user/i, page.locator('.cardtabs')).click()
      await snap('21-password-link-validity')
    },
  ],
  [
    '22-password-link-generated',
    async () => {
      await button(/generate link/i).click()
      const field = page.locator('#lu')
      await field.waitFor()
      link = await field.inputValue()
      await snap('22-password-link-generated')
      await close()
    },
  ],
  [
    '25-password-link-page',
    async () => {
      const anon = await browser.newContext({
        viewport: { width: 1600, height: 900 },
        ignoreHTTPSErrors: true,
        locale: 'en-US',
      })
      const p = await anon.newPage()
      await p.goto(link.replace(/^https:\/\/[^/]+/, P))
      await p.locator('input[type=password]').first().waitFor()
      await snap('25-password-link-page', p)
      await anon.close()
    },
  ],
  [
    '05-new-user',
    async () => {
      await go('/users')
      await button(/new user/i).click()
      await snap('05-new-user')
      await close()
    },
  ],
  [
    '06-confirm-delete',
    async () => {
      await go('/users')
      await button(/delete/i, row('bruno.lima')).click()
      await snap('06-confirm-delete')
      await close()
    },
  ],
  [
    '07-groups',
    async () => {
      await go('/groups')
      await snap('07-groups')
    },
  ],
  [
    '08-group-members',
    async () => {
      await go('/groups')
      await button(/members/i, row('Finance')).click()
      await snap('08-group-members')
      await close()
    },
  ],
  [
    '09-ous',
    async () => {
      await go('/ous')
      await snap('09-ous')
    },
  ],
  [
    '26-gpos',
    async () => {
      await go('/ous')
      await button(/^GPOs$/).click()
      await snap('26-gpos')
      await close()
    },
  ],
  [
    '10-computers',
    async () => {
      await go('/computers')
      await snap('10-computers')
    },
  ],
  [
    '27-dns',
    async () => {
      await go('/computers')
      await button(/^DNS$/).click()
      await snap('27-dns')
      await close()
    },
  ],
  [
    '28-smb-sessions',
    async () => {
      await go('/computers')
      await button(/sessions/i).click()
      await snap('28-smb-sessions')
      await close()
    },
  ],
  [
    '11-shares',
    async () => {
      await go('/shares')
      await snap('11-shares')
    },
  ],
  [
    '12-edit-share',
    async () => {
      await go('/shares')
      await button(/edit/i, row('Finance')).click()
      await page
        .getByText(/windows permissions/i)
        .first()
        .waitFor()
      await snap('12-edit-share')
      await close()
    },
  ],
  [
    '13-password-policy',
    async () => {
      await go('/password-policy')
      await snap('13-password-policy')
    },
  ],
  [
    '14-import-export',
    async () => {
      await go('/import-export')
      await snap('14-import-export')
    },
  ],
  [
    '29-backup',
    async () => {
      await go('/backup')
      await snap('29-backup')
    },
  ],
  [
    '30-sync',
    async () => {
      await go('/sync')
      await snap('30-sync')
    },
  ],
  [
    '31-sync-wizard',
    async () => {
      await go('/sync')
      await button(/^new$/i).click()
      await page.locator('#w-host').waitFor()
      await page.locator('#w-host').fill('dc1.domain.local')
      if (process.env.DOCS_SYNC_PASS) {
        // the container's machine account has no Kerberos ticket: a test account
        await page.getByRole('radio', { name: /user and password/i }).click()
        await page.locator('#w-user').fill(process.env.DOCS_SYNC_USER || 'svc.sync')
        await page.locator('#w-dom').fill('DOMAIN')
        await page.locator('#w-pw').fill(process.env.DOCS_SYNC_PASS)
      }
      await button(/test connection/i).click()
      await page
        .getByText(/shares visible/i)
        .first()
        .waitFor({ timeout: 30_000 })
      await snap('31-sync-wizard')
    },
  ],
  [
    '32-sync-wizard-what-to-copy',
    async () => {
      await button(/^next/i).click()
      await button(/^Public$/).click()
      await page
        .getByRole('button', { name: /^size$/i })
        .first()
        .waitFor({ timeout: 30_000 }) // the folders listed
      await snap('32-sync-wizard-what-to-copy')
      await close()
    },
  ],
  [
    '15-logs-audit',
    async () => {
      await go('/logs')
      await snap('15-logs-audit')
    },
  ],
  [
    '16-logs-samba',
    async () => {
      await go('/logs')
      await button(/samba — dc1/i).click()
      await snap('16-logs-samba')
    },
  ],
  [
    '23-logs-http',
    async () => {
      await go('/logs')
      await button(/http — panel/i).click()
      await snap('23-logs-http')
    },
  ],
  [
    '17-settings',
    async () => {
      await go('/settings')
      await snap('17-settings')
    },
  ],
  [
    '24-swagger',
    async () => {
      await go('/swagger')
      await page.waitForTimeout(1500)
      await snap('24-swagger')
    },
  ],
  [
    '19-asynqmon',
    async () => {
      await go('/asynqmon')
      await page.waitForTimeout(1500)
      await snap('19-asynqmon')
    },
  ],
  [
    '20-qhd',
    async () => {
      await page.setViewportSize({ width: 2560, height: 1440 })
      await go('/')
      await snap('20-qhd')
      await page.setViewportSize({ width: 1600, height: 900 })
    },
  ],
  [
    '18-portuguese-users',
    async () => {
      await page.evaluate(() => localStorage.setItem('ad-manager.lang', 'pt-BR'))
      await go('/users')
      await snap('18-portuguese-users')
      await page.evaluate(() => localStorage.setItem('ad-manager.lang', 'en'))
    },
  ],
]

// shots that continue the screen of earlier ones: picking them by name runs those too
const needs = {
  '21-password-link-validity': ['04-reset-password'],
  '22-password-link-generated': ['04-reset-password', '21-password-link-validity'],
  '25-password-link-page': ['04-reset-password', '21-password-link-validity', '22-password-link-generated'],
  '32-sync-wizard-what-to-copy': ['31-sync-wizard'],
}
const wanted = new Set(['01-login', ...only, ...only.flatMap((n) => needs[n] || [])])
let failed = 0
for (const [name, run] of shots) {
  if (only.length && !wanted.has(name)) continue
  try {
    await run()
  } catch (e) {
    failed++
    console.log('FAIL', name, e.message.split('\n')[0])
    await close().catch(() => {})
  }
}
await browser.close()
process.exit(failed ? 1 : 0)
