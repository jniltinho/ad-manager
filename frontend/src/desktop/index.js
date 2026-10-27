// The Wails desktop's own code, loaded by main.js only in `vp build --mode desktop` (never part of the web build).
import { Call } from '@wailsio/runtime'

// marks a desktop bundle: the web build's check fails if this string reaches web/dist
export const DESKTOP_SENTINEL = 'adm-desktop-wails-bundle'

// shell calls a method of the Go Shell service (main.go)
export const shell = (method, ...args) => Call.ByName(`main.Shell.${method}`, ...args)

// install wires the desktop before the app mounts.
export async function install() {
  window.__admDesktop = DESKTOP_SENTINEL
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
