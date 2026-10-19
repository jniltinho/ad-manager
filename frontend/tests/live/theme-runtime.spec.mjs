// The saved theme reaches the first paint (the server puts it in the HTML) and switching in Settings applies it
// without reloading. Restores the theme it found.
import { expect, test } from '@playwright/test'

const USER = { user: 'diego.rocha', password: 'Demo-0004#ok' } // demo login of the test domain (docs/development.md)
// backgrounds a frame may show while loading: the page ("Loading…") and the login page, of each theme. Before the
// CSS the browser paints its default canvas for the page's color-scheme (white for light themes): allowed, it is
// not another theme.
const BG = {
  default: [
    [217, 230, 243],
    [231, 236, 239],
  ],
  admanager: [
    [240, 241, 243],
    [249, 249, 249],
  ],
  graphite: [[17, 18, 23]],
}
// Chrome's default canvas for each color-scheme
const CANVAS = { admanager: [255, 255, 255], graphite: [18, 18, 18] }

test.describe.configure({ mode: 'serial' })
let original

// the session cookie is Secure: over http:// the API client does not send it back by itself
async function setTheme(request, theme) {
  const login = await request.post('/api/login', { data: USER })
  expect(login.status()).toBe(200)
  const headers = {
    cookie: login
      .headersArray()
      .filter((h) => h.name.toLowerCase() === 'set-cookie')
      .map((h) => h.value.split(';')[0])
      .join('; '),
  }
  const cur = (await (await request.get('/api/settings', { headers })).json()).settings
  original ??= cur.theme
  const r = await request.put('/api/settings', { data: { ...cur, theme }, headers })
  expect(r.status()).toBe(200)
}

// pixel color of the top-left corner of each frame Chrome paints while the page loads
async function firstFrames(page, cdp, url, during) {
  const frames = []
  cdp.on('Page.screencastFrame', async (f) => {
    frames.push(f.data)
    await cdp.send('Page.screencastFrameAck', { sessionId: f.sessionId }).catch(() => {})
  })
  await cdp.send('Page.startScreencast', { format: 'png', everyNthFrame: 1 })
  await page.goto(url, { waitUntil: 'commit' })
  await during()
  await cdp.send('Page.stopScreencast')
  return Promise.all(
    frames.map(async (data) =>
      page.evaluate(async (src) => {
        const img = new Image()
        img.src = 'data:image/png;base64,' + src
        await img.decode()
        const c = new OffscreenCanvas(1, 1)
        const x = c.getContext('2d')
        x.drawImage(img, -2, -2)
        return Array.from(x.getImageData(0, 0, 1, 1).data.slice(0, 3))
      }, data),
    ),
  )
}

test.afterAll(async ({ request }) => original && setTheme(request, original))

test('documents come with the saved theme and are not cached', async ({ request }) => {
  await setTheme(request, 'admanager')
  for (const path of ['/', '/users', '/password/' + 'a'.repeat(64)]) {
    // conditional requests too: the document is never answered with 304 (it depends on the saved theme)
    for (const headers of [{}, { 'if-none-match': '"x"', 'if-modified-since': new Date().toUTCString() }]) {
      const r = await request.get(path, { headers })
      expect(r.status(), path).toBe(200)
      expect(r.headers()['cache-control']).toBe('no-store')
      expect(await r.text()).toContain('data-theme="admanager"')
    }
  }
})

