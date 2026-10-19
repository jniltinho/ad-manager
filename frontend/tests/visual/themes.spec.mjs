// Pixel-exact screens of every theme (theme-base, phase A): the role refactor must not change a single pixel of
// Default and ADManager.
//
// - The API is replayed from one HAR per test (tests/visual/har/<test>.har, versioned for the CI), recorded with
//   RECORD=1 against the dev panel on :8080 (demo login of the test domain; cookies are stripped after recording).
//   A request missing from the HAR fails the test.
// - The file server agents and a valid password link are fixtures (the dev panel has no reachable agent): both
//   builds get the same replies.
// - An /api reply >= 400 fails the test unless the test expects it, and before each capture the screen must show
//   its own content (no "Loading…", no error toast unless the state is the toast).
// - The comparison is strict: decoded RGBA bytes, no antialiasing tolerance, no mask (Playwright's comparator
//   ignores pixels it classifies as antialiasing even with threshold 0, so it is not used).
// Baseline from the base commit's build: DIST=<base>/web/dist BASELINE=1 npx playwright test; then
// `npx playwright test` compares the current build (web/dist).
import { mkdirSync, readFileSync, writeFileSync, existsSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { expect, test } from '@playwright/test'
import { PNG } from 'pngjs'

const RUN = fileURLToPath(new URL('../../../.run/visual/', import.meta.url))
const HAR = fileURLToPath(new URL('har/', import.meta.url))
const RECORD = !!process.env.RECORD
const BASELINE = !!process.env.BASELINE
// every theme of the registry (internal/server/themes.go); THEMES=a,b limits the run (e.g. the baseline of a new one)
const REGISTRY = Object.fromEntries(
  [
    ...readFileSync(fileURLToPath(new URL('../../../internal/server/themes.go', import.meta.url)), 'utf8').matchAll(
      /\{ID: (?:"([^"]+)"|theme(\w+)), Label: "[^"]+", Scheme: "(\w+)"/g,
    ),
  ].map((m) => [m[1] || m[2].toLowerCase(), m[3]]),
)
const THEMES = process.env.THEMES ? process.env.THEMES.split(',') : Object.keys(REGISTRY)
const FIXED = new Date('2026-10-18T09:30:00-03:00')
const TOKEN = 'a'.repeat(64) // the password page only opens for a 64-hex token (expired in the HAR)
const VALID = 'b'.repeat(64) // a valid link (fixture)
const SHARES = [
  {
    name: 'Financeiro',
    path: '/srv/dados/financeiro',
    comment: 'Planilhas',
    rw_groups: ['TEST\\financeiro'],
    ro_groups: [],
    browseable: true,
  },
  {
    name: 'Publico',
    path: '/srv/dados/publico',
    rw_groups: [],
    ro_groups: ['TEST\\Domain Users'],
    full_groups: ['TEST\\ti'],
    browseable: false,
  },
]
const json =
  (body, status = 200) =>
  (route) =>
    route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })

test.describe.configure({ mode: 'serial' })

