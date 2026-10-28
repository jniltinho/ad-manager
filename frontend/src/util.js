// Dependency-free helpers: dates, file saving and sending, CSV download, password generator.
import { ref, watch } from 'vue'
import { apiRaw, auth, endSession, platform, session } from './api'
import { locale, serverText, t } from './i18n'

// spinner is true while busy() is, and for at least `ms` after it turned true: a refresh icon still visibly turns
// when the reload answers at once.
export function spinner(busy, ms = 600) {
  const on = ref(false)
  let since = 0
  let timer = 0
  watch(
    busy,
    (v) => {
      clearTimeout(timer)
      if (v) {
        on.value = true
        since = Date.now()
      } else timer = setTimeout(() => (on.value = false), Math.max(0, ms - (Date.now() - since)))
    },
    { immediate: true },
  )
  return on
}

// formatDate shows date and time in the interface language ("—" when empty).
export function formatDate(iso) {
  if (!iso) return '—'
  const d = new Date(iso)
  return d.toLocaleDateString(locale()) + ' ' + d.toLocaleTimeString(locale(), { hour: '2-digit', minute: '2-digit' })
}

// timeAgo is a time relative to now in the interface language ("10 minutes ago").
export function timeAgo(iso, now = Date.now()) {
  const s = (new Date(iso) - now) / 1000
  const [n, unit] = Math.abs(s) < 3600 ? [s / 60, 'minute'] : Math.abs(s) < 86400 ? [s / 3600, 'hour'] : [s / 86400, 'day']
  return new Intl.RelativeTimeFormat(locale(), { numeric: 'auto' }).format(Math.round(n), unit)
}

// daysSince counts whole days since iso (Infinity when there is no date).
export function daysSince(iso) {
  return iso ? Math.floor((Date.now() - new Date(iso)) / 86400000) : Infinity
}

// inactive tells whether a last logon is older than the configured limit.
export const inactive = (iso) => daysSince(iso) > (auth.settings?.inactive_days || 90)

// downloadCsv saves rows (array of objects) with columns [[title, fn|field]].
export function downloadCsv(name, rows, columns) {
  const esc = (v) => `"${String(v ?? '').replaceAll('"', '""')}"`
  const val = (r, c) => (typeof c === 'function' ? c(r) : r[c])
  const txt = [columns.map((c) => esc(c[0])).join(';'), ...rows.map((r) => columns.map((c) => esc(val(r, c[1]))).join(';'))].join('\r\n')
  return saveFile({ name, text: '﻿' + txt })
}

// saveFile saves text built here, or a panel download (apiPath "/api/…"), as name. The web lets the browser
// download it; the desktop saves it through Go with a native dialog (platform.saveFile, src/desktop).
export async function saveFile({ name, text, apiPath }) {
  if (platform.saveFile) return platform.saveFile({ name, text, apiPath })
  const a = document.createElement('a')
  a.href = apiPath ?? URL.createObjectURL(new Blob([text], { type: 'text/csv;charset=utf-8' }))
  a.download = name
  a.click()
  if (!apiPath) URL.revokeObjectURL(a.href)
}

// savePanelBlob saves a panel answer made on demand (a GPO backup), named by its Content-Disposition on the web; the
// desktop has Go make the request and write the answer into the file the operator picks
export async function savePanelBlob(apiPath, fallbackName) {
  if (platform.saveFile) return platform.saveFile({ name: fallbackName, apiPath })
  const r = await apiRaw(apiPath.replace(/^\/api/, ''))
  if (!r.ok) throw new Error(serverText((await r.json().catch(() => ({}))).error) || `HTTP ${r.status}`)
  const name = /filename="([^"]+)"/.exec(r.headers.get('Content-Disposition') || '')?.[1] || fallbackName
  const a = document.createElement('a')
  a.href = URL.createObjectURL(await r.blob())
  a.download = name
  a.click()
  URL.revokeObjectURL(a.href)
}

// sendFile POSTs a file as the raw body of apiPath ("/api/…") with progress (0–100) and resolves with the panel's
// JSON answer. The web sends the File picked in the form; the desktop picks and streams it in Go (platform.sendFile),
// and resolves null when the operator cancels its dialog.
export function sendFile(apiPath, file, onProgress) {
  if (platform.sendFile) return platform.sendFile(apiPath, onProgress)
  const gen = session()
  return new Promise((resolve, reject) => {
    const req = new XMLHttpRequest()
    req.open('POST', apiPath)
    req.setRequestHeader('X-ADM-Session', String(gen))
    req.upload.onprogress = (e) => e.lengthComputable && onProgress?.(Math.round((100 * e.loaded) / e.total))
    req.onload = () => {
      let data = {}
      try {
        data = JSON.parse(req.responseText)
      } catch {
        /* not JSON */
      }
      if (gen !== session()) return // an answer of an earlier session never settles
      if (req.status === 401) endSession()
      if (req.status === 200) resolve(data)
      else reject(new Error(serverText(data.error) || `HTTP ${req.status}`))
    }
    req.onerror = () => gen === session() && reject(new Error(t('The upload failed (connection).')))
    req.send(file)
  })
}

// generatePassword returns n characters (configured length by default) with
// upper, lower, digit and symbol, without ambiguous characters (0/O, 1/l/I).
export function generatePassword(n = auth.settings?.password_length || 14) {
  const sets = ['ABCDEFGHJKLMNPQRSTUVWXYZ', 'abcdefghijkmnopqrstuvwxyz', '23456789', '@#%+=!?']
  const all = sets.join('')
  const rnd = (max) => crypto.getRandomValues(new Uint32Array(1))[0] % max
  const s = sets.map((g) => g[rnd(g.length)])
  while (s.length < n) s.push(all[rnd(all.length)])
  for (let i = s.length - 1; i > 0; i--) {
    const j = rnd(i + 1)
    ;[s[i], s[j]] = [s[j], s[i]]
  }
  return s.join('')
}

// agentHost is where a file server's agent answers ("This server" for the panel's own machine): two servers with
// look-alike names stay apart in the lists.
export function agentHost(url) {
  try {
    const h = new URL(url).hostname
    return ['127.0.0.1', 'localhost', '[::1]'].includes(h) ? t('This server') : h
  } catch {
    return ''
  }
}

// footerVersion is what the footer shows: the app's own version, and the panel's beside it when they differ (a desktop
// app talks to a panel that may be older or newer). In the browser the app is the panel: its version alone.
export function footerVersion(appVersion, panelVersion) {
  const bare = (v) => (v || '').replace(/^v/, '')
  const main = appVersion || panelVersion
  return { main, panel: appVersion && panelVersion && bare(appVersion) !== bare(panelVersion) ? panelVersion : '' }
}
