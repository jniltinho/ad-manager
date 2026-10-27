// The Wails desktop's own code, loaded by main.js only in `vp build --mode desktop` (never part of the web build).
import { Call, Events } from '@wailsio/runtime'
import { platform, session, setGeneration } from '../api'
import { serverText } from '../i18n'

// marks a desktop bundle: the web build's check fails if this string reaches web/dist
export const DESKTOP_SENTINEL = 'adm-desktop-wails-bundle'

// call runs a method of a Go service (main.go, files.go). Its error shows translated like the panel's; a transfer of
// a session that has ended never settles, as api() answers do.
async function call(service, method, ...args) {
  try {
    return await Call.ByName(`main.${service}.${method}`, ...args)
  } catch (e) {
    if (e?.message === 'stale session') return new Promise(() => {})
    throw new Error(serverText(e?.message) || String(e))
  }
}
export const shell = (method, ...args) => call('Shell', method, ...args)

// install wires the desktop before the app mounts.
export async function install() {
  window.__admDesktop = DESKTOP_SENTINEL
  const [gen, limit, origin] = await Promise.all([shell('Session'), shell('UploadLimit'), shell('PanelURL')])
  setGeneration(gen)
  Object.assign(platform, {
    maxUpload: limit,
    panelOrigin: origin,
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
  // the app's version, from Go: also proves the bindings work (the smoke build reads it)
  shell('Version').then((v) => (window.__admDesktopVersion = v))
  // the window never leaves the app: the panel's own pages open in the system browser, other links do nothing
  document.addEventListener(
    'click',
    (e) => {
      const a = e.target.closest?.('a[href]')
      if (!a) return
      const href = a.getAttribute('href')
      if (href === '/asynqmon' || href === '/swagger') {
        e.preventDefault()
        shell('OpenExternal', href)
      } else if (a.target === '_blank' || new URL(href, location.href).origin !== location.origin) e.preventDefault()
    },
    true,
  )
  window.open = () => null
}