async function setup(page, info) {
  await page.clock.install({ time: FIXED })
  // no transitions/animations: elements with transform/filter transitions may get their own compositing layer,
  // rasterized differently from run to run (a few antialiased icon pixels); the static screen is the same
  await page.addInitScript(() => {
    const css = '*, *::before, *::after { transition: none !important; animation: none !important; }'
    const add = () => document.head.append(Object.assign(document.createElement('style'), { textContent: css }))
    if (document.head) add()
    else document.addEventListener('DOMContentLoaded', add)
  })
  // generated passwords use crypto.getRandomValues: a seeded generator makes them repeatable
  await page.addInitScript(() => {
    let seed = 0x2f6b9a1d
    crypto.getRandomValues = (a) => {
      for (let i = 0; i < a.length; i++) {
        seed ^= seed << 13
        seed ^= seed >>> 17
        seed ^= seed << 5
        a[i] = seed >>> 0
      }
      return a
    }
  })
  // VISUAL_LANG=en: the screens in that language (the docs' theme mosaics); the recorded API replies stay the same
  if (process.env.VISUAL_LANG) await page.addInitScript((l) => localStorage.setItem('ad-manager.lang', l), process.env.VISUAL_LANG)
  const har = `${HAR}${info.title.replace(/\W+/g, '-')}.har`
  await page.routeFromHAR(har, { url: '**/api/**', update: RECORD, updateContent: 'embed', notFound: 'abort' })
  const missing = []
  page.on('requestfailed', (r) => r.url().includes('/api/') && missing.push(r.url()))
  // fixtures (registered after the HAR, so they win)
  await page.route('**/api/agents/*/health', json({ status: 'ok' }))
  await page.route('**/api/agents/*/shares', json(SHARES))
  await page.route('**/api/agents/*/dirs', json({ roots: ['/srv/dados'] }))
  await page.route('**/api/agents/*/sizes*', json({ sizes: { Financeiro: 5583457484, Publico: 734003200 } }))
  await page.route(`**/api/password-link/${VALID}`, (r) =>
    json(r.request().method() === 'POST' ? {} : { user: 'diego.rocha', name: 'Diego Rocha', expires: '2026-10-19T09:30:00-03:00' })(r),
  )
  // replies with an error: each test compares them (in any order) with the ones it expects
  page.errors = []
  page.on(
    'response',
    (r) => r.url().includes('/api/') && r.status() >= 400 && page.errors.push(`${r.status()} ${new URL(r.url()).pathname}`),
  )
  return missing
}

async function login(page) {
  await page.goto('/')
  await page.locator('#rcmloginuser').fill('diego.rocha')
  await page.locator('#rcmloginpwd').fill('Demo-0004#ok') // demo login of the test domain (docs/development.md)
  await Promise.all([page.waitForResponse('**/api/login'), page.locator('button.button').click()])
  await page.locator('nav.top-tabs').waitFor()
}

// go clicks a top tab and waits for that exact route
async function go(page, path) {
  await page.locator(`nav.top-tabs a[href="${path}"]`).click()
  await page.waitForURL((u) => u.pathname === path)
  // the page transition is out-in: the old view stays a moment after the URL changes
  await page.waitForFunction(() => {
    const v = document.querySelectorAll('.page-transition')
    return v.length === 1 && !/-(enter|leave)-/.test(v[0].className)
  })
}

// ready: the screen shows `selector`, nothing is loading and (unless `toast`) no error toast is up — checked now,
// not waited away
async function ready(page, selector, toast = false, keepMouse = false) {
  await page.waitForLoadState('networkidle')
  if (selector) await expect(page.locator(selector).first(), `waiting for ${selector}`).toBeVisible()
  await expect(page.getByText(/^(Carregando…|Loading…)$/)).toHaveCount(0)
  if (!toast) expect(await page.locator('.toast.err').count(), 'error toast on screen').toBe(0)
  await page.evaluate(() => document.fonts.ready)
  // images decoded at their drawn size (the SVG logo otherwise rasterizes a few edge pixels differently)
  await page.evaluate(() => Promise.all([...document.images].map((i) => i.decode().catch(() => {}))))
  if (!keepMouse) await page.mouse.move(0, 0) // keepMouse: a state that needs the pointer (an open tooltip)
  await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))))
}

// stable capture: screenshots until two in a row are byte-identical
async function capture(page) {
  const opts = { animations: 'disabled', caret: 'hide', scale: 'css' }
  let prev = await page.screenshot(opts)
  for (let i = 0; i < 6; i++) {
    const cur = await page.screenshot(opts)
    if (cur.equals(prev)) return cur
    prev = cur
  }
  throw new Error('screen never settled')
}