// /api/health is held or failed through CDP Fetch, not page.route(): Playwright's interception turns the HTTP
// cache off, and the warm case must use it
const delayed = (cdp, id) => setTimeout(() => cdp.send('Fetch.continueRequest', { requestId: id }).catch(() => {}), 3000)
const failed = (cdp, id) => cdp.send('Fetch.failRequest', { requestId: id, errorReason: 'Failed' }).catch(() => {})
for (const [name, health, warm, target = 'admanager'] of [
  ['health delayed', delayed, false],
  ['health failing', failed, false],
  ['warm cache, health delayed', delayed, true],
  ['dark theme, health delayed', delayed, false, 'graphite'],
]) {
  test(`first paint already themed (${name})`, async ({ page, request }) => {
    await setTheme(request, 'default')
    if (warm) {
      await page.goto('/') // the browser now has the page and its assets cached, with the Default theme
      await page.goto('about:blank') // leave it, so the captured frames are only the new document's (the cache stays)
    }
    await setTheme(request, target)
    // a dark theme starts from a dark page: the tab's own about:blank is white, which is not the panel's paint
    if (CANVAS[target][0] < 128) {
      await page.goto('data:text/html,<meta name="color-scheme" content="dark">')
      await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)))) // painted
    }
    const cdp = await page.context().newCDPSession(page)
    let held = 0
    cdp.on('Fetch.requestPaused', (e) => (held++, health(cdp, e.requestId)))
    await cdp.send('Fetch.enable', { patterns: [{ urlPattern: '*/api/health*', requestStage: 'Request' }] })
    let cached = 0
    cdp.on('Network.requestServedFromCache', () => cached++)
    await cdp.send('Network.enable')
    const colors = await firstFrames(page, cdp, '/', () => page.waitForTimeout(3500))
    expect(held, '/api/health intercepted').toBeGreaterThan(0)
    if (warm) expect(cached, 'assets served from the HTTP cache').toBeGreaterThan(0)
    expect(colors.length).toBeGreaterThan(0)
    for (const c of colors) expect([...BG[target], CANVAS[target]], `frame color ${c}`).toContainEqual(c)
    for (const c of colors) for (const other of BG.default) expect(c, 'a frame with the Default background').not.toEqual(other)
    expect(BG[target]).toContainEqual(colors.at(-1))
    expect(await page.evaluate(() => document.documentElement.dataset.theme)).toBe(target)
  })
}

test('switching in Settings applies the theme without reloading', async ({ page, request }) => {
  await setTheme(request, 'admanager')
  await page.goto('/')
  await page.locator('#rcmloginuser').fill(USER.user)
  await page.locator('#rcmloginpwd').fill(USER.password)
  await Promise.all([page.waitForResponse('**/api/login'), page.locator('button.button').click()])
  await page.locator('nav.top-tabs').waitFor()
  await page.locator('nav.top-tabs a[href="/settings"]').click()
  await page.locator('#s-theme').waitFor()
  await page.evaluate(() => (window.__noReload = true))
  // computed styles that tell the themes apart: the tab band (gradient in Default, flat #424f56 in ADManager)
  const band = () =>
    page.evaluate(() => {
      const s = getComputedStyle(document.querySelector('nav.top-tabs'))
      return { image: s.backgroundImage.startsWith('linear-gradient') ? 'gradient' : s.backgroundImage, color: s.backgroundColor }
    })
  const expected = { default: { image: 'gradient', color: 'rgb(10, 18, 34)' }, admanager: { image: 'none', color: 'rgb(66, 79, 86)' } }
  // a dark theme in between: the page's color-scheme (meta and the <html> served inline) follows the theme
  const scheme = () => page.evaluate(() => getComputedStyle(document.documentElement).colorScheme)
  for (const [theme, sch] of [
    ['default', 'light'],
    ['graphite', 'dark'],
    ['admanager', 'light'],
  ]) {
    await page.locator('#s-theme').selectOption(theme)
    await page.locator('form button[type="submit"], button.btn:has-text("Salvar")').first().click()
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
    await expect(page.locator('meta[name="color-scheme"]')).toHaveAttribute('content', sch)
    await expect.poll(scheme).toBe(sch)
    if (expected[theme]) await expect.poll(band).toEqual(expected[theme])
  }
  expect(await page.evaluate(() => window.__noReload)).toBe(true)

  // square corners in every theme, measured on what is on screen in several views
  for (const path of ['/', '/users', '/groups', '/shares', '/import-export', '/logs', '/settings']) {
    await page.locator(`nav.top-tabs a[href="${path}"]`).click()
    await page.waitForURL((u) => u.pathname === path)
    await page.waitForLoadState('networkidle')
    for (const theme of ['default', 'admanager', 'graphite', 'classic-gray']) {
      await page.evaluate((t) => (document.documentElement.dataset.theme = t), theme)
      const rounded = await page.evaluate(() =>
        [...document.querySelectorAll('*')]
          .filter((e) => e.getClientRects().length)
          .filter((e) =>
            ['TopLeft', 'TopRight', 'BottomLeft', 'BottomRight'].some((k) => getComputedStyle(e)[`border${k}Radius`] !== '0px'),
          )
          .map((e) => e.tagName + '.' + e.className),
      )
      expect(rounded, `${path} ${theme}`).toEqual([])
    }
  }
})

