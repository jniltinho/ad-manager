// Client for the Go API. A 401 sends the operator back to the login page.
import { reactive, shallowReactive } from 'vue'
import { serverText, setServerCatalog, t, useDefault } from './i18n'

export const auth = reactive({ user: null, domain: '', version: '', checked: false, settings: {}, brand: {} })

// brand is the public company branding (login page), loaded before login.
export const brand = reactive({
  company: '',
  support_url: '',
  logo: false,
  lang: '',
  theme: '',
  flat: false,
  themes: [],
  domain: '',
  netbios: '',
})

// ── sessions. Every sign-in and sign-out starts a new one. An answer of an earlier session never settles: nothing
// waiting for it runs, so a late list or a late 401 cannot reach the new session's screens. The desktop's Go proxy
// keeps the generation too: it refuses (409, X-ADM-Stale) a request built under an older one, before the panel.
let generation = 0
const never = () => new Promise(() => {})
const sessionHooks = new Set()
// onSessionChange runs fn at every new session (open confirmations are cancelled, pollers stop); returns the undo
export function onSessionChange(fn) {
  sessionHooks.add(fn)
  return () => sessionHooks.delete(fn)
}
function newSession(gen) {
  generation = gen || generation + 1
  for (const fn of sessionHooks) fn()
}
// session is the current generation (a raw XHR compares it before and after)
export const session = () => generation
// setGeneration adopts the desktop proxy's generation at start
export const setGeneration = (gen) => (generation = gen)

// platform holds what the desktop replaces (src/desktop): file saving and sending, the upload limit of its bridge,
// the panel's origin for links used outside the app, a screen shown instead of the app (Configure) and how to open
// it, this computer's appearance over the panel's, the user the sign-in starts with
export const platform = shallowReactive({
  saveFile: null,
  sendFile: null,
  maxUpload: 0,
  panelOrigin: '',
  screen: null,
  configure: null,
  appearance: reactive({ theme: '', flat: '' }),
  defaultUser: '',
})

// endSession ends the session on screen: a 401 of the current session, or a transfer the panel refused as signed
// out. Open dialogs close and pollers stop (onSessionChange), and the sign-in screen shows.
export function endSession() {
  auth.user = null
  newSession()
}

// apiRaw is fetch('/api' + path) with the session rules; api() and the few callers that need the raw answer (a
// blob, a text) use it. Everything about an earlier session never settles, a failure as much as an answer; a
// sign-in or a sign-out counts only for the session it was made in.
export async function apiRaw(path, opts = {}) {
  const gen = generation
  const headers = { ...opts.headers, 'X-ADM-Session': String(gen) }
  let r
  try {
    r = await fetch('/api' + path, { credentials: 'same-origin', ...opts, headers })
  } catch (e) {
    if (gen !== generation) return never()
    if (path === '/logout') newSession() // a sign-out that failed still ends the session here
    throw e
  }
  if (gen !== generation || r.headers.has('X-ADM-Stale')) return never()
  if (path === '/logout' || (path === '/login' && r.ok)) newSession(Number(r.headers.get('X-ADM-Session')))
  // a 401 ends a signed-in session; signed out (the first /api/me) it only says so, and the call answers
  else if (r.status === 401 && path !== '/login') {
    if (auth.user) endSession()
    else auth.user = null
  }
  return r
}

export async function api(method, path, body) {
  const opts = { method }
  if (body instanceof FormData) {
    checkUpload(body)
    opts.body = body
  } else if (body) Object.assign(opts, { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
  const gen = generation
  const r = await apiRaw(path, opts)
  if (r.status === 204) return null
  const data = await r.json().catch(() => ({}))
  if (gen !== generation && path !== '/login' && path !== '/logout') return never()
  if (!r.ok) throw new Error(serverText(data.error) || `HTTP ${r.status}`)
  return data
}

// checkUpload refuses, before sending, a form whose files pass the desktop's upload limit (no limit on the web)
export function checkUpload(fd) {
  if (!platform.maxUpload) return
  let size = 0
  for (const v of fd.values()) size += typeof v === 'string' ? v.length : v.size
  if (size > platform.maxUpload)
    throw new Error(t('The files are larger than {n} MiB, the most the desktop app sends at once.', { n: platform.maxUpload >> 20 }))
}

// latest() is a guard for a page that switches targets (a server): wrap each load's request with the returned
// function, and an answer (or a failure) that a newer load replaced never settles; drop() replaces the pending one
// at once (the target changed before a new load starts)
export function latest() {
  let n = 0
  const fresh = async (promise) => {
    const mine = ++n
    try {
      const v = await promise
      return mine === n ? v : never()
    } catch (e) {
      if (mine !== n) return never()
      throw e
    }
  }
  fresh.drop = () => n++
  return fresh
}

// panelOrigin is the panel's address for links used outside the app (the single-use password link)
export const panelOrigin = () => platform.panelOrigin || location.origin

export const get = (p) => api('GET', p)
export const post = (p, b) => api('POST', p, b ?? {})
export const put = (p, b) => api('PUT', p, b)
export const del = (p) => api('DELETE', p)
export const enc = encodeURIComponent

// logoUrl busts the cache after a new logo is uploaded.
let logoVersion = 0
export const logoUrl = () => `/api/brand/logo?v=${logoVersion}`
export const bumpLogo = () => logoVersion++

export async function loadBrand() {
  fetch('/api/i18n')
    .then((r) => r.json())
    .then(setServerCatalog)
    .catch(() => {}) // without it, server messages show in English
  try {
    const h = await fetch('/api/health').then((r) => r.json())
    Object.assign(brand, h.brand || {}, { domain: h.domain, netbios: h.netbios, setup: !!h.setup })
    useDefault(brand.lang)
  } catch {
    // panel still works with the default brand
  }
}

export async function loadMe() {
  try {
    Object.assign(auth, await get('/me'))
    Object.assign(brand, auth.brand)
    useDefault(auth.settings?.lang)
  } catch {
    auth.user = null
  }
  auth.checked = true
}