// differing returns how many pixels differ from the baseline (and writes the actual image and the diff)
function compare(name, png) {
  if (RECORD) return
  const file = `${RUN}baseline/${name}.png`
  if (BASELINE) {
    mkdirSync(`${RUN}baseline`, { recursive: true })
    writeFileSync(file, png)
    return null
  }
  expect(existsSync(file), `no baseline ${name} (record it from the base commit)`).toBe(true)
  const a = PNG.sync.read(readFileSync(file))
  const b = PNG.sync.read(png)
  expect([b.width, b.height], `${name}: size`).toEqual([a.width, a.height])
  const diff = new PNG({ width: a.width, height: a.height })
  let n = 0
  let box = null
  for (let i = 0; i < a.data.length; i += 4) {
    const same =
      a.data[i] === b.data[i] && a.data[i + 1] === b.data[i + 1] && a.data[i + 2] === b.data[i + 2] && a.data[i + 3] === b.data[i + 3]
    diff.data.set(same ? [a.data[i], a.data[i + 1], a.data[i + 2], 40] : [255, 0, 0, 255], i)
    if (same) continue
    n++
    const x = (i / 4) % a.width
    const y = Math.floor(i / 4 / a.width)
    box = box ? [Math.min(box[0], x), Math.min(box[1], y), Math.max(box[2], x), Math.max(box[3], y)] : [x, y, x, y]
  }
  if (n) {
    mkdirSync(`${RUN}diff`, { recursive: true })
    writeFileSync(`${RUN}diff/${name}-actual.png`, png)
    writeFileSync(`${RUN}diff/${name}-diff.png`, PNG.sync.write(diff))
  }
  return { n, box }
}

// holdToasts keeps the toasts on screen while every theme is captured: their own hide timers (4 s and 4.4 s) are not
// started, so a slow capture cannot lose them; afterwards they are closed by a click, as a user would
async function holdToasts(page, hold) {
  await page.evaluate((hold) => {
    if (hold) {
      window.__setTimeout = window.setTimeout
      window.setTimeout = (f, ms, ...a) => (ms === 4000 || ms === 4400 ? 0 : window.__setTimeout(f, ms, ...a))
    } else window.setTimeout = window.__setTimeout
  }, hold)
  if (!hold) {
    for (const t of await page.locator('.toast').all()) await t.click()
    await expect(page.locator('.toast')).toHaveCount(0)
  }
}

// autofill shows the fields as the browser fills a saved password: the :autofill state forced through the
// inspector (no saved credential needed; the browser's own autofill background is not part of it)
async function autofill(page, selectors) {
  const cdp = await page.context().newCDPSession(page)
  await cdp.send('DOM.enable')
  await cdp.send('CSS.enable')
  const { root } = await cdp.send('DOM.getDocument')
  for (const selector of selectors) {
    const { nodeId } = await cdp.send('DOM.querySelector', { nodeId: root.nodeId, selector })
    await cdp.send('CSS.forcePseudoState', { nodeId, forcedPseudoClasses: ['autofill'] })
  }
}

async function shot(page, name, selector, toast = false, keepMouse = false) {
  // the screen is loaded first: App.vue applies the saved theme once the brand arrives, which would undo ours
  await ready(page, selector, toast, keepMouse)
  for (const theme of THEMES) {
    // as App.vue does: the theme and the page's color-scheme
    await page.evaluate(
      ([t, scheme]) => {
        document.documentElement.dataset.theme = t
        document.querySelector('meta[name="color-scheme"]').setAttribute('content', scheme)
        // open popups (suggestions) place themselves again: a theme may move the content (another font size)
        window.dispatchEvent(new Event('resize'))
      },
      [theme, REGISTRY[theme]],
    )
    await ready(page, selector, toast, keepMouse)
    // exact match, but a fresh capture is retried: a rare late repaint (a few pixels) is not a regression, while a
    // real change differs on every attempt
    let res
    for (let attempt = 0; attempt < 3; attempt++) {
      res = compare(`${name}-${theme}`, await capture(page))
      if (!res?.n) break
      // a fresh paint: rotated pieces (the checkbox tick) can rasterize a few pixels differently once laid out;
      // switching the theme away and back repaints everything from scratch
      await page.evaluate((t) => {
        const html = document.documentElement
        html.dataset.theme = t === 'default' ? 'admanager' : 'default'
        void html.offsetHeight
        html.dataset.theme = t
        window.dispatchEvent(new Event('resize'))
      }, theme)
      await page.waitForTimeout(300)
    }
    expect(res?.n ?? 0, `${name}-${theme}: ${res?.n} pixels differ in ${res?.box} (see .run/visual/diff)`).toBe(0)
    expect(await page.evaluate(() => document.documentElement.dataset.theme), `${name}: theme kept`).toBe(theme)
  }
}

