// Dependency-free helpers: dates, CSV download, password generator.
import { auth } from './api'
import { locale, t } from './i18n'

// formatDate shows date and time in the interface language ("—" when empty).
export function formatDate(iso) {
  if (!iso) return '—'
  const d = new Date(iso)
  return d.toLocaleDateString(locale()) + ' ' + d.toLocaleTimeString(locale(), { hour: '2-digit', minute: '2-digit' })
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
  const a = document.createElement('a')
  a.href = URL.createObjectURL(new Blob(['﻿' + txt], { type: 'text/csv;charset=utf-8' }))
  a.download = name
  a.click()
  URL.revokeObjectURL(a.href)
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
