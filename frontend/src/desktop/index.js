// The Wails desktop's own code, loaded by main.js only in `vp build --mode desktop` (never part of the web build).
import { Call, Events } from '@wailsio/runtime'
import { markRaw, watch } from 'vue'
import { auth, brand, endSession, platform, session, setGeneration } from '../api'
import { addTranslations, pinLang, serverText, useDefault } from '../i18n'
import catalog from '../locales/pt-BR/desktop.js'
import { toast } from '../toasts'
import Configure from './Configure.vue'

// marks a desktop bundle: the web build's check fails if this string reaches web/dist
export const DESKTOP_SENTINEL = 'adm-desktop-wails-bundle'

// call runs a method of a Go service (desktop.go, files.go). As api() answers, whatever comes back after the session
// changed never settles; a transfer the panel refused as signed out ends the session; errors show translated.
async function call(service, method, ...args) {
  const gen = session()
  const never = () => new Promise(() => {})
  try {
    const v = await Call.ByName(`main.${service}.${method}`, ...args)
    return gen === session() ? v : never()
  } catch (e) {
    if (gen !== session() || e?.message === 'stale session') return never()
    if (e?.message === 'the session has ended: sign in again') endSession()
    throw new Error(serverText(e?.message) || String(e))
  }
}
export const shell = (method, ...args) => call('Shell', method, ...args)
export const configure = (method, ...args) => call('Configure', method, ...args)

// language shows the interface in l (desktop.toml's lang), or follows the panel's default again ('')
export function language(l) {
  pinLang(l)
  if (!l) useDefault(brand.lang)
}

// themes is the panel's theme registry, remembered for Configure when the panel cannot be reached (a convenience of
// this computer: the registry itself comes from the panel)
const THEMES_KEY = 'adm-desktop-themes'
export function themes() {
  if (brand.themes?.length) return brand.themes
  try {
    return JSON.parse(localStorage.getItem(THEMES_KEY)) || []
  } catch {
    return []
  }
}

// install wires the desktop before the app mounts.
export async function install() {
  window.__admDesktop = DESKTOP_SENTINEL
  addTranslations('pt-BR', catalog)
  const [gen, limit, origin, st] = await Promise.all([shell('Session'), shell('UploadLimit'), shell('PanelURL'), configure('State')])
  setGeneration(gen)
  language(st.lang)
  Object.assign(platform.appearance, { theme: st.theme, flat: st.flat })
  Object.assign(platform, {
    maxUpload: limit,
    panelOrigin: origin,
    defaultUser: st.user,
    configure: () => (platform.screen = markRaw(Configure)),
    cancelSend: () => call('Files', 'CancelUpload'),
    // files move in Go, between the panel and the disk, with native dialogs (files.go)
    async saveFile({ name, text, apiPath }) {
      if (apiPath) await call('Files', 'SaveFromPanel', session(), apiPath, name)
      else await call('Files', 'SaveText', name, text)
    },
    async sendFile(apiPath, onProgress) {
      const off = Events.On('adm:upload-progress', (e) => onProgress?.(Math.round((100 * e.data.sent) / (e.data.total || 1))))
      try {
        const answer = await call('Files', 'SendToPanel', session(), apiPath)
        return answer ? JSON.parse(answer) : null
      } finally {
        off()
      }
    },
  })
  if (!st.configured || st.problem) platform.configure()
  watch(
    () => brand.themes,
    (list) => {
      try {
        if (list?.length) localStorage.setItem(THEMES_KEY, JSON.stringify(list))
      } catch {
        /* a convenience only */
      }
    },
  )
  // said once after sign-in: the agent tokens of an older desktop.toml were removed
  watch(
    () => auth.user,
    async (u) => {
      const n = u && (await shell('Notice'))
      if (n) toast.warn(serverText(n))
    },
  )
  // the app's version, from Go: also proves the bindings work (the smoke build reads it)
  shell('Version').then((v) => (window.__admDesktopVersion = v))
  // the window never leaves the app: the panel's own pages open in the system browser, other links do nothing
  document.addEventListener(
    'click',
    (e) => {
      const a = e.target.closest?.('a[href]')
      if (!a) return
      const href = a.getAttribute('href')
      const to = new URL(href, location.href)
      if (href === '/asynqmon' || href === '/swagger' || to.origin !== location.origin) {
        // the panel's own pages and its support link open in the system browser (Go checks which); nothing else
        e.preventDefault()
        shell('OpenExternal', to.origin === location.origin ? href : to.href).catch(() => {})
      } else if (a.target === '_blank') e.preventDefault()
    },
    true,
  )
  window.open = () => null
}