test('login', async ({ page }, info) => {
  const missing = await setup(page, info)
  await page.goto('/')
  await shot(page, 'login', '#rcmloginuser')
  expect(missing).toEqual([])
  expect(page.errors.sort()).toEqual(['401 /api/me'].sort())
})

test('password link', async ({ page }, info) => {
  const missing = await setup(page, info)
  // expired, valid, changed
  await page.goto(`/password/${TOKEN}`)
  await shot(page, 'password-link', '#login-form .error')
  await page.goto(`/password/${VALID}`)
  await shot(page, 'password-link-valid', '#lnk-new')
  await page.locator('#lnk-new').fill('Nova-Senha-2026#')
  await page.locator('#lnk-confirm').fill('Nova-Senha-2026#')
  await page.locator('button.button').click()
  await shot(page, 'password-link-done', '#login-form .done')
  expect(missing).toEqual([])
  expect(page.errors.sort()).toEqual([`404 /api/password-link/${TOKEN}`, '401 /api/me', '401 /api/me'].sort())
})

test('panel pages', async ({ page }, info) => {
  test.setTimeout(900_000) // the pages, then a fresh page for every theme (slow on the CI runner)
  const missing = await setup(page, info)
  await login(page)
  for (const [path, name, selector] of [
    ['/', 'home', 'main section button'],
    ['/users', 'users', '.grid-t tbody tr'],
    ['/groups', 'groups', '.grid-t tbody tr'],
    ['/ous', 'ous', '.grid-t tbody tr'],
    ['/computers', 'computers', '.grid-t tbody tr'],
    ['/shares', 'shares', '.grid-t tbody tr .font-semibold'],
    ['/password-policy', 'password-policy', 'main input'],
    ['/import-export', 'import-export', 'main .card'],
    ['/logs', 'logs', '.grid-t tbody tr'],
    ['/settings', 'settings', '#s-theme'],
  ]) {
    await go(page, path)
    await shot(page, name, selector)
  }
  // a table still loading says so, not "no shares" (interface-modais-setup): the agent's reply is held back
  let release
  const held = new Promise((r) => (release = r))
  await page.route('**/api/agents/*/shares', async (route) => {
    await held
    await route.fallback()
  })
  await go(page, '/shares')
  await expect(page.locator('.grid-t tbody [role="status"]')).toContainText(/^(Carregando…|Loading…)$/)
  await expect(page.getByText(/^(Nenhum compartilhamento|No shares)/)).toHaveCount(0)
  release()
  await ready(page, '.grid-t tbody tr .font-semibold')
  await page.unroute('**/api/agents/*/shares')
  // a theme leaves nothing behind (theme-conformity): each theme on a freshly loaded page (the reference, never
  // drawn after another theme) gives the same pixels as after all the others, in two orders (Cristal included)
  const setTheme = async (theme) => {
    await page.evaluate(
      ([t, scheme]) => {
        document.documentElement.dataset.theme = t
        document.querySelector('meta[name="color-scheme"]').setAttribute('content', scheme)
        window.dispatchEvent(new Event('resize'))
      },
      [theme, REGISTRY[theme]],
    )
    await ready(page, 'main section button')
    return capture(page)
  }
  const fresh = new Map()
  for (const theme of THEMES) {
    await go(page, '/')
    await page.reload()
    await ready(page, 'main section button')
    fresh.set(theme, await setTheme(theme))
  }
  for (const order of [THEMES, [...THEMES].reverse()]) {
    for (const theme of order) {
      const after = await setTheme(theme)
      expect(after.equals(fresh.get(theme)), `${theme} drawn after other themes differs from a fresh page`).toBe(true)
    }
  }
  expect(missing).toEqual([])
  expect(page.errors.sort()).toEqual(['401 /api/me'].sort())
})

