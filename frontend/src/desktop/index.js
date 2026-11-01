// The Wails desktop's own code, loaded by main.js only in `vp build --mode desktop` (never part of the web build).
import { Call, Events } from '@wailsio/runtime'
import { markRaw, watch } from 'vue'
import { auth, brand, endSession, onSessionChange, platform, session, setGeneration } from '../api'
import { addTranslations, pinLang, serverText, useDefault } from '../i18n'
import catalog from '../locales/pt-BR/desktop.js'
import { toast } from '../toasts'
import Configure from './Configure.vue'
import builtinThemes from './themes.json'

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

// themes is the panel's theme registry, or this build's copy of it (themes.json, kept in step with the server's by
// internal/server's tests) when the panel cannot be reached
export const themes = () => (brand.themes?.length ? brand.themes : builtinThemes)

// install wires the desktop before the app mounts.
export async function install() {
  window.__admDesktop = DESKTOP_SENTINEL
  // the desktop's panel fills its window (the web keeps its widest box: style.css --app-max-w)
  document.documentElement.style.setProperty('--app-max-w', 'none')
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
    themes: builtinThemes,
    cancelSend: () => call('Files', 'CancelUpload'),
    // files move in Go, between the panel and the disk, with native dialogs (files.go)
    async saveFile({ name, text, apiPath }) {
      if (apiPath) await call('Files', 'SaveFromPanel', session(), apiPath, name)
      else await call('Files', 'SaveText', name, text)
    },
    // the SSH terminal: the webview opens no WebSocket (the asset proxy carries none, the cookie is in Go), so Go opens
    // the panel's (terminal.go) and its frames come as "adm:terminal" events; this has the WebSocket's shape
    terminal(agentId) {
      const sock = { readyState: 0, binaryType: 'arraybuffer', error: '', onopen: null, onmessage: null, onclose: null }
      let id = 0
      let off = () => {}
      let queue = Promise.resolve() // one call at a time, in order, as a WebSocket sends: a pasted line before its Enter
      const closed = () => {
        if (sock.readyState === 3) return
        sock.readyState = 3
        off()
        sock.onclose?.()
      }
      sock.send = (d) => {
        if (sock.readyState !== 1) return
        // ignoreBOM: a pasted text's leading U+FEFF is typed as the browser's WebSocket would send it
        const [method, arg] = typeof d === 'string' ? ['Control', d] : ['Input', new TextDecoder('utf-8', { ignoreBOM: true }).decode(d)]
        queue = queue.then(() => call('Terminals', method, id, arg)).catch(closed)
      }
      sock.close = () => {
        // after the input already queued (an Enter then Disconnect still runs the command)
        if (id && sock.readyState !== 3) queue = queue.then(() => call('Terminals', 'Close', id)).catch(() => {})
        closed()
      }
      call('Terminals', 'Open', session(), agentId).then(
        (n) => {
          if (sock.readyState === 3) return call('Terminals', 'Close', n)
          id = n
          // subscribed before the first message goes: the panel sends nothing until it gets it
          off = Events.On('adm:terminal', ({ data: m }) => {
            if (m.id !== id) return
            if (m.closed) closed()
            else if (m.text != null) sock.onmessage?.({ data: m.text })
            else sock.onmessage?.({ data: Uint8Array.from(atob(m.data), (c) => c.charCodeAt(0)).buffer })
          })
          sock.readyState = 1
          sock.onopen?.()
        },
        (e) => {
          sock.error = e.message
          closed()
        },
      )
      return sock
    },
    readClipboard: () => call('Terminals', 'Clipboard'),
    async sendFile(apiPath, onProgress) {
      const off = Events.On('adm:upload-progress', (e) => onProgress?.(Math.round((100 * e.data.sent) / (e.data.total || 1))))
      // a transfer whose session ends never settles: its listener goes with the session
      const unhook = onSessionChange(() => (off(), unhook()))
      try {
        const answer = await call('Files', 'SendToPanel', session(), apiPath)
        return answer ? JSON.parse(answer) : null
      } finally {
        off()
        unhook()
      }
    },
  })
  if (!st.configured || st.problem) platform.configure()
  // said once after sign-in: the agent tokens of an older desktop.toml were removed
  watch(
    () => auth.user,
    async (u) => {
      const n = u && (await shell('Notice'))
      if (n) toast.warn(serverText(n))
    },
  )
  // the app's version, from Go: the footer shows it (the panel's may differ); also proves the bindings work (the smoke
  // build reads it)
  shell('Version').then((v) => {
    window.__admDesktopVersion = v
    auth.appVersion = v
  })
  // the window never leaves the app: the panel's own pages open in the system browser, other links do nothing
  document.addEventListener(
    'click',
    (e) => {
      const a = e.target.closest?.('a[href]')
      if (!a) return
      const href = a.getAttribute('href')
      const to = new URL(href, location.href)
      if (href === '/asynqmon' || href === '/swagger' || to.origin !== location.origin) {
        // the panel's own pages and its support link (as written: Go compares it exactly) open in the system browser
        e.preventDefault()
        shell('OpenExternal', href).catch(() => {})
      } else if (a.target === '_blank') e.preventDefault()
    },
    true,
  )
  window.open = () => null
}
