// Client for the Go API. A 401 sends the operator back to the login page.
import { reactive } from 'vue'
import { serverText, setServerCatalog, useDefault } from './i18n'

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

export async function api(method, path, body) {
  const opts = { method, credentials: 'same-origin' }
  if (body instanceof FormData) opts.body = body
  else if (body) Object.assign(opts, { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
  const r = await fetch('/api' + path, opts)
  if (r.status === 401 && path !== '/login') auth.user = null
  if (r.status === 204) return null
  const data = await r.json().catch(() => ({}))
  if (!r.ok) throw new Error(serverText(data.error) || `HTTP ${r.status}`)
  return data
}

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