test('states', async ({ page }, info) => {
  const missing = await setup(page, info)
  await login(page)
  // logs: HTTP and Samba tabs
  await go(page, '/logs')
  await page.locator('main button', { hasText: /HTTP/ }).click()
  await shot(page, 'logs-http', '.grid-t tbody tr')
  await page.locator('main button', { hasText: /Samba/ }).first().click()
  await shot(page, 'logs-samba', 'main')
  // suggestions under a field
  await go(page, '/')
  await ready(page, 'main section button')
  await page.locator('main form input').first().pressSequentially('a')
  await shot(page, 'suggestions', '.suggestions li')
  // dialogs, and a dialog with a field rejected by validation
  await go(page, '/users')
  await ready(page, '.grid-t tbody tr')
  await page
    .locator('main button.btn')
    .filter({ has: page.locator('svg') })
    .first()
    .click()
  await shot(page, 'users-new', 'dialog[open]')
  await page.keyboard.press('Escape')
  await go(page, '/groups')
  await ready(page, '.grid-t tbody tr')
  await page
    .locator('main button.btn')
    .filter({ has: page.locator('svg') })
    .first()
    .click()
  await shot(page, 'groups-new', 'dialog[open]')
  await page.locator('dialog[open] button[type="submit"], dialog[open] .pnl-foot .btn').last().click()
  // the rejected field also raises its error toast (it hides itself after 4 s)
  await shot(page, 'groups-new-invalid', 'dialog[open] .inp.invalid', true)
  await page.keyboard.press('Escape')
  await expect(page.locator('.toast.err')).toHaveCount(0, { timeout: 10000 })
  // flat look (Settings) over every theme
  await page.evaluate(() => (document.documentElement.dataset.flat = 'true'))
  await shot(page, 'groups-flat', '.grid-t tbody tr')
  await page.evaluate(() => (document.documentElement.dataset.flat = 'false'))
  // shares: the last server selected (its bottom line), then a failed refresh (error toast)
  await go(page, '/shares')
  await ready(page, '.grid-t tbody tr .font-semibold')
  await page.locator('.side-nav li').last().click()
  await shot(page, 'shares-last', '.side-nav li:last-child[aria-current]')
  await page.route('**/api/agents/*/shares', json({ error: 'server fs-visual-2 unavailable' }, 502))
  await page.locator('main button.btn-ghost', { hasText: /Atualizar|Refresh/ }).click()
  await shot(page, 'toast-error', '.toast.err.show', true)
  expect(missing).toEqual([])
  expect(page.errors.sort()).toEqual(['401 /api/me', '502 /api/agents/2/shares'].sort())
})

// a "customer logo" for the logo states: a 330 × 110 PNG drawn here (deterministic)
function customerLogo() {
  const png = new PNG({ width: 330, height: 110 })
  for (let y = 0; y < 110; y++)
    for (let x = 0; x < 330; x++) {
      const i = (y * 330 + x) * 4
      const mark = x > 20 && x < 110 && y > 15 && y < 95
      const bar = x > 130 && x < 310 && ((y > 30 && y < 50) || (y > 62 && y < 78))
      png.data.set(mark ? [200, 40, 60, 255] : bar ? [40, 60, 90, 255] : [0, 0, 0, 0], i)
    }
  return PNG.sync.write(png)
}