// QHD/4K zoom the page (style.css): the suggestion list must still open right under its field
test('suggestions sit under their field at every page zoom', async ({ browser }) => {
  for (const width of [1600, 2560, 3400]) {
    const page = await browser.newPage({ viewport: { width, height: 1300 } })
    await page.goto('/')
    await page.locator('#rcmloginuser').fill(USER.user)
    await page.locator('#rcmloginpwd').fill(USER.password)
    await Promise.all([page.waitForResponse('**/api/login'), page.locator('button.button').click()])
    const input = page.locator('main form input').first()
    await input.waitFor()
    await page.waitForLoadState('networkidle')
    await input.pressSequentially('a')
    await page.locator('.suggestions').first().waitFor()
    const [i, l] = await page.evaluate(() =>
      [document.querySelector('main form input'), document.querySelector('.suggestions')].map((e) => e.getBoundingClientRect().toJSON()),
    )
    expect(Math.abs(l.left - i.left), `${width}: left`).toBeLessThan(2)
    expect(Math.abs(l.width - i.width), `${width}: width`).toBeLessThan(2)
    expect(l.top - i.bottom, `${width}: gap`).toBeGreaterThanOrEqual(0)
    expect(l.top - i.bottom, `${width}: gap`).toBeLessThan(6)
    await page.close()
  }
})

// browsers without appearance: base-select (Firefox, older Chrome): the open list of a select is themed (native.js)
test('select lists follow the theme where the browser cannot style them', async ({ page, request }) => {
  await setTheme(request, 'graphite')
  await page.addInitScript(() => {
    const supports = CSS.supports.bind(CSS)
    CSS.supports = (...a) => (String(a.join(':')).includes('base-select') ? false : supports(...a))
  })
  await page.goto('/')
  await page.locator('#rcmloginuser').fill(USER.user)
  await page.locator('#rcmloginpwd').fill(USER.password)
  await Promise.all([page.waitForResponse('**/api/login'), page.locator('button.button').click()])
  await page.locator('nav.top-tabs a[href="/settings"]').click()
  const sel = page.locator('#s-theme')
  await sel.waitFor()
  await sel.click()
  const pop = page.locator('.select-pop')
  await expect(pop).toBeVisible()
  // theme roles, not system colors: the field background of Grafite and its accent on top
  const look = await pop.evaluate((e) => [getComputedStyle(e).backgroundColor, getComputedStyle(e).borderTopColor])
  expect(look).toEqual(['rgb(17, 18, 23)', 'rgb(255, 136, 51)'])
  expect(await pop.locator('li').count()).toBe(await sel.locator('option').count())
  // right under the select
  const [s, p] = await Promise.all([sel.boundingBox(), pop.boundingBox()])
  expect(Math.abs(p.x - s.x)).toBeLessThan(2)
  expect(p.y - (s.y + s.height)).toBeLessThan(6)
  // keyboard: one up and Enter picks the previous theme (Grafite is the last); the click picks another and closes the list
  const before = await sel.inputValue()
  await page.keyboard.press('ArrowUp')
  await page.keyboard.press('Enter')
  await expect(pop).toHaveCount(0)
  expect(await sel.inputValue()).not.toBe(before)
  await sel.click()
  await pop.locator('li', { hasText: /^Default$/ }).click()
  await expect(pop).toHaveCount(0)
  expect(await sel.inputValue()).toBe('default')
})