test('more states', async ({ page }, info) => {
  const missing = await setup(page, info)
  // a company logo: the public brand of the recorded HAR with logo: true, and the image itself
  if (!RECORD) {
    const har = JSON.parse(readFileSync(`${HAR}${info.title.replace(/\W+/g, '-')}.har`, 'utf8'))
    const body = (path) => {
      const e = har.log.entries.find((e) => new URL(e.request.url).pathname === path && e.response.status === 200)
      const b = JSON.parse(e.response.content.text)
      b.brand.logo = true
      return b
    }
    await page.route('**/api/health', json(body('/api/health')))
    // /api/me answers 401 (from the HAR) until the sign-in, then the session with the logo
    let signedIn = false
    const me = body('/api/me')
    await page.route('**/api/login', (r) => ((signedIn = true), r.fallback()))
    await page.route('**/api/me', (r) => (signedIn ? json(me)(r) : r.fallback()))
  }
  await page.route('**/api/brand/logo*', (r) => r.fulfill({ status: 200, contentType: 'image/png', body: customerLogo() }))
  await page.goto('/')
  if (!RECORD) {
    await shot(page, 'login-logo', '#login-form img.logo-plate')
    // a saved password filled by the browser, the user field focused
    await page.locator('#rcmloginpwd').fill('xxxxxxxxxx')
    await page.locator('#rcmloginuser').fill('diego.rocha')
    await autofill(page, ['#rcmloginuser', '#rcmloginpwd'])
    await shot(page, 'login-autofill', '#login-form')
  }
  await login(page)
  if (!RECORD) await shot(page, 'header-logo', 'header img.logo-plate')
  // an open tooltip (the pointer stays on the element)
  await go(page, '/users')
  await ready(page, '.grid-t tbody tr')
  await page.locator('.grid-t tbody tr [title]:not([title=""])').first().hover()
  await shot(page, 'tooltip', '.theme-tip', false, true)
  await page.mouse.move(0, 0)
  // a success toast: saving the settings (the PUT echoes what was sent)
  await go(page, '/settings')
  await ready(page, '#s-theme')
  await page.route('**/api/settings', (r) =>
    r.request().method() === 'PUT'
      ? r.fulfill({ status: 200, contentType: 'application/json', body: r.request().postData() })
      : r.fallback(),
  )
  await holdToasts(page, true)
  await page.locator('main button.btn', { hasText: /Salvar configuração|Save settings/ }).click()
  await shot(page, 'toast-success', '.toast.show', true)
  await holdToasts(page, false)
  // the import preview, then an apply with one error (warning toast)
  const plan = [
    { kind: 'ou.create', target: 'Financeiro', detail: 'OU=Financeiro', ok: true },
    { kind: 'group.create', target: 'financeiro', detail: 'security / global', ok: true },
    { kind: 'user.create', target: 'ana.lima', detail: 'Ana Lima → OU=Financeiro', ok: true },
    { kind: 'skip', target: 'diego.rocha', detail: 'unchanged', ok: true },
  ]
  await page.route('**/api/import/plan', json({ plan }))
  await page.route('**/api/import/apply', json({ plan: plan.map((a, i) => ({ ...a, ok: i !== 2, result: i === 2 ? 'exists' : 'ok' })) }))
  await go(page, '/import-export')
  await ready(page, 'main .card')
  await page
    .locator('main input[type="file"]')
    .first()
    .setInputFiles({ name: 'ous.csv', mimeType: 'text/csv', buffer: Buffer.from('ou\nFinanceiro\n') })
  await page
    .locator('main button.btn', { hasText: /Pré-visualizar|Preview/ })
    .first()
    .click()
  await shot(page, 'import-preview', 'main .grid-t tbody tr')
  await page.locator('main button.btn', { hasText: /Gravar no domínio|Write to the domain/ }).click()
  await holdToasts(page, true)
  await page.locator('dialog[open] .pnl-foot .btn, dialog[open] button.btn').first().click()
  await shot(page, 'toast-warn', '.toast.warn.show', true)
  await holdToasts(page, false)
  expect(missing).toEqual([])
  // the login page loads twice (first visit, then the login helper)
  expect(page.errors.sort()).toEqual(['401 /api/me', '401 /api/me'])
})
